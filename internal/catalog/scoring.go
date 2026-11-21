package catalog

import (
	"net/url"
	"strings"
)

// Score computes a quality score (0-10) for a catalog entry based on where its
// spec document is published and on the metadata it carries.
//
// Both bonuses are reachable from the generator path: Score runs over the entry
// ConnectionToCatalogEntry builds (type "api", a spec URL, and the API base URL
// the connection declares), so the official-domain arm resolves the SPEC host —
// docs, openapi and api subdomains included — against the provider's domains and
// against the declared base host, and the structured-spec arm credits the API
// connector type the generator actually emits. Before CATALOG-005 both arms were
// dead code for every generated entry.
func Score(entry CatalogEntry) (int, string) {
	score := 0

	// Official domain bonus (+5)
	if isOfficialSpecHost(entry.SpecURL, entry.BaseURL) {
		score += 5
	}

	// Structured spec bonus (+2)
	if hasStructuredSpec(entry) {
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

// officialProviderDomains is the curated registry of API providers that publish
// their own spec. Entries are registrable domains (eTLD+1) rather than single
// hosts, so every host a provider uses for documentation is covered by one
// entry: api.stripe.com, docs.hetzner.cloud and openapi.vercel.sh all resolve to
// their provider here.
var officialProviderDomains = []string{
	// Legacy API-base-host allowlist (TASK-014), reduced to registrable domains.
	"github.com",
	"stripe.com",
	"openai.com",
	"anthropic.com",
	"slack.com",
	"discord.com",
	"notion.com",
	"linear.app",
	"vercel.com",
	"heroku.com",
	"netlify.com",
	"cloudflare.com",
	"dropboxapi.com",
	"twilio.com",
	"sendgrid.com",
	"mailgun.net",
	"airtable.com",
	"figma.com",
	"zoom.us",
	"atlassian.com",
	// Providers proven by the pass-2 staging run (CATALOG-005): each serves its
	// spec from a docs/openapi/bare host instead of an api.* base host, e.g.
	// docs.hetzner.cloud, openapi.vercel.sh, api.weather.gov,
	// huggingface.co/.well-known/openapi.json.
	"vercel.sh",
	"hetzner.cloud",
	"weather.gov",
	"huggingface.co",
}

// docHostLabels are the leading host labels providers use for the host that
// publishes their documentation and machine-readable specs — docs.hetzner.cloud,
// openapi.vercel.sh, api.weather.gov, developer.mozilla.org. A spec served from
// such a host, on the same provider domain as the API base host the entry
// declares, is the provider's own publication even though the two hosts differ.
var docHostLabels = map[string]bool{
	"api":        true,
	"docs":       true,
	"doc":        true,
	"developer":  true,
	"developers": true,
	"openapi":    true,
	"spec":       true,
	"specs":      true,
	"raw":        true,
}

// isOfficialSpecHost reports whether an entry's spec document is published by the
// API provider itself. Three tests, cheapest first:
//
//  1. the spec host is a known official provider domain (any subdomain of it);
//  2. the spec host is exactly the API base host the entry declares — the
//     provider serving its spec next to its API (huggingface.co, api.weather.gov);
//  3. the spec host is a documentation-style host on the SAME provider domain as
//     the declared base host (docs.hetzner.cloud next to api.hetzner.cloud).
//
// A spec mirrored on a third-party host matches none of them: ollama's spec URL
// is raw.githubusercontent.com (GitHub's CDN, not ollama's host) while its base
// URL is localhost:11434, so its mirror does not earn the bonus. That mirror is
// not the provider's own publication, which is what this bonus signals.
func isOfficialSpecHost(specURL, baseURL string) bool {
	if isOfficialDomain(specURL) {
		return true
	}
	specHost := urlHost(specURL)
	if specHost == "" {
		return false
	}
	baseHost := urlHost(baseURL)
	if baseHost == "" {
		return false
	}
	if specHost == baseHost {
		return true
	}
	return registrableDomain(specHost) == registrableDomain(baseHost) && isDocHost(specHost)
}

// isOfficialDomain reports whether a spec URL is hosted on a known official API
// provider domain.
//
// It matches the URL's HOST against the registry's registrable domains, so every
// documentation/API host a known provider uses qualifies (api.stripe.com,
// docs.hetzner.cloud, openapi.vercel.sh) while an unrelated host that merely
// mentions a provider name in its path or query does not. The previous
// implementation substring-matched the whole URL against API base hosts, which is
// exactly why a docs-hosted spec could never earn this bonus.
func isOfficialDomain(specURL string) bool {
	host := urlHost(specURL)
	if host == "" {
		return false
	}
	for _, domain := range officialProviderDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// hasStructuredSpec reports whether an entry carries a machine-readable spec
// document.
//
// The catalog's own type vocabulary (client.go) describes API connectors as
// "api", and ConnectionToCatalogEntry emits exactly that, so keying the bonus on
// "openapi" alone made it unreachable for every generator-produced entry.
// "openapi" stays honored because older hand-written entries use it, and "api"
// qualifies when the entry actually points at a spec document.
func hasStructuredSpec(entry CatalogEntry) bool {
	switch entry.Type {
	case "openapi":
		return true
	case "api":
		return strings.TrimSpace(entry.SpecURL) != ""
	default:
		return false
	}
}

// isDocHost reports whether the host's leading label marks it as a
// documentation/spec host rather than a bare marketing site.
func isDocHost(host string) bool {
	first := host
	if i := strings.IndexByte(first, '.'); i >= 0 {
		first = first[:i]
	}
	return docHostLabels[first]
}

// urlHost returns the lowercased host of raw without its port, tolerating a
// scheme-less value such as "api.github.com/spec.json" (entries normally carry a
// full URL, but a host-only spec reference is no reason to lose the bonus).
func urlHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Host != "" {
		return stripPort(strings.ToLower(u.Host))
	}
	if strings.Contains(raw, "://") {
		return ""
	}
	// Scheme-less: url.Parse puts the whole value in Path.
	candidate := u.Path
	if i := strings.IndexByte(candidate, '/'); i >= 0 {
		candidate = candidate[:i]
	}
	if candidate == "" || !strings.Contains(candidate, ".") {
		return ""
	}
	return stripPort(strings.ToLower(candidate))
}

// stripPort drops a trailing :port from host.
func stripPort(host string) string {
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		if !strings.Contains(host[i+1:], "]") {
			return host[:i]
		}
	}
	return host
}

// registrableDomain returns host's registrable domain (eTLD+1) for the shapes
// the catalog sees, understanding the two-label public suffixes used behind
// ccTLDs (example.co.uk, example.com.co). Anything else keeps its last two
// labels. Ports are already removed by urlHost: this answers which organisation
// publishes the host, not which listener serves it.
func registrableDomain(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}
	n := 2
	if last := labels[len(labels)-1]; len(last) == 2 && len(labels[len(labels)-2]) <= 3 {
		n = 3
	}
	if len(labels) < n {
		n = len(labels)
	}
	return strings.Join(labels[len(labels)-n:], ".")
}
