package app

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDefect names the construct that stops a spec from becoming a working
// command tree. It carries the three things an operator needs to fix the
// document: which spec, which JSON pointer inside it, and why.
type LoadDefect struct {
	// Spec is the spec source as the operator named it (file path or URL).
	Spec string
	// Pointer is the JSON pointer of the offending node, e.g.
	// "/paths/~1items~1{itemId}/get/parameters". Empty when the document
	// shape is broken somewhere this diagnosis cannot localise.
	Pointer string
	// Reason states what is wrong in operator terms.
	Reason string
	// Cause is the underlying parser error, when there is one.
	Cause error
}

func (d *LoadDefect) Error() string {
	if d == nil {
		return "spec cannot be loaded"
	}
	var b strings.Builder
	if d.Spec != "" {
		b.WriteString("spec ")
		b.WriteString(d.Spec)
		b.WriteString(" cannot be loaded into a command tree: ")
	} else {
		b.WriteString("spec cannot be loaded into a command tree: ")
	}
	if d.Pointer != "" {
		b.WriteString(d.Pointer)
	} else {
		b.WriteString("(pointer unknown)")
	}
	b.WriteString(": ")
	b.WriteString(d.Reason)
	if d.Cause != nil {
		b.WriteString(" — parser: ")
		b.WriteString(d.Cause.Error())
	}
	return b.String()
}

// Unwrap exposes the parser error so errors.Is/As still work for callers that
// classify parse failures.
func (d *LoadDefect) Unwrap() error {
	if d == nil {
		return nil
	}
	return d.Cause
}

// orderedMethods is the traversal order for operations inside one path item.
// Fixed order keeps the reported defect deterministic across runs (the YAML
// decode produces a map, whose iteration order is not stable).
var orderedMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// DiagnoseLoadFailure inspects a raw OpenAPI document and returns the first
// construct known to stop it from producing a callable command tree, with the
// JSON pointer of the offending node.
//
// The rules here are deliberately narrow: each one corresponds to a defect
// class this project has seen block loading (or produce an empty command tree)
// in a real document. When nothing matches, the caller still reports the file
// and the parser's own reason — the pointer is then honestly reported as
// unknown rather than guessed.
func DiagnoseLoadFailure(data []byte) *LoadDefect {
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return &LoadDefect{Reason: "document is not valid YAML/JSON", Cause: err}
	}

	doc, ok := asMap(root)
	if !ok {
		return &LoadDefect{Reason: "document root is not a mapping, so it is not an OpenAPI document"}
	}

	paths, hasPaths := asMap(doc["paths"])
	pathPointers := sortedKeys(paths)
	callable := 0
	for _, p := range pathPointers {
		item, ok := asMap(paths[p])
		if !ok {
			return &LoadDefect{
				Pointer: "/paths/" + escapePointer(p),
				Reason:  "path item must be a mapping of operations",
			}
		}
		for _, method := range orderedMethods {
			raw, present := item[method]
			if !present {
				continue
			}
			callable++
			if defect := diagnoseOperation("/paths/"+escapePointer(p)+"/"+method, raw); defect != nil {
				return defect
			}
		}
	}

	if callable == 0 {
		if _, hasWebhooks := asMap(doc["webhooks"]); hasWebhooks {
			return &LoadDefect{
				Pointer: "/webhooks",
				Reason: "OpenAPI 3.1 webhooks are not callable operations — no CLI commands can be generated from them; " +
					"declare the operations under paths instead",
			}
		}
		if !hasPaths {
			return &LoadDefect{
				Pointer: "/paths",
				Reason:  "document has no paths section, so no CLI commands can be generated",
			}
		}
		return &LoadDefect{
			Pointer: "/paths",
			Reason:  "document declares no path operations, so no CLI commands can be generated",
		}
	}

	// Components are only inspected when every callable operation looked fine;
	// a schema shape the parser rejects blocks the whole document.
	if components, ok := asMap(doc["components"]); ok {
		if schemas, ok := asMap(components["schemas"]); ok {
			for _, name := range sortedKeys(schemas) {
				if _, ok := asMap(schemas[name]); !ok {
					return &LoadDefect{
						Pointer: "/components/schemas/" + escapePointer(name),
						Reason:  "schema must be a mapping",
					}
				}
			}
		}
	}

	return nil
}

// diagnoseOperation checks the shapes the parser cannot represent inside one
// operation object.
func diagnoseOperation(ptr string, raw any) *LoadDefect {
	op, ok := asMap(raw)
	if !ok {
		return &LoadDefect{Pointer: ptr, Reason: "operation must be a mapping"}
	}

	if params, present := op["parameters"]; present {
		if _, ok := params.([]any); !ok {
			return &LoadDefect{
				Pointer: ptr + "/parameters",
				Reason: fmt.Sprintf("parameters must be an array of parameter objects — found %s "+
					"(a mapping keyed by parameter name is a common hand-written mistake)", kindOf(params)),
			}
		}
	}
	if responses, present := op["responses"]; present {
		if _, ok := asMap(responses); !ok {
			return &LoadDefect{
				Pointer: ptr + "/responses",
				Reason:  fmt.Sprintf("responses must be a mapping of status codes to response objects — found %s", kindOf(responses)),
			}
		}
	}
	if body, present := op["requestBody"]; present {
		if _, ok := asMap(body); !ok {
			return &LoadDefect{
				Pointer: ptr + "/requestBody",
				Reason:  fmt.Sprintf("requestBody must be a mapping — found %s", kindOf(body)),
			}
		}
	}
	if id, present := op["operationId"]; present {
		if _, ok := id.(string); !ok {
			return &LoadDefect{
				Pointer: ptr + "/operationId",
				Reason:  fmt.Sprintf("operationId must be a string — found %s", kindOf(id)),
			}
		}
	}
	return nil
}

// DiagnoseEmptyPlan explains an empty command tree from a plan alone (the load
// path has no raw document to inspect). It is the load-time counterpart of
// DiagnoseLoadFailure and is only reached if a plan slipped through connect
// without operations.
func DiagnoseEmptyPlan(plan *SpecPlan) *LoadDefect {
	if plan != nil && plan.SpecURL != "" {
		return &LoadDefect{
			Spec:    plan.SpecURL,
			Pointer: "/paths",
			Reason:  "cached command plan has no operations; re-run 'musterflow connect' to rebuild it from the source spec",
		}
	}
	return &LoadDefect{Pointer: "/paths", Reason: "no operations to build commands from"}
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// kindOf names a decoded YAML/JSON value's shape for operator-facing messages.
func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "mapping"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	default:
		return "scalar"
	}
}

// escapePointer escapes one JSON pointer reference token (RFC 6901).
func escapePointer(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}
