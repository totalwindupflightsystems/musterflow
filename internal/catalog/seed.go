package catalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

// seedJSON is the bundled default catalog: a copy of the repo's tracked
// catalog/seed.json. go:embed cannot reach outside a package's own directory,
// so the entries live here as well; TestBundledSeedMatchesRepoSeed fails if the
// two copies drift apart.
//
//go:embed seed.json
var seedJSON []byte

// Source says where a catalog answer came from.
type Source string

const (
	// SourceCommunity means the answer came from the community index.
	SourceCommunity Source = "community"
	// SourceBundled means the community index could not be served, so the
	// catalog bundled inside the binary answered instead.
	SourceBundled Source = "bundled"
)

// Catalog is the outcome of a catalog read that can degrade to the bundled
// defaults. Reason records why the community index could not be served, so the
// caller can show the cause next to its fallback notice.
type Catalog struct {
	Entries []CatalogEntry
	Source  Source
	Reason  error
}

// EntryResult is the outcome of resolving a single catalog entry by id.
// Entry is nil when neither the community index nor the bundled defaults carry
// the id — the caller's "not found" path.
type EntryResult struct {
	Entry  *CatalogEntry
	Source Source
	Reason error
}

// BundledEntries returns the seed entries carried inside the binary.
func BundledEntries() ([]CatalogEntry, error) {
	var entries []CatalogEntry
	if err := json.Unmarshal(seedJSON, &entries); err != nil {
		return nil, fmt.Errorf("decode bundled seed catalog: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("bundled seed catalog is empty")
	}
	return entries, nil
}

// bundledEntry looks up one bundled entry by id. A miss reports (nil, nil).
func bundledEntry(id string) (*CatalogEntry, error) {
	entries, err := BundledEntries()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// SearchCatalog searches the community catalog and falls back to the bundled
// seed entries when the community index cannot be served. Source says which
// answered, and Reason carries the community error in the bundled case.
//
// An error is returned only when the community index failed AND the bundled
// catalog is unusable — the one situation where the caller has nothing to show.
func (c *Client) SearchCatalog(query string) (Catalog, error) {
	entries, err := c.List()
	if err == nil {
		return Catalog{Entries: Search(entries, query), Source: SourceCommunity}, nil
	}
	return c.bundledCatalog("search", err, func(bundled []CatalogEntry) []CatalogEntry {
		return Search(bundled, query)
	})
}

// ListCatalog returns every catalog entry, falling back to the bundled seed
// entries when the community index cannot be served.
func (c *Client) ListCatalog() (Catalog, error) {
	entries, err := c.List()
	if err == nil {
		return Catalog{Entries: entries, Source: SourceCommunity}, nil
	}
	return c.bundledCatalog("list", err, func(bundled []CatalogEntry) []CatalogEntry {
		return bundled
	})
}

// FetchCatalogEntry resolves one entry by id, falling back to the bundled seed
// entries when the community index cannot be served — or when it serves an index
// that simply does not carry the id (a 404 there is the same user-visible
// situation: the community catalog has nothing to offer).
//
// A miss in both reports the community-side error, which is the not-found text
// `catalog pull` has always printed for an unknown id.
func (c *Client) FetchCatalogEntry(id string) (EntryResult, error) {
	fetched, _, err := c.FetchEntry(id)
	if err == nil && fetched != nil {
		return EntryResult{Entry: fetched, Source: SourceCommunity}, nil
	}
	// A miss (404) and an unreachable index are handled the same way: the
	// bundled catalog may still carry the id.
	bundled, seedErr := bundledEntry(id)
	if seedErr != nil {
		if err == nil {
			// The community index answered but did not carry the entry, and
			// the bundled catalog is broken: name the bundled failure, since
			// that is the only reason this lookup could not answer.
			return EntryResult{}, fmt.Errorf("%w (community catalog has no entry %q)", seedErr, id)
		}
		return EntryResult{}, fmt.Errorf("%w (bundled seed catalog unusable: %v)", err, seedErr)
	}
	if bundled != nil {
		reason := err
		if reason == nil {
			// Entry absent upstream but present in the bundled defaults is
			// still a degraded answer; keep the community miss as the reason.
			reason = fmt.Errorf("community catalog has no entry %q", id)
		}
		return EntryResult{Entry: bundled, Source: SourceBundled, Reason: reason}, nil
	}
	if err != nil {
		return EntryResult{}, err
	}
	return EntryResult{}, nil
}

// bundledCatalog applies transform to the bundled entries after a community
// index failure. The community error is kept as Reason when the fallback
// answers, and surfaced when the bundled catalog itself is unusable.
func (c *Client) bundledCatalog(op string, communityErr error, transform func([]CatalogEntry) []CatalogEntry) (Catalog, error) {
	bundled, seedErr := BundledEntries()
	if seedErr != nil {
		return Catalog{}, fmt.Errorf("%w (bundled seed catalog unusable during %s: %v)", communityErr, op, seedErr)
	}
	return Catalog{Entries: transform(bundled), Source: SourceBundled, Reason: communityErr}, nil
}
