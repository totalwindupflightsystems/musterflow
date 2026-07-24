// Package cli provides dashboard HTTP API routing helpers.
// When the MusterFlow dashboard is running, CLI commands route through
// the dashboard HTTP API to avoid DuckDB lock conflicts.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

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
	defer resp.Body.Close()

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
	if resp.StatusCode != http.StatusCreated {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("connect via dashboard: %s", msg)
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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

	if resp.StatusCode != http.StatusOK {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("refresh via dashboard: %s", msg)
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	if resp.StatusCode != http.StatusCreated {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("pull via dashboard: %s", msg)
	}

	fmt.Printf("✓ Pulled and connected: %s\n", result.SpecTitle)
	fmt.Printf("  ID: %s\n", result.ID)
	fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
	return nil
}

// mcpViaDashboard fetches MCP endpoint information from the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func mcpViaDashboard() error {
	resp, err := http.Get(dashboardBaseURL + "/api/mcp/info")
	if err != nil {
		return fmt.Errorf("dashboard MCP info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Endpoint  string `json:"endpoint"`
		Transport string `json:"transport"`
		ToolCount int    `json:"tool_count"`
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
	fmt.Printf("Exposed MCP tools from %d APIs:\n\n", result.ToolCount)
	for _, t := range result.Tools {
		fmt.Printf("  [%s] %s\n", t.Name, t.Description)
	}
	fmt.Println("\nConnect an MCP client to " + result.Endpoint + " to use these tools.")
	return nil
}
