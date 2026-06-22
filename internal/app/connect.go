// Package app provides API connection management for MusterFlow.
package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"
)

// ConnectResult holds the result of connecting an API.
type ConnectResult struct {
	Connection    *APIConnection
	EndpointCount int
	SpecVersion   string
	SpecTitle     string
}

// ConnectOptions configures API connection.
type ConnectOptions struct {
	SpecURL  string // file path or http(s) URL
	BaseURL  string // override base URL from spec
	Name     string // human-readable name (auto-detected if empty)
	AuthType string // "none", "apikey", "bearer", "oauth2", "mtls"
}

// Connect loads an OpenAPI spec, validates it, generates commands, and registers the API.
func Connect(ctx context.Context, registry *Registry, opts ConnectOptions) (*ConnectResult, error) {
	data, err := fetchSpec(opts.SpecURL)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	apiName := opts.Name
	if apiName == "" {
		apiName = deriveName(opts.SpecURL, result.Document)
	}

	baseURL := opts.BaseURL
	if baseURL == "" && len(result.Document.Servers) > 0 {
		baseURL = result.Document.Servers[0].URL
	}

	// Clean up base URL: don't add https:// to path-only URLs like "/api/v3"
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		if strings.HasPrefix(baseURL, "/") {
			// Path-only — keep as-is, or use spec origin
			if strings.HasPrefix(opts.SpecURL, "http") {
				parts := strings.SplitN(opts.SpecURL, "/", 4)
				if len(parts) >= 3 {
					baseURL = parts[0] + "//" + parts[2] + baseURL
				}
			}
		} else {
			baseURL = "https://" + baseURL
		}
	}

	endpointCount := countEndpoints(result.Document)
	id := generateID(opts.SpecURL)

	conn := &APIConnection{
		ID:            id,
		Name:          apiName,
		SpecURL:       opts.SpecURL,
		BaseURL:       baseURL,
		Version:       result.Version,
		Description:   deriveDescription(result.Document),
		AuthType:      opts.AuthType,
		EndpointCount: endpointCount,
	}

	if err := registry.Add(conn); err != nil {
		return nil, fmt.Errorf("register api: %w", err)
	}

	return &ConnectResult{
		Connection:    conn,
		EndpointCount: endpointCount,
		SpecVersion:   result.Version,
		SpecTitle:     apiName,
	}, nil
}

// Disconnect removes a connected API from the registry.
func Disconnect(registry *Registry, id string) error {
	return registry.Remove(id)
}

// GenerateCommandConfig creates a generator config for a connected API.
func GenerateCommandConfig(conn *APIConnection) *generator.Config {
	return &generator.Config{
		AppName:          "musterflow",
		AppDescription:   fmt.Sprintf("MusterFlow CLI for %s", conn.Name),
		BaseURL:          conn.BaseURL,
		DefaultFormat:    "table",
		SupportedFormats: []string{"table", "json", "yaml"},
	}
}

func fetchSpec(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, fmt.Errorf("http get: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http status %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}

func generateID(specURL string) string {
	h := sha256.Sum256([]byte(specURL))
	return fmt.Sprintf("%x", h[:8])
}

func deriveName(specURL string, doc *openapi3.T) string {
	if doc != nil && doc.Info != nil && doc.Info.Title != "" {
		name := strings.ToLower(doc.Info.Title)
		// Replace spaces and non-alphanum with hyphens, collapse multiples
		name = collapseHyphens(strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			if r == ' ' || r == '_' || r == '.' {
				return '-'
			}
			return -1
		}, name))
		return strings.Trim(name, "-")
	}
	if strings.HasPrefix(specURL, "http") {
		u := strings.TrimPrefix(specURL, "https://")
		u = strings.TrimPrefix(u, "http://")
		parts := strings.Split(u, "/")
		if len(parts) > 0 {
			return strings.ReplaceAll(parts[0], ".", "-")
		}
	}
	name := specURL
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSuffix(name, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	name = strings.TrimSuffix(name, ".json")
	return name
}

// collapseHyphens replaces sequences of hyphens with a single hyphen.
func collapseHyphens(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, c := range s {
		if c == '-' {
			if !prevHyphen {
				b.WriteRune(c)
			}
			prevHyphen = true
		} else {
			b.WriteRune(c)
			prevHyphen = false
		}
	}
	return b.String()
}

func deriveDescription(doc *openapi3.T) string {
	if doc != nil && doc.Info != nil && doc.Info.Description != "" {
		d := doc.Info.Description
		if len(d) > 200 {
			return d[:200] + "..."
		}
		return d
	}
	return ""
}

func countEndpoints(doc *openapi3.T) int {
	if doc == nil || doc.Paths == nil {
		return 0
	}
	count := 0
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		if pathItem.Get != nil {
			count++
		}
		if pathItem.Post != nil {
			count++
		}
		if pathItem.Put != nil {
			count++
		}
		if pathItem.Patch != nil {
			count++
		}
		if pathItem.Delete != nil {
			count++
		}
		if pathItem.Head != nil {
			count++
		}
		if pathItem.Options != nil {
			count++
		}
	}
	return count
}
