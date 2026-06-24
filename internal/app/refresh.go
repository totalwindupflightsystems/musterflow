package app

import (
	"context"
	"fmt"
	"time"

	"github.com/wojons/muster/pkg/openapi"
)

// Refresh re-fetches and re-parses an API spec, updating the connection's
// metadata and endpoint count. Auth credentials are preserved.
func Refresh(ctx context.Context, registry *Registry, id string) (*RefreshResult, error) {
	conn, err := registry.Get(id)
	if err != nil {
		return nil, fmt.Errorf("get api: %w", err)
	}

	specURL := conn.SpecURL

	// Re-fetch and re-parse the spec
	data, err := fetchSpec(specURL)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	oldVersion := conn.Version
	oldEndpoints := conn.EndpointCount

	conn.Version = result.Version
	conn.EndpointCount = countEndpoints(result.Document)
	conn.UpdatedAt = time.Now()

	// Re-derive base URL if servers changed
	newBaseURL := ""
	if len(result.Document.Servers) > 0 {
		newBaseURL = result.Document.Servers[0].URL
	}
	if newBaseURL != "" && newBaseURL != conn.BaseURL {
		// Could change — warn
		fmt.Printf("  ⚠ Base URL changed: %s → %s\n", conn.BaseURL, newBaseURL)
		conn.BaseURL = newBaseURL
	}

	if err := registry.Add(conn); err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}

	return &RefreshResult{
		Connection:    conn,
		OldVersion:    oldVersion,
		NewVersion:    conn.Version,
		OldEndpoints:  oldEndpoints,
		NewEndpoints:  conn.EndpointCount,
		VersionChanged: oldVersion != conn.Version,
	}, nil
}

// RefreshResult holds the outcome of a spec refresh.
type RefreshResult struct {
	Connection     *APIConnection
	OldVersion     string
	NewVersion     string
	OldEndpoints   int
	NewEndpoints   int
	VersionChanged bool
}
