package catalog

import "strings"

// Score computes a quality score (0-10) for a catalog entry based on its
// spec URL domain and metadata.
func Score(entry CatalogEntry) (int, string) {
	score := 0

	// Official domain bonus (+5)
	if isOfficialDomain(entry.SpecURL) {
		score += 5
	}

	// Structured spec bonus
	if entry.Type == "openapi" {
		score += 2
	}

	// Has description
	if entry.Description != "" && len(entry.Description) > 20 {
		score += 1
	}

	// Has version
	if entry.Version != "" {
		score += 1
	}

	// Community engagement bonus
	if entry.Downloads > 100 {
		score += 1
	}

	tier := scoreToTier(score)
	return score, tier
}

func scoreToTier(score int) string {
	switch {
	case score >= 7:
		return "official"
	case score >= 3:
		return "community-inferred"
	default:
		return "untested"
	}
}

// isOfficialDomain returns true if the spec URL is from a known official API provider.
func isOfficialDomain(specURL string) bool {
	officialDomains := []string{
		"api.github.com",
		"api.stripe.com",
		"api.openai.com",
		"api.anthropic.com",
		"api.slack.com",
		"discord.com/api",
		"api.notion.com",
		"api.linear.app",
		"api.vercel.com",
		"api.heroku.com",
		"api.netlify.com",
		"api.cloudflare.com",
		"api.dropboxapi.com",
		"api.twilio.com",
		"api.sendgrid.com",
		"api.mailgun.net",
		"api.airtable.com",
		"api.figma.com",
		"api.zoom.us",
		"api.atlassian.com",
	}

	lower := strings.ToLower(specURL)
	for _, domain := range officialDomains {
		if strings.Contains(lower, domain) {
			return true
		}
	}
	return false
}
