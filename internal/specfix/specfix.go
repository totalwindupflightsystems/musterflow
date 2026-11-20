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
		maxWarnings:  opts.MaxWarnings,
		schemaScopes: schemaScopePointers(root),
	}
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
	targets      map[string]bool
	warnings     []Warning
	changed      bool
	suppressed   int
	maxWarnings  int
	schemaScopes map[string]bool
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
// later pass can tell a resolvable local $ref from a dangling one.
func (r *repairer) collectTargets(node any, ptr string) {
	switch v := node.(type) {
	case map[string]any:
		r.targets[ptr] = true
		for k, child := range v {
			r.collectTargets(child, ptr+"/"+escapePointer(k))
		}
	case []any:
		r.targets[ptr] = true
		for i, child := range v {
			r.collectTargets(child, fmt.Sprintf("%s/%d", ptr, i))
		}
	}
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

		if r.isSchemaNode(v) {
			r.fixNullType(v, ptr)
			r.fixRequired(v, ptr)
			r.fixArrayItems(v, ptr)
			r.fixValues(v, ptr)
			r.fixExampleShape(v, ptr)
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

// isSchemaNode reports whether v looks like a JSON Schema object rather than a
// parameter, request body, response or operation object. Parameter and
// request-body objects legitimately carry a boolean `required`, so the rules
// below must not touch them.
func (r *repairer) isSchemaNode(v map[string]any) bool {
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

// fixExampleShape drops an object example that omits a property its own schema
// marks required — the example fails the schema it is attached to, which is a
// documentation defect and never a contract change.
func (r *repairer) fixExampleShape(v map[string]any, ptr string) {
	example, ok := v["example"].(map[string]any)
	if !ok {
		return
	}
	required, ok := v["required"].([]any)
	if !ok {
		return
	}
	var missing []string
	for _, raw := range required {
		name, isString := raw.(string)
		if !isString {
			continue
		}
		if _, present := example[name]; !present {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	delete(v, "example")
	r.changed = true
	r.warn(CodeExampleMissingRequired, ptr+"/example",
		"example omits required propert(y/ies) "+strings.Join(missing, ", "))
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
