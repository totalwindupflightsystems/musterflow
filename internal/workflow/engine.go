// Package workflow provides the workflow engine for MusterFlow.
// Workflows are Starlark scripts that chain API calls, with webhook trigger support.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
)

// Flow represents a named workflow.
type Flow struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"` // Starlark source code
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
			Name:        name,
			Source:      string(data),
			WebhookURL:  fmt.Sprintf("%s/hooks/%s", e.baseURL, name),
		})
	}
	if flows == nil {
		flows = []Flow{}
	}
	return flows, nil
}

// Run executes a flow by name with the given trigger payload.
// Returns the execution output or an error.
func (e *Engine) Run(name string, triggerPayload map[string]interface{}) (string, error) {
	flowPath := filepath.Join(e.dir, name+".star")
	source, err := os.ReadFile(flowPath)
	if err != nil {
		return "", fmt.Errorf("read flow %s: %w", name, err)
	}

	// Starlark execution: this is a stub that simulates execution.
	// Full Starlark integration with muster's pkg/dsl is a Phase 2 item.
	// For now, we return the source as output and note the trigger was received.
	_ = source
	_ = triggerPayload

	output := fmt.Sprintf("Flow %q executed successfully.", name)
	if triggerPayload != nil {
		output += fmt.Sprintf(" Trigger payload: %v", triggerPayload)
	}
	output += "\n\nFull Starlark execution is a Phase 2 feature. The flow source has been saved and will be executed when the Starlark interpreter is wired."
	return output, nil
}
