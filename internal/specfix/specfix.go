// Package specfix repairs cosmetic defects in OpenAPI documents so that a
// vendor-published spec that other tooling tolerates can still be loaded by a
// strict consumer.
//
// The defect classes handled here are the ones observed in real, widely used
// public specs (Docker Engine, GitLab, Spotify, Square, DigitalOcean,
// PagerDuty, Weaviate): metadata that contradicts the schema it sits on,
// structurally impossible constructs, and references that do not resolve.
// Nothing here changes what an API can do — paths, operations, parameters,
// responses, properties and required semantics are never removed.
//
// Repair is opt-in at the call site and never silent: every change is reported
// as a Warning carrying a stable code and the JSON path of the offending node,
// so a caller can mark the resulting connection as degraded. When a defect
// cannot be repaired safely the document is left alone and the strict error
// stands.
package specfix

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// Warning codes. Each names one repair rule, so a caller can aggregate them
// for display and so evidence produced by a sweep is stable across runs.
const (
	// CodeExampleTypeMismatch reports an `example` whose JSON kind cannot be
	// held by the sibling `type` (e.g. an array example on a string schema).
	CodeExampleTypeMismatch = "example-type-mismatch"
	// CodeDefaultTypeMismatch reports a `default` with the same problem.
	CodeDefaultTypeMismatch = "default-type-mismatch"
	// CodeRequiredBool reports a schema-level `required` that is a boolean
	// instead of an array of property names.
	CodeRequiredBool = "required-bool"
	// CodeArrayMissingItems reports `type: array` with no `items` schema.
	CodeArrayMissingItems = "array-missing-items"
	// CodeDanglingLocalRef reports a local `$ref` whose target does not exist.
	CodeDanglingLocalRef = "dangling-local-ref"
	// CodeNonStringText reports a `description`/`title` that is not a string.
	CodeNonStringText = "non-string-text"
	// CodeNullType reports a schema whose `type` is the literal "null", which
	// is not a legal OpenAPI type name (Swagger 2.0 substitutions and
	// generated specs emit it).
	CodeNullType = "null-type"
	// CodeExampleMissingRequired reports an object example that omits a
	// property the schema lists as required, so the example fails its own
	// schema.
	CodeExampleMissingRequired = "example-missing-required"
	// CodeMalformedRefShape reports a `$ref` that resolves to something that
	// is not an object, so it cannot be a schema/parameter/response.
	CodeMalformedRefShape = "malformed-ref-shape"
	// CodeExternalRef reports an external (non-#) reference. It is recorded
	// but deliberately NOT fetched or rewritten.
	CodeExternalRef = "external-ref"
	// CodeInvalidComponentName reports a components/* entry whose name contains
	// characters OpenAPI 3 rejects. The entry is renamed to a legal name and
	// every reference to it is rewritten, so nothing is dropped.
	CodeInvalidComponentName = "invalid-component-name"
	// CodeUnknownType reports a `type` value outside the Swagger 2.0 type set.
	// The keyword is dropped; the rest of the schema is kept verbatim.
	CodeUnknownType = "unknown-type"
	// CodeNullTypeInArray reports a Swagger 2.0 schema whose `type` is a list
	// containing "null". That spelling is OpenAPI 3.1 syntax; the Swagger 2.0
	// -> 3.0 conversion copies it verbatim, where "null" is not a legal type
	// name. The null alternative is re-expressed through the conversion's own
	// nullability channel (x-nullable).
	CodeNullTypeInArray = "null-type-in-array"
	// CodeExampleNullValue reports a null inside an example where the sibling
	// schema cannot hold null. An example is documentation, so the offending
	// entry is dropped rather than the whole example.
	CodeExampleNullValue = "example-null-value"
	// CodeWrongBucketRef reports a response position that references a
	// components/requestBodies entry. The referenced object is copied into
	// components/responses under the same name and the reference repointed, so
	// the media types the author wrote are preserved exactly.
	CodeWrongBucketRef = "wrong-bucket-ref"
)

// Warning is one repaired (or recorded) defect.
type Warning struct {
	Code   string `json:"code"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// String renders one warning as a single audit-friendly line.
func (w Warning) String() string {
	return fmt.Sprintf("%s at %s: %s", w.Code, w.Path, w.Detail)
}

// Options bounds and filters a repair pass. The zero value is the intended
// default: no cap, every rule enabled.
type Options struct {
	// MaxWarnings caps how many individual warnings are retained. Zero means
	// no cap. Skipped warnings are counted in Result.Suppressed.
	MaxWarnings int
}

// Result is the outcome of a repair pass.
type Result struct {
	// Data is the document to hand to the parser. When no rule fired it is
	// the caller's original bytes, byte for byte.
	Data []byte
	// Warnings lists every repair or recorded defect, in document order.
	Warnings []Warning
	// Changed reports whether Data differs from the input document.
	Changed bool
	// Suppressed counts warnings dropped by Options.MaxWarnings.
	Suppressed int
}

// Codes returns the distinct warning codes with a count, sorted by code, for
// compact operator-facing summaries.
func (r Result) Codes() []CodeCount {
	counts := map[string]int{}
	for _, w := range r.Warnings {
		counts[w.Code]++
	}
	out := make([]CodeCount, 0, len(counts))
	for code, n := range counts {
		out = append(out, CodeCount{Code: code, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// CodeCount is one row of a warning summary.
type CodeCount struct {
	Code  string
	Count int
}

// Repair decodes raw (JSON or YAML), applies every repairable cosmetic fix,
// and returns the document re-encoded as JSON plus the warnings it produced.
//
// An undecodable document is an error: there is nothing cosmetic to fix when
// the bytes cannot be read as a spec at all.
func Repair(raw []byte) (Result, error) {
	return RepairWithOptions(raw, Options{})
}

// RepairWithOptions is Repair with an explicit Options.
func RepairWithOptions(raw []byte, opts Options) (Result, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Result{}, fmt.Errorf("specfix: decode document: %w", err)
	}
	doc = normalize(doc)

	root, ok := doc.(map[string]any)
	if !ok {
		return Result{}, fmt.Errorf("specfix: document root is not a mapping")
	}

	r := &repairer{
		targets:      map[string]bool{},
		nodes:        map[string]any{},
		root:         root,
		swagger2:     isSwagger2Document(root),
		maxWarnings:  opts.MaxWarnings,
		schemaScopes: schemaScopePointers(root),
	}
	// Component names are renamed before anything indexes the document, so
	// every pointer rule below sees the names the parser will validate.
	r.normalizeComponentNames()
	r.nonSchemaScopes = nonSchemaScopePointers(root)
	r.collectTargets(root, "#")
	r.fix(root, "#", "")

	res := Result{Warnings: r.warnings, Changed: r.changed, Suppressed: r.suppressed}
	if !r.changed {
		res.Data = raw
		return res, nil
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return Result{}, fmt.Errorf("specfix: encode repaired document: %w", err)
	}
	res.Data = out
	return res, nil
}

type repairer struct {
	targets  map[string]bool
	nodes    map[string]any
	root     map[string]any
	swagger2 bool

	warnings     []Warning
	changed      bool
	suppressed   int
	maxWarnings  int
	schemaScopes map[string]bool

	// nonSchemaScopes lists component pointers whose objects are not schemas.
	nonSchemaScopes map[string]bool

	// exampleInvalid is set while pruning one example when a null lands on a
	// property its schema marks required; the example cannot be salvaged.
	exampleInvalid bool

	// budget bounds the nodes one reference-inlining walk may visit.
	budget int
}

func (r *repairer) warn(code, path, detail string) {
	w := Warning{Code: code, Path: path, Detail: detail}
	if r.maxWarnings > 0 && len(r.warnings) >= r.maxWarnings {
		r.suppressed++
		return
	}
	r.warnings = append(r.warnings, w)
}

// collectTargets records every JSON pointer that exists in the document, so a
// later pass can tell a resolvable local $ref from a dangling one, and keeps
// the node behind each pointer so a rule can resolve a $ref to the object it
// names.
func (r *repairer) collectTargets(node any, ptr string) {
	switch v := node.(type) {
	case map[string]any:
		r.targets[ptr] = true
		r.nodes[ptr] = v
		for k, child := range v {
			r.collectTargets(child, ptr+"/"+escapePointer(k))
		}
	case []any:
		r.targets[ptr] = true
		r.nodes[ptr] = v
		for i, child := range v {
			r.collectTargets(child, fmt.Sprintf("%s/%d", ptr, i))
		}
	}
}

// isSwagger2Document reports whether the document root declares Swagger 2.0,
// which the engine converts to OpenAPI 3.0 before validating. Rules that exist
// only because of that conversion are gated on it.
func isSwagger2Document(root map[string]any) bool {
	version, _ := root["swagger"].(string)
	return version == "2.0"
}

// fix applies every rule to node and returns the (possibly replaced) node.
// parentKey names the key node was stored under, which decides whether a
// dangling reference may be replaced by an empty schema.
func (r *repairer) fix(node any, ptr, parentKey string) any {
	switch v := node.(type) {
	case map[string]any:
		if replaced, ok := r.fixReference(v, ptr, parentKey); ok {
			return replaced
		}

		// description / title must be strings.
		for _, key := range []string{"description", "title"} {
			val, present := v[key]
			if !present || val == nil {
				continue
			}
			if _, isString := val.(string); !isString {
				delete(v, key)
				r.changed = true
				r.warn(CodeNonStringText, ptr+"/"+escapePointer(key),
					fmt.Sprintf("%s must be a string, found %s", key, kindName(val)))
			}
		}

		if r.isSchemaNode(v, ptr) {
			r.fixNullType(v, ptr)
			r.fixNullTypeInArray(v, ptr)
			r.fixUnknownType(v, ptr)
			r.fixRequired(v, ptr)
			r.fixArrayItems(v, ptr)
			r.fixValues(v, ptr)
			r.fixExampleShape(v, ptr)
			r.fixExampleNulls(v, ptr)
			r.fixExampleValidation(v, ptr)
			r.fixDefaultValidation(v, ptr)
		} else if _, isParameter := v["in"]; isParameter {
			// Swagger 2.0 non-body parameters carry their schema inline.
			r.fixParameterDefault(v, ptr)
		}

		for k, child := range v {
			switch k {
			case "$ref", "example", "default", "enum":
				// Leaf data: never walked into (an example's shape is the
				// author's, not a schema's).
				continue
			}
			v[k] = r.fix(child, ptr+"/"+escapePointer(k), k)
		}
		return v
	case []any:
		for i := range v {
			v[i] = r.fix(v[i], fmt.Sprintf("%s/%d", ptr, i), parentKey)
		}
		return v
	default:
		return node
	}
}

// fixReference decides what to do with a node carrying $ref. It returns the
// replacement node and true when the caller must stop walking this node.
func (r *repairer) fixReference(v map[string]any, ptr, parentKey string) (any, bool) {
	ref, ok := v["$ref"].(string)
	if !ok {
		return nil, false
	}
	if !strings.HasPrefix(ref, "#") {
		r.warn(CodeExternalRef, ptr+"/$ref", "external reference is not fetched: "+ref)
		return nil, false
	}

	// A response position that references the requestBodies bucket cannot
	// resolve: the bucket holds request-body objects. The author's content is
	// preserved exactly — it is copied into components/responses under the
	// same name and the reference repointed — so no media type or schema is
	// invented or dropped.
	if name, ok := requestBodyClaimedAsResponse(ref, ptr); ok {
		if r.promoteRequestBodyToResponse(name) {
			promoted := "#/components/responses/" + name
			v["$ref"] = promoted
			r.changed = true
			r.warn(CodeWrongBucketRef, ptr+"/$ref",
				"response position referenced components/requestBodies/"+name+"; repointed to "+promoted+
					" (same name, content copied unchanged)")
			return nil, false
		}
	}

	if r.targets[ref] {
		return nil, false
	}

	// Dangling local reference. Replacing it with an empty schema is safe
	// where a schema is expected; anywhere else a silent replacement would
	// invent a parameter/response that the document never described, so the
	// reference is only reported and the strict error is allowed to stand.
	if !r.schemaPosition(ptr, parentKey) {
		r.warn(CodeDanglingLocalRef, ptr+"/$ref",
			"reference target does not exist ("+ref+"); left in place because this position is not a schema")
		return nil, false
	}
	r.changed = true
	r.warn(CodeDanglingLocalRef, ptr+"/$ref",
		"reference target does not exist ("+ref+"); replaced with an empty schema")
	return map[string]any{}, true
}

// schemaPosition reports whether a $ref at ptr is being used as a schema, and
// so may be replaced by an empty schema without inventing a new object kind.
func (r *repairer) schemaPosition(ptr, parentKey string) bool {
	switch parentKey {
	case "schema", "items", "additionalProperties", "not", "allOf", "anyOf", "oneOf":
		return true
	case "properties":
		// parentKey is the property name here; the grandparent is properties.
		return strings.Contains(ptr, "/properties/")
	}
	if r.schemaScopes[ptr] {
		return true
	}
	for _, prefix := range []string{"#/components/schemas", "#/definitions"} {
		if strings.HasPrefix(ptr, prefix) {
			return true
		}
	}
	return false
}

// schemaScopePointers marks the node pointers that live under a components
// schemas / definitions map, so a dangling ref anywhere inside one is treated
// as a schema position.
func schemaScopePointers(root map[string]any) map[string]bool {
	scopes := map[string]bool{}
	for _, container := range []struct{ key, child string }{
		{"components", "schemas"},
		{"definitions", ""},
	} {
		var bucket any
		if container.child == "" {
			bucket = root[container.key]
		} else if comps, ok := root[container.key].(map[string]any); ok {
			bucket = comps[container.child]
		}
		schemas, ok := bucket.(map[string]any)
		if !ok {
			continue
		}
		for name := range schemas {
			scopes["#/"+container.key+"/"+escapePointer(container.child)+"/"+escapePointer(name)] = true
		}
	}
	return scopes
}

// nonSchemaScopePointers lists the component objects that are NOT JSON Schemas:
// a security scheme, response, parameter, example, requestBody, header, link or
// callback object legitimately carries its own `type`/`required` keys with
// non-schema meanings (a security scheme's `type` is "oauth2"/"openIdConnect",
// a response's `required` is "required"; a header's is boolean). The schema
// rules must never touch those.
func nonSchemaScopePointers(root map[string]any) map[string]bool {
	scopes := map[string]bool{}
	if comps, ok := root["components"].(map[string]any); ok {
		for bucket, raw := range comps {
			if bucket == "schemas" {
				continue
			}
			entries, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for name := range entries {
				scopes["#/components/"+escapePointer(bucket)+"/"+escapePointer(name)] = true
			}
		}
	}
	if defs, ok := root["securityDefinitions"].(map[string]any); ok {
		for name := range defs {
			scopes["#/securityDefinitions/"+escapePointer(name)] = true
		}
	}
	return scopes
}

// underExtension reports whether a pointer lies inside a specification
// extension (`x-go-type`, `x-nullable`, …). Extension values are vendor
// metadata, not schema objects: a `type` key inside `x-go-type` is a Go type
// name, and the schema rules must not read it as a JSON Schema type.
func underExtension(ptr string) bool {
	for _, segment := range strings.Split(ptr, "/") {
		if strings.HasPrefix(segment, "x-") {
			return true
		}
	}
	return false
}

// isSchemaNode reports whether v looks like a JSON Schema object rather than a
// parameter, request body, response or operation object. Parameter and
// request-body objects legitimately carry a boolean `required`, so the rules
// below must not touch them.
func (r *repairer) isSchemaNode(v map[string]any, ptr string) bool {
	if r.nonSchemaScopes[ptr] || underExtension(ptr) {
		return false
	}
	if _, ok := v["in"]; ok {
		return false // parameter object
	}
	if _, ok := v["name"]; ok {
		return false // parameter / header object
	}
	if _, ok := v["content"]; ok {
		if _, isType := v["type"]; !isType {
			return false // request body / response object
		}
	}
	for _, key := range []string{"type", "properties", "items", "allOf", "anyOf", "oneOf", "enum", "format"} {
		if _, ok := v[key]; ok {
			return true
		}
	}
	return false
}

// fixNullType removes a literal "null" schema type, which no OpenAPI version
// accepts as a type name.
func (r *repairer) fixNullType(v map[string]any, ptr string) {
	typ, ok := v["type"].(string)
	if !ok || !strings.EqualFold(typ, "null") {
		return
	}
	delete(v, "type")
	r.changed = true
	r.warn(CodeNullType, ptr+"/type",
		`"null" is not a legal OpenAPI type name; removed so the schema keeps its other constraints`)
}

// fixNullTypeInArray rewrites a Swagger 2.0 `type` list that includes "null".
// A type list is OpenAPI 3.1 spelling; the engine's 2.0 -> 3.0 conversion
// copies the list verbatim, where "null" is not a legal 3.0 type name and the
// whole document is rejected. Nullability is preserved through the channel the
// conversion does translate (x-nullable -> nullable), so the schema keeps the
// same meaning.
func (r *repairer) fixNullTypeInArray(v map[string]any, ptr string) {
	if !r.swagger2 {
		return
	}
	list, ok := v["type"].([]any)
	if !ok {
		return
	}
	kept := make([]string, 0, len(list))
	found := false
	for _, raw := range list {
		name, isString := raw.(string)
		if !isString {
			return // not a shape this rule understands; leave it alone
		}
		if strings.EqualFold(name, "null") {
			found = true
			continue
		}
		kept = append(kept, name)
	}
	if !found {
		return
	}
	switch len(kept) {
	case 0:
		delete(v, "type")
	case 1:
		v["type"] = kept[0]
	default:
		// More than one real type is a different defect (3.0 has no type
		// unions). Picking a winner would change what the API accepts, so the
		// document is left for the strict error to report.
		return
	}
	if _, hasNullable := v["nullable"]; !hasNullable {
		if _, hasXNullable := v["x-nullable"]; !hasXNullable {
			v["x-nullable"] = true
		}
	}
	r.changed = true
	r.warn(CodeNullTypeInArray, ptr+"/type",
		`"null" in a Swagger 2.0 type list is not a legal 3.0 type name; removed and expressed as x-nullable`)
}

// fixExampleNulls drops null entries from an example where the schema that
// owns them cannot hold null once the document reaches validation.
//
// Examples are documentation: removing a null that the schema rejects never
// changes what the API accepts, and it is the only repair that keeps the rest
// of the example (the alternative — dropping the whole example — loses more).
func (r *repairer) fixExampleNulls(v map[string]any, ptr string) {
	example, ok := v["example"]
	if !ok || example == nil {
		return
	}
	switch example.(type) {
	case map[string]any, []any:
	default:
		return
	}
	r.exampleInvalid = false
	fixed, removed := r.pruneExampleNulls(v, example, ptr+"/example", false)
	if r.exampleInvalid {
		// A null on a REQUIRED property cannot be repaired by dropping the
		// entry: the example would then be missing a property the schema
		// demands. The example is the only thing that can go.
		delete(v, "example")
		r.changed = true
		return
	}
	if removed {
		v["example"] = fixed
	}
}

// pruneExampleNulls walks one example value against the schema that owns it,
// returning the value to store and whether anything was dropped. The schema is
// followed through local $ref so a referenced property schema is honoured.
func (r *repairer) pruneExampleNulls(schema map[string]any, val any, ptr string, viaAdditional bool) (any, bool) {
	switch example := val.(type) {
	case map[string]any:
		removed := false
		for key, child := range example {
			sub, subViaAdditional, ok := r.exampleSubschema(schema, key)
			if !ok {
				continue
			}
			if child == nil {
				if !r.exampleNullRejected(sub, subViaAdditional) {
					continue
				}
				if schemaRequires(schema, key) {
					r.exampleInvalid = true
					r.changed = true
					r.warn(CodeExampleNullValue, ptr+"/"+escapePointer(key),
						"example value is null for a required property the schema declares "+declaredTypeLabel(sub)+
							"; the whole example is dropped rather than leaving the required property absent")
					return example, removed
				}
				delete(example, key)
				removed = true
				r.changed = true
				r.warn(CodeExampleNullValue, ptr+"/"+escapePointer(key),
					"example value is null but the schema declares "+declaredTypeLabel(sub))
				continue
			}
			fixed, childRemoved := r.pruneExampleNulls(sub, child, ptr+"/"+escapePointer(key), subViaAdditional)
			if childRemoved {
				example[key] = fixed
				removed = true
			}
		}
		return example, removed
	case []any:
		items, ok := r.resolveSchema(schema["items"])
		if !ok {
			return example, false
		}
		kept := make([]any, 0, len(example))
		removed := false
		for i, child := range example {
			at := fmt.Sprintf("%s/%d", ptr, i)
			if child == nil {
				if !r.exampleNullRejected(items, false) {
					kept = append(kept, child)
					continue
				}
				removed = true
				r.changed = true
				r.warn(CodeExampleNullValue, at,
					"example element is null but the schema declares "+declaredTypeLabel(items))
				continue
			}
			fixed, childRemoved := r.pruneExampleNulls(items, child, at, false)
			if childRemoved {
				removed = true
			}
			kept = append(kept, fixed)
		}
		if !removed {
			return example, false
		}
		return kept, true
	default:
		return val, false
	}
}

// exampleNullRejected reports whether a null at this position is certainly
// rejected: the schema declares a concrete type and no nullability flag covers
// it. An untyped schema constrains nothing locally, so the example is left
// alone rather than guessed about.
func (r *repairer) exampleNullRejected(schema map[string]any, viaAdditional bool) bool {
	if _, typed := concreteType(schema); !typed {
		return false
	}
	return !permitsNull(schema, viaAdditional)
}

// exampleSubschema returns the schema that governs property key of an object
// schema, and whether it was reached through additionalProperties.
func (r *repairer) exampleSubschema(schema map[string]any, key string) (map[string]any, bool, bool) {
	if properties, ok := schema["properties"].(map[string]any); ok {
		if child, exists := properties[key]; exists {
			if sub, ok := r.resolveSchema(child); ok {
				return sub, false, true
			}
			return nil, false, false
		}
	}
	if additional, exists := schema["additionalProperties"]; exists {
		if sub, ok := r.resolveSchema(additional); ok {
			return sub, true, true
		}
	}
	return nil, false, false
}

// resolveSchema follows local $ref hops to the schema object they name. The
// hop count is bounded so a reference cycle cannot hang the repair pass.
func (r *repairer) resolveSchema(node any) (map[string]any, bool) {
	v, ok := node.(map[string]any)
	if !ok {
		return nil, false
	}
	for hops := 0; hops < 8; hops++ {
		ref, hasRef := v["$ref"].(string)
		if !hasRef {
			return v, true
		}
		if !strings.HasPrefix(ref, "#") {
			return nil, false
		}
		target, exists := r.nodes[ref]
		if !exists {
			return nil, false
		}
		next, isMap := target.(map[string]any)
		if !isMap {
			return nil, false
		}
		v = next
	}
	return nil, false
}

// concreteType reports the single non-null type a schema declares.
func concreteType(schema map[string]any) (string, bool) {
	switch types := schema["type"].(type) {
	case string:
		if types != "" && !strings.EqualFold(types, "null") {
			return types, true
		}
	case []any:
		for _, raw := range types {
			name, ok := raw.(string)
			if ok && name != "" && !strings.EqualFold(name, "null") {
				return name, true
			}
		}
	}
	return "", false
}

// permitsNull reports whether the converted schema can hold a JSON null.
//
// The conversion translates x-nullable into `nullable` only for schemas it
// walks through openapi2conv.ToV3SchemaRef (definitions, properties, items,
// allOf). A schema reached through additionalProperties is copied by
// toV3AdditionalProperties, which leaves x-nullable as an extension and never
// sets nullable — so a null there is rejected by validation even though the
// source says x-nullable. That asymmetry is why viaAdditional matters.
func permitsNull(schema map[string]any, viaAdditional bool) bool {
	if nullable, _ := schema["nullable"].(bool); nullable {
		return true
	}
	if !viaAdditional {
		if xNullable, _ := schema["x-nullable"].(bool); xNullable {
			return true
		}
	}
	switch types := schema["type"].(type) {
	case string:
		return strings.EqualFold(types, "null")
	case []any:
		for _, raw := range types {
			if name, ok := raw.(string); ok && strings.EqualFold(name, "null") {
				return true
			}
		}
	}
	return false
}

// declaredTypeLabel names the type in a warning so the audit line says what the
// example violated.
func declaredTypeLabel(schema map[string]any) string {
	if typ, ok := concreteType(schema); ok {
		return "type " + typ
	}
	return "a typed schema"
}

// requestBodyClaimedAsResponse reports the component name when a $ref sitting
// in a response position points into components/requestBodies.
func requestBodyClaimedAsResponse(ref, ptr string) (string, bool) {
	const prefix = "#/components/requestBodies/"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	// components/responses/<name> is itself a response object, not a response
	// position; only a slot inside a responses map qualifies.
	if strings.HasPrefix(ptr, "#/components/") {
		return "", false
	}
	segments := strings.Split(ptr, "/")
	if len(segments) < 2 || segments[len(segments)-2] != "responses" {
		return "", false
	}
	return strings.TrimPrefix(ref, prefix), true
}

// promoteRequestBodyToResponse makes the referenced request-body object
// available as a response of the same name. The content is copied unchanged;
// when the response already exists the copy is skipped and the reference is
// simply repointed.
func (r *repairer) promoteRequestBodyToResponse(name string) bool {
	body, ok := r.nodes["#/components/requestBodies/"+name].(map[string]any)
	if !ok {
		return false
	}
	responsePtr := "#/components/responses/" + name
	if _, exists := r.nodes[responsePtr]; exists {
		return true
	}
	components, ok := r.root["components"].(map[string]any)
	if !ok {
		return false
	}
	responses, ok := components["responses"].(map[string]any)
	if !ok {
		if _, present := components["responses"]; present {
			return false // unexpected shape; leave the document alone
		}
		responses = map[string]any{}
		components["responses"] = responses
		r.nodes["#/components/responses"] = responses
		r.targets["#/components/responses"] = true
	}
	response := map[string]any{"description": ""}
	if description, isString := body["description"].(string); isString && description != "" {
		response["description"] = description
	}
	if content, present := body["content"]; present {
		response["content"] = content
	}
	responses[unescapePointer(name)] = response
	r.nodes[responsePtr] = response
	r.targets[responsePtr] = true
	return true
}

// normalizeComponentNames renames components/* entries whose names OpenAPI 3
// rejects (the spec allows only [a-zA-Z0-9._-]) and rewrites every reference to
// them, so a document whose generator derived a name from a path template
// (GitLab's "postApiV4ProjectsId(refRef)triggerPipeline") still loads.
//
// Renaming is metadata-only: the entry keeps its content, and every $ref that
// pointed at it points at the new name.
func (r *repairer) normalizeComponentNames() {
	type bucketRef struct {
		entries map[string]any
		prefix  string // reference prefix, e.g. "#/definitions/"
		path    string // warning path of the bucket
	}
	var buckets []bucketRef
	if components, ok := r.root["components"].(map[string]any); ok {
		for bucket, raw := range components {
			if entries, ok := raw.(map[string]any); ok {
				buckets = append(buckets, bucketRef{
					entries: entries,
					prefix:  "#/components/" + escapePointer(bucket) + "/",
					path:    "#/components/" + escapePointer(bucket),
				})
			}
		}
	}
	// Swagger 2.0 has no components: its definitions become components/schemas
	// after the engine's conversion, so an illegal name there is rejected at
	// exactly the same validation step.
	if r.swagger2 {
		if defs, ok := r.root["definitions"].(map[string]any); ok {
			buckets = append(buckets, bucketRef{entries: defs, prefix: "#/definitions/", path: "#/definitions"})
		}
	}

	renames := map[string]map[string]string{} // prefix -> escaped old -> new
	for _, bucket := range buckets {
		for name := range bucket.entries {
			if validComponentName(name) {
				continue
			}
			newName := sanitizeComponentName(name, bucket.entries)
			renames[bucket.prefix] = orNewMap(renames[bucket.prefix])
			renames[bucket.prefix][escapePointer(name)] = newName
		}
	}
	if len(renames) == 0 {
		return
	}

	// Rename the keys first, then repoint every reference in the document.
	for _, bucket := range buckets {
		mapping, ok := renames[bucket.prefix]
		if !ok {
			continue
		}
		for oldEscaped, newName := range mapping {
			oldName := unescapePointer(oldEscaped)
			entry := bucket.entries[oldName]
			delete(bucket.entries, oldName)
			bucket.entries[newName] = entry
			r.changed = true
			r.warn(CodeInvalidComponentName, bucket.path+"/"+oldEscaped,
				"component name is not valid OpenAPI 3 ("+oldName+"); renamed to "+newName+" and references repointed")
		}
	}
	r.repointRefs(renames)
}

// repointRefs rewrites $ref values that name a renamed component.
func (r *repairer) repointRefs(renames map[string]map[string]string) {
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok && strings.HasPrefix(ref, "#") {
				for prefix, mapping := range renames {
					if !strings.HasPrefix(ref, prefix) {
						continue
					}
					if newName, hit := mapping[strings.TrimPrefix(ref, prefix)]; hit {
						v["$ref"] = prefix + newName
						r.changed = true
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(r.root)
}

// validComponentName reports whether name is legal for a components/* key.
func validComponentName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// sanitizeComponentName maps every invalid character to '_' and, on a
// collision, appends a numeric suffix so two entries never collapse.
func sanitizeComponentName(name string, taken map[string]any) string {
	var b strings.Builder
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	base := b.String()
	if base == "" {
		base = "component"
	}
	if _, clash := taken[base]; !clash {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if _, clash := taken[candidate]; !clash {
			return candidate
		}
	}
}

func orNewMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// schemaRequires reports whether the object schema lists key in `required`.
func schemaRequires(schema map[string]any, key string) bool {
	required, ok := schema["required"].([]any)
	if !ok {
		return false
	}
	for _, raw := range required {
		if name, isString := raw.(string); isString && name == key {
			return true
		}
	}
	return false
}

// fixUnknownType drops a `type` value that is not part of the Swagger 2.0 type
// set on a document the engine validates after conversion.
//
// A value like "text" (a database/DTO type that leaked into the spec) is not a
// legal type at all, so nothing is lost that the conversion could honour; the
// key is dropped instead of guessed at ("text" is most likely string, but
// picking a type would be a contract change the author never wrote). Every other
// constraint on the schema — properties, items, format, enum — is untouched.
func (r *repairer) fixUnknownType(v map[string]any, ptr string) {
	if !r.swagger2 {
		return
	}
	name, ok := v["type"].(string)
	if !ok {
		return
	}
	if swagger2Types[strings.ToLower(name)] {
		return
	}
	delete(v, "type")
	r.changed = true
	r.warn(CodeUnknownType, ptr+"/type",
		`"`+name+`" is not a Swagger 2.0 type; the type keyword is dropped and the rest of the schema is kept`)
}

// swagger2Types is the Swagger 2.0 type set (JSON Schema draft 4 plus the
// Swagger-specific "file").
var swagger2Types = map[string]bool{
	"array":   true,
	"boolean": true,
	"integer": true,
	"number":  true,
	"object":  true,
	"string":  true,
	"file":    true,
}

// fixExampleValidation drops an example that its own schema rejects — an enum
// value outside the enumeration, an integer outside its format's range, a
// string that is not a date-time, and so on.
//
// This is the general form of the example rules above: an example is
// documentation, so an example that contradicts the schema it documents is
// removed rather than the schema being loosened. The schema itself is never
// changed, and a schema this rule cannot evaluate (it contains a $ref, which
// cannot be resolved from the node alone, or a shape kin-openapi refuses to
// unmarshal) is left entirely alone — a repair is only made when the verdict
// is certain.
func (r *repairer) fixExampleValidation(v map[string]any, ptr string) {
	example, ok := v["example"]
	if !ok || example == nil {
		return
	}
	verr, judged := r.validateValue(v, example)
	if !judged || verr == nil {
		return
	}
	delete(v, "example")
	r.changed = true
	r.warn(CodeExampleTypeMismatch, ptr+"/example",
		"example does not satisfy its own schema: "+firstLine(verr.Error()))
}

// fixDefaultValidation drops a default value the schema would reject. A default
// is the author's suggested value, so one that fails its own schema cannot be
// honoured by the generated client anyway; the schema is never loosened.
func (r *repairer) fixDefaultValidation(v map[string]any, ptr string) {
	if _, present := v["default"]; !present {
		return
	}
	def := v["default"]
	if def == nil {
		return
	}
	verr, judged := r.validateValue(v, def)
	if !judged || verr == nil {
		return
	}
	delete(v, "default")
	r.changed = true
	r.warn(CodeDefaultTypeMismatch, ptr+"/default",
		"default does not satisfy its own schema: "+firstLine(verr.Error()))
}

// validateValue checks value against the schema at v, with local references
// inlined so a $ref-heavy schema is judged as the parser will judge it. It
// reports judged=false when the verdict cannot be trusted: the tree could not
// be fully inlined (cycle, external target, budget) or kin-openapi refuses the
// shape. A repair is only made on a certain verdict.
func (r *repairer) validateValue(v map[string]any, value any) (error, bool) {
	schemaCopy := make(map[string]any, len(v))
	for key, val := range v {
		switch key {
		case "example", "examples", "default":
			continue
		}
		schemaCopy[key] = val
	}
	r.budget = resolveBudget
	resolved := r.resolveDeep(schemaCopy, 0)
	if hasRefDeep(resolved, 0) {
		return nil, false
	}
	return visitJSONSchema(resolved, value)
}

// fixParameterDefault drops a parameter-level default the parameter's own type
// would reject (Swagger 2.0 keeps a non-body parameter's schema inline, so
// `type`/`items`/`enum` sit beside `name` and `in` rather than under `schema`).
func (r *repairer) fixParameterDefault(v map[string]any, ptr string) {
	if _, hasIn := v["in"]; !hasIn {
		return
	}
	def, present := v["default"]
	if !present || def == nil {
		return
	}
	schema := make(map[string]any, len(v))
	for key, val := range v {
		switch key {
		case "name", "in", "required", "description", "collectionFormat",
			"allowEmptyValue", "example", "examples", "default":
			continue
		}
		schema[key] = val
	}
	if len(schema) == 0 {
		return
	}
	verr, judged := visitJSONSchema(schema, def)
	if !judged || verr == nil {
		return
	}
	delete(v, "default")
	r.changed = true
	r.warn(CodeDefaultTypeMismatch, ptr+"/default",
		"parameter default does not satisfy the parameter's own type: "+firstLine(verr.Error()))
}

// visitJSONSchema validates value against a plain schema document through the
// same library the parser validates with.
func visitJSONSchema(schemaDoc any, value any) (error, bool) {
	raw, err := json.Marshal(schemaDoc)
	if err != nil {
		return nil, false
	}
	var schema openapi3.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, false
	}
	return schema.VisitJSON(value), true
}

// resolveBudget bounds the work one example check may spend inlining
// references, so a pathological document cannot turn a repair pass quadratic.
const resolveBudget = 200000

// resolveDeep returns a copy of node with every local $ref replaced by the
// object it names. refDepth counts only the references inlined along one path
// — the cycle guard — so an ordinary deep property chain is never truncated.
// When the reference chain is too long or the node budget is spent, the node is
// returned unchanged (still holding its reference), which the caller detects
// and treats as unverifiable.
func (r *repairer) resolveDeep(node any, refDepth int) any {
	switch v := node.(type) {
	case map[string]any:
		if ref, hasRef := v["$ref"].(string); hasRef {
			if refDepth >= 8 || r.budget <= 0 || !strings.HasPrefix(ref, "#") {
				return v
			}
			target, exists := r.nodes[ref]
			if !exists {
				return v
			}
			r.budget--
			return r.resolveDeep(target, refDepth+1)
		}
		r.budget--
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == "example" || key == "examples" {
				out[key] = child
				continue
			}
			out[key] = r.resolveDeep(child, refDepth)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = r.resolveDeep(child, refDepth)
		}
		return out
	default:
		return node
	}
}

// hasRefDeep reports whether any node in the tree still carries a $ref.
func hasRefDeep(node any, depth int) bool {
	if depth > 64 {
		return true
	}
	switch v := node.(type) {
	case map[string]any:
		if _, hasRef := v["$ref"]; hasRef {
			return true
		}
		for _, child := range v {
			if hasRefDeep(child, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasRefDeep(child, depth+1) {
				return true
			}
		}
	}
	return false
}

// firstLine trims a validation error to its first line so a warning stays one
// audit-friendly line.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// fixExampleShape drops an example that fails the schema it is attached to:
// an object example omitting a required property, or an array example whose
// elements omit one required by the items schema.
func (r *repairer) fixExampleShape(v map[string]any, ptr string) {
	if example, ok := v["example"].(map[string]any); ok {
		required, ok := v["required"].([]any)
		if !ok {
			return
		}
		missing := missingRequired(example, required)
		if len(missing) == 0 {
			return
		}
		delete(v, "example")
		r.changed = true
		r.warn(CodeExampleMissingRequired, ptr+"/example",
			"example omits required propert(y/ies) "+strings.Join(missing, ", "))
		return
	}

	// An array-level example is validated element by element against the items
	// schema, which may itself carry `required`. The example is the author's
	// documentation, so an element that omits a required property drops the
	// example — no schema or operation is touched.
	if typ, _ := v["type"].(string); typ != "array" {
		return
	}
	elements, ok := v["example"].([]any)
	if !ok {
		return
	}
	items, ok := r.resolveSchema(v["items"])
	if !ok {
		return
	}
	required, ok := items["required"].([]any)
	if !ok {
		return
	}
	for _, element := range elements {
		object, isObject := element.(map[string]any)
		if !isObject {
			continue
		}
		if missing := missingRequired(object, required); len(missing) > 0 {
			delete(v, "example")
			r.changed = true
			r.warn(CodeExampleMissingRequired, ptr+"/example",
				"array example element omits required propert(y/ies) "+strings.Join(missing, ", "))
			return
		}
	}
}

// missingRequired lists the required property names object omits, sorted.
func missingRequired(object map[string]any, required []any) []string {
	var missing []string
	for _, raw := range required {
		name, isString := raw.(string)
		if !isString {
			continue
		}
		if _, present := object[name]; !present {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func (r *repairer) fixRequired(v map[string]any, ptr string) {
	req, ok := v["required"]
	if !ok {
		return
	}
	if _, isBool := req.(bool); !isBool {
		return
	}
	delete(v, "required")
	r.changed = true
	r.warn(CodeRequiredBool, ptr+"/required",
		"required must be an array of property names, found a boolean")
}

func (r *repairer) fixArrayItems(v map[string]any, ptr string) {
	if typ, _ := v["type"].(string); typ != "array" {
		return
	}
	items, ok := v["items"]
	if ok && items != nil {
		return
	}
	v["items"] = map[string]any{}
	r.changed = true
	r.warn(CodeArrayMissingItems, ptr+"/items",
		"array schema declares no items schema; replaced with an empty schema")
}

func (r *repairer) fixValues(v map[string]any, ptr string) {
	typ, _ := v["type"].(string)
	if typ == "" {
		return
	}
	for _, key := range []string{"example", "default"} {
		val, ok := v[key]
		if !ok || val == nil {
			continue
		}
		if kindMatches(typ, val) {
			continue
		}
		code := CodeExampleTypeMismatch
		if key == "default" {
			code = CodeDefaultTypeMismatch
		}
		delete(v, key)
		r.changed = true
		r.warn(code, ptr+"/"+key,
			fmt.Sprintf("%s %s cannot satisfy declared type %q", key, kindName(val), typ))
	}
}

// normalize converts the map[any]any values a YAML decoder can produce into
// map[string]any so the walker only ever meets one shape.
func normalize(node any) any {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			v[k] = normalize(child)
		}
		return v
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[fmt.Sprintf("%v", k)] = normalize(child)
		}
		return out
	case []any:
		for i := range v {
			v[i] = normalize(v[i])
		}
		return v
	default:
		return node
	}
}

// kindMatches reports whether a declared OpenAPI type can hold v. "integer" is
// satisfied by an integral number because JSON has no integer type of its own.
func kindMatches(declared string, v any) bool {
	got := jsonKind(v)
	if declared == got {
		return true
	}
	if declared == "integer" {
		if f, ok := asFloat(v); ok {
			return f == math.Trunc(f)
		}
	}
	return false
}

func jsonKind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return ""
	}
}

func kindName(v any) string {
	if k := jsonKind(v); k != "" {
		return k
	}
	return fmt.Sprintf("%T", v)
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// escapePointer applies RFC 6901 escaping so pointers round-trip keys that
// contain "/" or "~".
func escapePointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// unescapePointer reverses escapePointer.
func unescapePointer(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}
