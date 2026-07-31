// Package workflow provides the workflow engine for MusterFlow.
// Workflows are Starlark scripts that chain API calls, with webhook trigger support.
package workflow

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wojons/muster/pkg/dsl"
	"go.starlark.net/starlark"
)

// Flow represents a named workflow.
type Flow struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`  // Starlark source code
	Webhook     bool   `json:"webhook"` // has webhook trigger
	WebhookURL  string `json:"webhook_url,omitempty"`
}

// Engine manages workflow storage and execution.
type Engine struct {
	dir     string
	baseURL string // for webhook URL generation
}

// NewEngine creates a workflow engine storing flows at the given directory.
func NewEngine(dir, baseURL string) *Engine {
	return &Engine{dir: dir, baseURL: baseURL}
}

// Create writes a new flow file and returns the flow.
// If webhook is true, a webhook trigger is created at /hooks/<name>.
func (e *Engine) Create(name, source string, webhook bool) (*Flow, error) {
	if err := os.MkdirAll(e.dir, 0755); err != nil {
		return nil, fmt.Errorf("create flows dir: %w", err)
	}

	flowPath := filepath.Join(e.dir, name+".star")
	if err := os.WriteFile(flowPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("write flow: %w", err)
	}

	flow := &Flow{
		Name:       name,
		Source:     source,
		Webhook:    webhook,
		WebhookURL: "",
	}
	if webhook {
		flow.WebhookURL = fmt.Sprintf("%s/hooks/%s", e.baseURL, name)
	}

	return flow, nil
}

// List returns all flows in the flows directory.
func (e *Engine) List() ([]Flow, error) {
	if err := os.MkdirAll(e.dir, 0755); err != nil {
		return nil, fmt.Errorf("create flows dir: %w", err)
	}

	entries, err := os.ReadDir(e.dir)
	if err != nil {
		return nil, fmt.Errorf("read flows dir: %w", err)
	}

	var flows []Flow
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".star" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(e.dir, entry.Name()))
		if err != nil {
			continue
		}
		name := entry.Name()[:len(entry.Name())-5] // strip .star
		flows = append(flows, Flow{
			Name:       name,
			Source:     string(data),
			WebhookURL: fmt.Sprintf("%s/hooks/%s", e.baseURL, name),
		})
	}
	if flows == nil {
		flows = []Flow{}
	}
	return flows, nil
}

// Run executes a flow by name with the given trigger payload.
// The flow source is parsed and compiled as Starlark via muster's pkg/dsl
// Interpreter and executed with a thread whose Print callback captures all
// print() output. The trigger payload is exposed to the flow as a predeclared
// global named "trigger": a starlark.Dict when triggerPayload is non-nil, or
// None when triggerPayload is nil (so flows can guard with `if trigger !=
// None`). The captured print output (with trailing newlines trimmed) is
// returned as the result string.
func (e *Engine) Run(name string, triggerPayload map[string]interface{}) (string, error) {
	flowPath := filepath.Join(e.dir, name+".star")
	source, err := os.ReadFile(flowPath)
	if err != nil {
		return "", fmt.Errorf("read flow %s: %w", name, err)
	}

	interp := dsl.NewInterpreter(nil)

	ast, err := interp.Parse(string(source), name+".star")
	if err != nil {
		return "", fmt.Errorf("parse flow %s: %w", name, err)
	}

	// Compile the parsed AST into a starlark Program. We use starlark.FileProgram
	// directly because the muster Interpreter.Compile re-reads the source from
	// ast.Path (a filename) via starlark.SourceProgram, which fails for in-memory
	// flow sources. FileProgram accepts the already-parsed AST. The isPredeclared
	// callback returns true for "trigger" so the compiler treats it as a
	// predeclared global whose binding is supplied by program.Init.
	program, err := starlark.FileProgram(ast, func(name string) bool {
		return name == "trigger"
	})
	if err != nil {
		return "", fmt.Errorf("compile flow %s: %w", name, err)
	}

	// Build the predeclared globals for execution. We create a fresh dict
	// containing only "trigger"; the interpreter is constructed with no
	// builtins (nil), so there is nothing else to carry over.
	predeclared := starlark.StringDict{
		"trigger": triggerToStarlark(triggerPayload),
	}

	// Execute with our own thread so we can capture print() output.
	var out bytes.Buffer
	thread := &starlark.Thread{
		Name: "flow:" + name,
		Print: func(_ *starlark.Thread, msg string) {
			fmt.Fprintln(&out, msg)
		},
	}

	if _, err := program.Init(thread, predeclared); err != nil {
		return "", fmt.Errorf("exec flow %s: %w", name, err)
	}

	return strings.TrimRight(out.String(), "\n"), nil
}

// triggerToStarlark converts a trigger payload map into a starlark value.
// A nil map becomes starlark.None so flows can guard with `if trigger != None`.
// A non-nil map becomes a starlark.Dict keyed by string. Nested maps and slices
// are converted recursively; scalars (string, int, float, bool) are mapped to
// their starlark equivalents. Unknown value types fall back to their string
// representation so a payload never breaks flow execution.
func triggerToStarlark(payload map[string]interface{}) starlark.Value {
	if payload == nil {
		return starlark.None
	}
	dict := starlark.NewDict(len(payload))
	for k, v := range payload {
		if err := dict.SetKey(starlark.String(k), toStarlarkValue(v)); err != nil {
			// SetKey on a fresh dict only fails on duplicate keys, which
			// cannot happen for a Go map. Treat as best-effort and skip.
			continue
		}
	}
	return dict
}

// toStarlarkValue converts a Go value to the closest starlark equivalent.
func toStarlarkValue(v interface{}) starlark.Value {
	switch val := v.(type) {
	case nil:
		return starlark.None
	case string:
		return starlark.String(val)
	case bool:
		return starlark.Bool(val)
	case int:
		return starlark.MakeInt(val)
	case int64:
		return starlark.MakeInt64(val)
	case float64:
		return starlark.Float(val)
	case map[string]interface{}:
		return triggerToStarlark(val)
	case []interface{}:
		elems := make([]starlark.Value, len(val))
		for i, e := range val {
			elems[i] = toStarlarkValue(e)
		}
		return starlark.NewList(elems)
	default:
		return starlark.String(fmt.Sprintf("%v", v))
	}
}
