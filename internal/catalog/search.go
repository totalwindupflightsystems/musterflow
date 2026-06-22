package catalog

import "sort"

// Search scores entries by relevance to the query and returns matches
// sorted by score descending. Entries with a score of 0 or less are excluded.
// Matching is case-insensitive.
func Search(entries []CatalogEntry, query string) []CatalogEntry {
	if query == "" {
		return nil
	}
	qlower := toLower(query)

	var scored []CatalogEntry
	for _, e := range entries {
		score := scoreEntry(e, qlower)
		if score > 0 {
			e.Score = score
			scored = append(scored, e)
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	return scored
}

// scoreEntry computes the fuzzy relevance score for an entry against a
// lowercased query.
func scoreEntry(e CatalogEntry, qlower string) int {
	if qlower == "" {
		return 0
	}

	nameLower := toLower(e.Name)
	descLower := toLower(e.Description)
	idLower := toLower(e.ID)

	score := 0

	// Exact name match: +100
	if nameLower == qlower {
		score += 100
	}
	// Name starts with query: +50
	if len(nameLower) >= len(qlower) && nameLower[:len(qlower)] == qlower {
		score += 50
	}
	// Name contains query: +30
	if contains(nameLower, qlower) {
		score += 30
	}
	// Description contains query: +10
	if contains(descLower, qlower) {
		score += 10
	}
	// ID matches query: +20
	if idLower == qlower {
		score += 20
	}

	return score
}