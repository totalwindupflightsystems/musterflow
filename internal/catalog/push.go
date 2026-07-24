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

	return CatalogEntry{
		ID:          conn.ID,
		Name:        conn.Name,
		Type:        "api",
		Description: description,
		SpecURL:     conn.SpecURL,
		Author:      "community",
		Version:     version,
		QualityTier: "community-inferred",
		AddedAt:     conn.AddedAt,
		Downloads:   0,
		Score:       0,
	}
}
