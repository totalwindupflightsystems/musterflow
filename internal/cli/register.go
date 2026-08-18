// Package cli provides dashboard HTTP API routing helpers.
// When the MusterFlow dashboard is running, CLI commands route through
// the dashboard HTTP API to avoid DuckDB lock conflicts.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
)

// connectViaDashboard routes a connect operation through the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func connectViaDashboard(specURL, baseURL, nameInput, authType string) error {
	payload := map[string]interface{}{
		"spec_url":  specURL,
		"base_url":  baseURL,
		"name":      nameInput,
		"auth_type": authType,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal connect payload: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/apis", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dashboard connect request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status BEFORE decoding so a non-JSON response (e.g. a foreign
	// server squatting on the port) gives a clean error instead of
	// "json: cannot unmarshal number into Go value of type struct". DF-013.
	if resp.StatusCode != http.StatusCreated {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("connect via dashboard: %s", msg)
	}

	var result struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SpecTitle     string `json:"spec_title"`
		SpecVersion   string `json:"spec_version"`
		EndpointCount int    `json:"endpoint_count"`
		BaseURL       string `json:"base_url"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Printf("✓ Connected: %s\n", result.SpecTitle)
	fmt.Printf("  ID: %s\n", result.ID)
	fmt.Printf("  Version: %s\n", result.SpecVersion)
	fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
	fmt.Printf("  Base URL: %s\n", result.BaseURL)
	fmt.Printf("\nTry: musterflow %s --help\n", result.Name)
	return nil
}

// disconnectViaDashboard routes a disconnect operation through the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func disconnectViaDashboard(apiID string) error {
	req, err := http.NewRequest(http.MethodDelete, dashboardBaseURL+"/api/apis/"+apiID, nil)
	if err != nil {
		return fmt.Errorf("create disconnect request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dashboard disconnect request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("disconnect via dashboard: %s", msg)
	}

	fmt.Printf("✓ Disconnected: %s\n", apiID)
	return nil
}

// listViaDashboard fetches the connected API list from the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func listViaDashboard() error {
	resp, err := http.Get(dashboardBaseURL + "/api/apis")
	if err != nil {
		return fmt.Errorf("dashboard list request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		APIs []*app.APIConnection `json:"apis"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if len(result.APIs) == 0 {
		fmt.Println("No APIs connected.")
		fmt.Println("Run 'musterflow connect <url>' to add one.")
		return nil
	}

	fmt.Printf("Connected APIs (%d):\n\n", len(result.APIs))
	for _, conn := range result.APIs {
		fmt.Printf("  %s (%s)\n", conn.Name, conn.ID)
		fmt.Printf("    Spec: %s\n", conn.SpecURL)
		fmt.Printf("    Base: %s\n", conn.BaseURL)
		fmt.Printf("    Endpoints: %d\n", conn.EndpointCount)
		fmt.Printf("    Auth: %s\n", conn.AuthType)
		fmt.Println()
	}
	return nil
}

// catalogSearchViaDashboard searches the catalog via the dashboard HTTP API.
func catalogSearchViaDashboard(query string) error {
	resp, err := http.Get(dashboardBaseURL + "/api/catalog/search?q=" + query)
	if err != nil {
		return fmt.Errorf("dashboard catalog search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []catalog.CatalogEntry `json:"results"`
		Total   int                    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if len(result.Results) == 0 {
		fmt.Println("No catalog entries found.")
		return nil
	}
	fmt.Printf("Catalog search results (%d):\n\n", len(result.Results))
	fmt.Printf("  %-20s %-20s %-6s %-60s %s\n", "ID", "NAME", "TYPE", "DESCRIPTION", "DOWNLOADS")
	for _, e := range result.Results {
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:60]
		}
		fmt.Printf("  %-20s %-20s %-6s %-60s %d\n", e.ID, e.Name, e.Type, desc, e.Downloads)
	}
	return nil
}

// refreshViaDashboard routes a refresh operation through the dashboard HTTP API.
func refreshViaDashboard(apiID string) error {
	resp, err := http.Post(dashboardBaseURL+"/api/apis/"+apiID+"/refresh", "application/json", nil)
	if err != nil {
		return fmt.Errorf("dashboard refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status before decoding. DF-013.
	if resp.StatusCode != http.StatusOK {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("refresh via dashboard: %s", msg)
	}

	var result struct {
		Name           string `json:"name"`
		OldVersion     string `json:"old_version"`
		NewVersion     string `json:"new_version"`
		VersionChanged bool   `json:"version_changed"`
		OldEndpoints   int    `json:"old_endpoints"`
		NewEndpoints   int    `json:"new_endpoints"`
		Error          string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Printf("✓ Refreshed %s\n", result.Name)
	fmt.Printf("  Version: %s → %s", result.OldVersion, result.NewVersion)
	if result.VersionChanged {
		fmt.Print(" (changed)")
	}
	fmt.Println()
	fmt.Printf("  Endpoints: %d → %d\n", result.OldEndpoints, result.NewEndpoints)
	return nil
}

// pushViaDashboard fetches an API connection from the dashboard and prints the catalog entry JSON.
func pushViaDashboard(apiID string) error {
	resp, err := http.Get(dashboardBaseURL + "/api/apis/" + apiID)
	if err != nil {
		return fmt.Errorf("dashboard get request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("get api via dashboard: %s", msg)
	}

	var conn app.APIConnection
	if err := json.NewDecoder(resp.Body).Decode(&conn); err != nil {
		return fmt.Errorf("decode connection: %w", err)
	}

	entry := catalog.ConnectionToCatalogEntry(&conn)
	data, err := entry.ToJSON()
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	fmt.Printf("Catalog entry JSON for %s:\n\n", conn.ID)
	fmt.Println(string(data))
	fmt.Println()
	fmt.Printf("Submit a PR to https://github.com/totalwindupflightsystems/musterflow-catalog with the entry JSON at entries/%s.json and add it to index.json\n", conn.ID)
	return nil
}

// pullViaDashboard pulls an API from the catalog via the dashboard HTTP API.
func pullViaDashboard(apiID string) error {
	client := catalog.NewClient()
	entry, _, err := client.FetchEntry(apiID)
	if err != nil {
		fmt.Printf("Error pulling from catalog: %v\n", err)
		return nil
	}
	if entry == nil {
		fmt.Printf("Entry %s not found in catalog.\n", apiID)
		return nil
	}
	fmt.Printf("Pulling %s (%s) from community catalog...\n", entry.ID, entry.Name)

	// Route through dashboard connect endpoint
	payload := map[string]interface{}{
		"spec_url": entry.SpecURL,
		"name":     entry.Name,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal pull payload: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/apis", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dashboard pull request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status before decoding. DF-013.
	if resp.StatusCode != http.StatusCreated {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("pull via dashboard: %s", msg)
	}

	var result struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SpecTitle     string `json:"spec_title"`
		EndpointCount int    `json:"endpoint_count"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Printf("✓ Pulled and connected: %s\n", result.SpecTitle)
	fmt.Printf("  ID: %s\n", result.ID)
	fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
	return nil
}

// flowListViaDashboard lists workflows via the dashboard HTTP API.
func flowListViaDashboard() error {
	resp, err := http.Get(dashboardBaseURL + "/api/flows")
	if err != nil {
		return fmt.Errorf("dashboard flow list request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Flows []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			WebhookURL  string `json:"webhook_url"`
		} `json:"flows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if len(result.Flows) == 0 {
		fmt.Println("No workflows defined.")
		fmt.Println("Create one with: musterflow flow create <name>")
		return nil
	}

	fmt.Println("Workflows:")
	for _, f := range result.Flows {
		fmt.Printf("  %s", f.Name)
		if f.Description != "" {
			fmt.Printf("  - %s", f.Description)
		}
		if f.WebhookURL != "" {
			fmt.Printf("  webhook: %s", f.WebhookURL)
		}
		fmt.Println()
	}
	return nil
}

// flowCreateViaDashboard creates a workflow via the dashboard HTTP API.
func flowCreateViaDashboard(name, source, description string, webhook bool) error {
	if source == "" {
		source = "# Write your Starlark workflow here\n"
	}
	payload := map[string]interface{}{
		"name":        name,
		"source":      source,
		"description": description,
		"webhook":     webhook,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal flow create payload: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/flows", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dashboard flow create request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status before decoding. DF-013.
	if resp.StatusCode != http.StatusCreated {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("create flow via dashboard: %s", msg)
	}

	var result struct {
		Name       string `json:"name"`
		WebhookURL string `json:"webhook_url"`
		FlowsDir   string `json:"flows_dir"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Printf("✓ Created flow %q\n", result.Name)
	if result.WebhookURL != "" {
		fmt.Printf("  Webhook URL: %s\n", result.WebhookURL)
	}
	flowsDir := result.FlowsDir
	if flowsDir == "" {
		flowsDir = app.DefaultDataDir()
	}
	fmt.Printf("  Edit: %s/%s.star\n", flowsDir, result.Name)
	return nil
}

// flowRunViaDashboard runs a workflow via the dashboard HTTP API and prints
// the raw output with a single trailing newline (matching the local
// engine.Run contract: if the result already ends with "\n" no extra
// newline is added, otherwise one is appended).
// When payload is nil, the request is sent with no body (preserving the
// existing nil-trigger behavior). When non-nil, the payload is wrapped as
// {"trigger": {...}} to match the dashboard server's expected body schema.
func flowRunViaDashboard(name string, payload map[string]interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		wrapped := map[string]interface{}{"trigger": payload}
		body, err := json.Marshal(wrapped)
		if err != nil {
			return fmt.Errorf("marshal flow run payload: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	}
	resp, err := http.Post(dashboardBaseURL+"/api/flows/"+name+"/run", "application/json", bodyReader)
	if err != nil {
		return fmt.Errorf("dashboard flow run request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status before decoding. DF-013.
	if resp.StatusCode != http.StatusOK {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("run flow via dashboard: %s", msg)
	}

	var result struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Print(result.Result)
	if !strings.HasSuffix(result.Result, "\n") {
		fmt.Println()
	}
	return nil
}

// mcpViaDashboard fetches MCP endpoint information from the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func mcpViaDashboard() error {
	resp, err := http.Get(dashboardBaseURL + "/api/mcp/info")
	if err != nil {
		return fmt.Errorf("dashboard MCP info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Endpoint  string `json:"endpoint"`
		Transport string `json:"transport"`
		ToolCount int    `json:"tool_count"`
		APICount  int    `json:"api_count"`
		Tools     []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Example     string `json:"example"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Println("MCP endpoint:", result.Endpoint)
	fmt.Println("Transport:", result.Transport)
	fmt.Println()
	if result.ToolCount == 0 {
		fmt.Println("No APIs connected. Connect APIs to expose them as MCP tools.")
		return nil
	}
	// DF-009: print the real API count (api_count) not the tool count.
	// Fall back to tool count if api_count is absent (older dashboard).
	apiCount := result.APICount
	if apiCount == 0 {
		apiCount = result.ToolCount
	}
	fmt.Printf("Exposed MCP tools from %d APIs (%d tools):\n\n", apiCount, result.ToolCount)
	for _, t := range result.Tools {
		fmt.Printf("  [%s] %s\n", t.Name, t.Description)
	}
	fmt.Println("\nConnect an MCP client to " + result.Endpoint + " to use these tools.")
	return nil
}

// exportViaDashboard routes export through the dashboard HTTP API.
// It GETs /api/export (JSONL), writes the response body to the local file,
// and prints the count of exported APIs.
func exportViaDashboard(path string) error {
	resp, err := http.Get(dashboardBaseURL + "/api/export")
	if err != nil {
		return fmt.Errorf("dashboard export request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("export via dashboard: %s", msg)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("write export file: %w", err)
	}

	// Count lines (one JSON object per line) to report the API count.
	count := 0
	if n > 0 {
		data, err := os.ReadFile(path)
		if err == nil {
			count = bytes.Count(data, []byte("\n"))
		}
	}

	fmt.Printf("✓ Exported %d APIs to %s\n", count, path)
	return nil
}

// importViaDashboard routes import through the dashboard HTTP API.
// It reads the local JSONL file and POSTs its content to /api/import.
func importViaDashboard(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read import file: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/import", "application/x-ndjson", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dashboard import request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check status before decoding. DF-013.
	if resp.StatusCode != http.StatusOK {
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResult)
		msg := errResult.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("import via dashboard: %s", msg)
	}

	var result struct {
		Imported int    `json:"imported"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	fmt.Printf("✓ Imported %d APIs from %s\n", result.Imported, path)
	return nil
}
