package app

import (
	"context"
	"fmt"
	"time"

	"github.com/totalwindupflightsystems/musterflow/internal/specfix"
	"github.com/wojons/muster/pkg/openapi"
)

// Refresh re-fetches and re-parses an API spec, updating the connection's
// metadata and endpoint count. Auth credentials are preserved.
//
// The parse runs under the connection's persisted tolerance opt-in
// (APIConnection.Tolerate), so an API connected with --tolerate keeps parsing
// the same way on every later refresh instead of failing a strict re-parse.
func Refresh(ctx context.Context, registry *Registry, id string) (*RefreshResult, error) {
	return RefreshWithOptions(ctx, registry, id, RefreshOptions{})
}

// RefreshOptions overrides the connection's persisted tolerance opt-in for one
// refresh run.
type RefreshOptions struct {
	// Tolerate forces the tolerance path on (true) or off (false) for this
	// run. Nil means "use the opt-in the connection was registered with".
	Tolerate *bool
}

// RefreshWithOptions is Refresh with a per-run override of the tolerance
// opt-in.
func RefreshWithOptions(ctx context.Context, registry *Registry, id string, opts RefreshOptions) (*RefreshResult, error) {
	conn, err := registry.Get(id)
	if err != nil {
		return nil, fmt.Errorf("get api: %w", err)
	}

	tolerate := conn.Tolerate
	if opts.Tolerate != nil {
		tolerate = *opts.Tolerate
		conn.Tolerate = tolerate
	}

	specURL := conn.SpecURL

	// Re-fetch and re-parse the spec
	data, err := fetchSpec(specURL)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	// Same tolerance path the connection was created under: repairing the
	// fetched bytes the same way is what makes a refresh of a tolerated
	// connection succeed (and report the same defect summary) rather than
	// failing where connect succeeded.
	fixed, tolerated, repaired, err := ApplyTolerance(data, tolerate)
	if err != nil {
		return nil, err
	}
	data = fixed

	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		if tolerate {
			return nil, fmt.Errorf("parse spec (tolerance applied, %d cosmetic defect(s) repaired, document still rejected): %w",
				len(tolerated), err)
		}
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	oldVersion := conn.Version
	oldEndpoints := conn.EndpointCount

	conn.Version = result.Version
	conn.EndpointCount = countEndpoints(result.Document)
	conn.UpdatedAt = time.Now()

	// Re-derive base URL only from a usable server URL. A path-only ("/api/v3")
	// or templated ("https://{host}/v1") server value cannot serve as a base
	// URL, so the existing one is preserved.
	rawServerURL := ""
	if len(result.Document.Servers) > 0 {
		rawServerURL = result.Document.Servers[0].URL
	}
	newBaseURL := ResolveRefreshBaseURL(conn.BaseURL, rawServerURL, conn.SpecURL)
	if newBaseURL != conn.BaseURL {
		fmt.Printf("  ⚠ Base URL changed: %s → %s\n", conn.BaseURL, newBaseURL)
		conn.BaseURL = newBaseURL
	}

	// Re-derive the cached command plan from the refreshed document and prove
	// it still produces a command tree — the same path connect validates
	// through. A refresh that produced no callable commands must fail loudly
	// (keeping the previous plan in place) instead of leaving the API with an
	// empty command list.
	plan, err := BuildPlan(result.Document, tolerated, repaired)
	if err != nil {
		return nil, fmt.Errorf("derive command plan for %s: %w", conn.SpecURL, err)
	}
	plan.ConnectionID = conn.ID
	plan.SpecURL = conn.SpecURL
	plan.Title = conn.Name
	plan.BaseURL = conn.BaseURL
	plan.EndpointCount = conn.EndpointCount
	plan.ContentSHA256 = ContentHash(data)

	commands, genErr := CommandsFromPlan(plan, conn)
	if genErr != nil || len(commands) == 0 {
		defect := DiagnoseLoadFailure(data)
		if defect == nil {
			defect = &LoadDefect{Pointer: "/paths", Reason: "the refreshed spec yields an empty command tree; the previous plan is kept"}
		}
		defect.Spec = conn.SpecURL
		if genErr != nil {
			defect.Cause = genErr
		}
		return nil, defect
	}
	IndexPlanResources(plan, commands)

	if err := registry.Add(conn); err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}
	if err := WritePlan(registry.DataDir(), plan); err != nil {
		return nil, fmt.Errorf("cache refreshed spec for %s: %w", conn.SpecURL, err)
	}

	return &RefreshResult{
		Connection:     conn,
		OldVersion:     oldVersion,
		NewVersion:     conn.Version,
		OldEndpoints:   oldEndpoints,
		NewEndpoints:   conn.EndpointCount,
		VersionChanged: oldVersion != conn.Version,
		Tolerated:      tolerated,
		Repaired:       repaired,
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

	// Tolerated lists the cosmetic defects the tolerant repair pass repaired
	// or recorded for this refresh. Non-empty means the refresh ran in a
	// degraded state and the caller must say so.
	Tolerated []specfix.Warning
	// Repaired reports whether the spec bytes handed to the parser differed
	// from the fetched document.
	Repaired bool
}
