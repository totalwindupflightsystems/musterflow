package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// stagedPreChangeScores are the scores the shipped formula computed for the
// pass-2 staged entries (catalog/staging/pass2) before CATALOG-005. They are
// recorded here as constants rather than read back from the staged files,
// because those files are the pass-2 artifact and this package does not rewrite
// another row's deliverable: the score recorded inside each file is the
// pre-change value, and this helper proves the current formula scores strictly
// above it.
var stagedPreChangeScores = map[string]int{
	"9a87419efed4870b": 2, // vercel
	"8ac8f489bdf0ba6f": 2, // huggingface
	"3f26453ec4d52504": 2, // ollama
	"70b5bf949a656585": 1, // nws
	"c3ecb6f87d542f23": 2, // hetzner
}

// TestStagedEntryScoresRegenerated loads every pass-2 staged catalog entry and
// recomputes its score with the shipped formula. It is the CATALOG-005
// regeneration harness — a test helper, not a new shipped command — and it logs
// one id/spec_url/before/after line per entry so the numbers in
// .coding-hermes/evidence/CATALOG-005/score-before-after.md can be re-derived
// from this tree.
func TestStagedEntryScoresRegenerated(t *testing.T) {
	dir := filepath.Join("..", "..", "catalog", "staging", "pass2")
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob staged entries: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no staged entries under %s", dir)
	}

	seen := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var entry CatalogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if entry.ID == "" {
			// seed-proposal.json is the pass-2 proposal document, not an entry.
			continue
		}
		seen++

		before, ok := stagedPreChangeScores[entry.ID]
		if !ok {
			t.Fatalf("%s: entry %q has no recorded pre-change score", path, entry.ID)
		}
		if entry.SpecURL == "" {
			t.Errorf("%s: entry %q carries no spec_url", path, entry.ID)
		}

		score, tier := Score(entry)
		t.Logf("%s\tspec_url=%s\tscore_before=%d\tscore_after=%d\ttier_after=%s",
			entry.ID, entry.SpecURL, before, score, tier)

		if score <= before {
			t.Errorf("%s: entry %q scored %d, want above the pre-change %d", path, entry.ID, score, before)
		}
	}
	if seen != len(stagedPreChangeScores) {
		t.Errorf("scored %d staged entries, want %d", seen, len(stagedPreChangeScores))
	}
}
