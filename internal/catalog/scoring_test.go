package catalog

import "testing"

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
	}
	for _, tt := range tests {
		got := isOfficialDomain(tt.url)
		if got != tt.want {
			t.Errorf("isOfficialDomain(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
