package catalog

import (
	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// ConnectionToCatalogEntry converts a local APIConnection into a CatalogEntry
// suitable for submission to the community catalog.
func ConnectionToCatalogEntry(conn *app.APIConnection) CatalogEntry {
	description := conn.Description
	if description == "" {
		description = "API connector for " + conn.Name
	}

	version := conn.Version
	if version == "" {
		version = "1.0.0"
	}

	entry := CatalogEntry{
		ID:          conn.ID,
		Name:        conn.Name,
		Type:        "api",
		Description: description,
		SpecURL:     conn.SpecURL,
		BaseURL:     conn.BaseURL,
		Author:      "community",
		Version:     version,
		AddedAt:     conn.AddedAt,
		Downloads:   0,
	}
	// The shipped scoring function decides both the score and the tier, so a
	// pushed entry carries a measured quality signal. Before CATALOG-002 the
	// entry was emitted with a hardcoded Score of 0 and a hardcoded
	// "community-inferred" tier, which is exactly the placeholder scoreToTier
	// exists to avoid: a zero reads as "unscored" and the tier contradicted it.
	//
	// BaseURL is carried so Score can tell a provider that publishes its spec on
	// a docs/openapi host (docs.hetzner.cloud next to api.hetzner.cloud) from a
	// spec mirrored on a third-party host (CATALOG-005).
	entry.Score, entry.QualityTier = Score(entry)
	return entry
}
