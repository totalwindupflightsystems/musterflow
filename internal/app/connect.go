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
	"github.com/totalwindupflightsystems/musterflow/internal/specfix"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"
)

// ToleratedDefect is one cosmetic defect a tolerant connect repaired or
// recorded. It is an alias so callers do not have to import internal/specfix.
type ToleratedDefect = specfix.Warning

// ConnectResult holds the result of connecting an API.
type ConnectResult struct {
	Connection    *APIConnection
	EndpointCount int
	SpecVersion   string
	SpecTitle     string

	// Tolerated lists the cosmetic defects a tolerant connect repaired or
	// recorded before the document was parsed. Non-empty means the API was
	// connected in a degraded state and the caller must say so.
	Tolerated []specfix.Warning
	// Repaired reports whether the spec bytes handed to the parser differed
	// from the fetched document.
	Repaired bool
}

// ConnectOptions configures API connection.
type ConnectOptions struct {
	SpecURL  string // file path or http(s) URL
	BaseURL  string // override base URL from spec
	Name     string // human-readable name (auto-detected if empty)
	AuthType string // "none", "apikey", "bearer", "oauth2", "mtls"

	// Tolerate enables the cosmetic-defect tolerance path: before parsing,
	// the document is passed through specfix and every repair is recorded on
	// ConnectResult.Tolerated. It is off by default, so the strict behaviour
	// (refuse the document unchanged) stays the default.
	Tolerate bool
}

// Connect loads an OpenAPI spec, validates it, generates commands, and registers the API.
func Connect(ctx context.Context, registry *Registry, opts ConnectOptions) (*ConnectResult, error) {
	data, err := fetchSpec(opts.SpecURL)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	var tolerated []specfix.Warning
	repaired := false
	if opts.Tolerate {
		// Cosmetic-defect tolerance: repair or record every known defect class
		// before the parser sees the document. Strict by default — this block
		// only runs when the operator asked for it.
		fix, fixErr := specfix.Repair(data)
		if fixErr != nil {
			return nil, fmt.Errorf("tolerate spec: %w", fixErr)
		}
		tolerated = fix.Warnings
		repaired = fix.Changed
		if fix.Changed {
			data = fix.Data
		}
	}

	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		if opts.Tolerate {
			return nil, fmt.Errorf("parse spec (tolerance applied, %d cosmetic defect(s) repaired, document still rejected): %w",
				len(tolerated), err)
		}
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
	baseURL = NormalizeBaseURL(baseURL, opts.SpecURL)

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
		Tolerated:     tolerated,
		Repaired:      repaired,
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
		APIName:          conn.Name,
	}
}

func fetchSpec(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, fmt.Errorf("http get: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
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
