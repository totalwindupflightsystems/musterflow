package catalog

import (
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

func TestScore_Official(t *testing.T) {
	entry := CatalogEntry{
		Name:        "GitHub",
		SpecURL:     "https://api.github.com",
		Type:        "openapi",
		Description: "GitHub REST API — full description for quality scoring bonus points",
		Version:     "2022-11-28",
		Downloads:   500,
	}
	score, tier := Score(entry)
	if tier != "official" {
		t.Errorf("tier = %q, want official", tier)
	}
	if score < 7 {
		t.Errorf("score = %d, want >= 7", score)
	}
}

func TestScore_CommunityInferred(t *testing.T) {
	entry := CatalogEntry{
		Name:        "Some API",
		SpecURL:     "https://random.example.com/openapi.json",
		Type:        "openapi",
		Description: "A well-described community API with decent metadata",
		Version:     "1.0",
	}
	score, tier := Score(entry)
	if tier != "community-inferred" {
		t.Errorf("tier = %q, want community-inferred", tier)
	}
	if score < 3 || score >= 7 {
		t.Errorf("score = %d, want 3-6", score)
	}
}

func TestScore_Untested(t *testing.T) {
	entry := CatalogEntry{
		Name:    "Bare API",
		SpecURL: "https://unknown.example.com/spec.yaml",
		Type:    "openapi",
	}
	score, tier := Score(entry)
	if tier != "untested" {
		t.Errorf("tier = %q, want untested", tier)
	}
	if score >= 3 {
		t.Errorf("score = %d, want < 3", score)
	}
}

func TestIsOfficialDomain(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://api.github.com", true},
		{"https://api.stripe.com/v1", true},
		{"https://api.openai.com/v1", true},
		{"https://api.slack.com/api", true},
		{"https://discord.com/api/v10", true},
		{"https://random.example.com/openapi.json", false},
		{"https://api.mycompany.com", false},
		// The bonus must resolve the SPEC host, so documentation and spec hosts
		// of a known provider count too (CATALOG-005).
		{"https://docs.hetzner.cloud/cloud.spec.json", true},
		{"https://openapi.vercel.sh/", true},
		{"https://api.weather.gov/openapi.json", true},
		{"https://huggingface.co/.well-known/openapi.json", true},
		// ...and a host that merely mentions a provider is not the provider.
		{"https://evil.example.com/redirect?to=api.github.com", false},
	}
	for _, tt := range tests {
		got := isOfficialDomain(tt.url)
		if got != tt.want {
			t.Errorf("isOfficialDomain(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// TestOfficialSpecHostReachability locks the official-domain arm against the
// shapes the generator actually produces: the spec document is served from a
// docs/openapi/api host (or the API host itself) while the API base host the
// entry declares is a different host.
func TestOfficialSpecHostReachability(t *testing.T) {
	tests := []struct {
		name    string
		specURL string
		baseURL string
		want    bool
	}{
		{
			name:    "docs host, differing api base (pass-2 hetzner)",
			specURL: "https://docs.hetzner.cloud/cloud.spec.json",
			baseURL: "https://api.hetzner.cloud/v1",
			want:    true,
		},
		{
			name:    "openapi host, differing api base (pass-2 vercel)",
			specURL: "https://openapi.vercel.sh/",
			baseURL: "https://api.vercel.com",
			want:    true,
		},
		{
			name:    "spec served off the api host itself (pass-2 nws)",
			specURL: "https://api.weather.gov/openapi.json",
			baseURL: "https://api.weather.gov",
			want:    true,
		},
		{
			name:    "well-known spec on the provider host (pass-2 huggingface)",
			specURL: "https://huggingface.co/.well-known/openapi.json",
			baseURL: "https://huggingface.co",
			want:    true,
		},
		{
			name:    "docs host on the declared base domain, unknown provider",
			specURL: "https://docs.example.com/raw/openapi.json",
			baseURL: "https://api.example.com",
			want:    true,
		},
		{
			name:    "docs host on a DIFFERENT domain than the declared base",
			specURL: "https://docs.example.com/openapi.json",
			baseURL: "https://api.other.example.net",
			want:    false,
		},
		{
			// ollama publishes its spec through GitHub's raw CDN, not its own
			// host: a third-party mirror is not the provider's publication.
			name:    "third-party raw mirror (pass-2 ollama)",
			specURL: "https://raw.githubusercontent.com/ollama/ollama/main/docs/openapi.yaml",
			baseURL: "http://localhost:11434",
			want:    false,
		},
		{
			name:    "bare marketing host, no base declared",
			specURL: "https://example.com/spec.json",
			baseURL: "",
			want:    false,
		},
		{
			name:    "empty spec url",
			specURL: "",
			baseURL: "https://api.example.com",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOfficialSpecHost(tt.specURL, tt.baseURL); got != tt.want {
				t.Errorf("isOfficialSpecHost(%q, %q) = %v, want %v", tt.specURL, tt.baseURL, got, tt.want)
			}
		})
	}
}

// TestScore_GeneratorEntryDocsHostedSpec is the CATALOG-005 acceptance case: a
// generator-produced entry (ConnectionToCatalogEntry) whose spec document is
// served from a docs host that differs from the API base host it declares must
// score strictly above 2. Pre-fix this shape scored exactly 2 (description +
// version): the +5 arm keyed the spec host against API base hosts so the docs
// host never matched, and the +2 arm keyed on type "openapi" which the
// generator never emits.
func TestScore_GeneratorEntryDocsHostedSpec(t *testing.T) {
	docsHosted := ConnectionToCatalogEntry(&app.APIConnection{
		ID:          "docs-hosted",
		Name:        "Docs Hosted",
		SpecURL:     "https://docs.example.com/raw/openapi.json",
		BaseURL:     "https://api.example.com",
		Description: "An API whose spec is published on the provider's docs host",
		Version:     "1.2.0",
	})
	if docsHosted.Score <= 2 {
		t.Fatalf("generator entry for a docs-hosted spec scored %d, want > 2 (%+v)", docsHosted.Score, docsHosted)
	}

	// Control: same metadata, same declared base, but the spec is mirrored on a
	// third-party host. The difference between the two entries is the +5
	// official-domain bonus, and that bonus must be what lifts the docs-hosted
	// entry — not the structured-spec bonus both of them earn.
	control := ConnectionToCatalogEntry(&app.APIConnection{
		ID:          "mirrored",
		Name:        "Mirrored",
		SpecURL:     "https://mirror.example.net/openapi.json",
		BaseURL:     "https://api.example.com",
		Description: "An API whose spec is published on the provider's docs host",
		Version:     "1.2.0",
	})
	if got := docsHosted.Score - control.Score; got != 5 {
		t.Errorf("docs-hosted bonus over the mirrored control = %d, want 5 (docs-hosted %d, control %d)",
			got, docsHosted.Score, control.Score)
	}
	if control.Score <= 2 {
		t.Errorf("control scored %d, want > 2: the structured-spec bonus must credit the emitted type", control.Score)
	}
	if docsHosted.QualityTier != "official" {
		t.Errorf("docs-hosted tier = %q, want official", docsHosted.QualityTier)
	}
}

// TestScore_StructuredSpecBonusReachableForGeneratorType locks the +2 arm to the
// type vocabulary the shipped entry shape actually uses.
func TestScore_StructuredSpecBonusReachableForGeneratorType(t *testing.T) {
	tests := []struct {
		name  string
		entry CatalogEntry
		want  int
	}{
		{
			name:  "generator type api with a spec document",
			entry: CatalogEntry{Type: "api", SpecURL: "https://example.com/spec.json"},
			want:  2,
		},
		{
			name:  "type api without a spec document",
			entry: CatalogEntry{Type: "api"},
			want:  0,
		},
		{
			name:  "legacy type openapi",
			entry: CatalogEntry{Type: "openapi", SpecURL: "https://example.com/spec.json"},
			want:  2,
		},
		{
			name:  "non-API catalog type",
			entry: CatalogEntry{Type: "workflow", SpecURL: "https://example.com/spec.json"},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _ := Score(tt.entry)
			if score != tt.want {
				t.Errorf("Score(%+v) = %d, want %d", tt.entry, score, tt.want)
			}
		})
	}
}
