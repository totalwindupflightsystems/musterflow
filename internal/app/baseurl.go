package app

import "strings"

// isAbsoluteHTTPURL reports whether raw is an absolute http(s) URL.
func isAbsoluteHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// isTemplatedBaseURL reports whether raw is a templated server URL such as
// "https://{host}/v1". The concrete host is only known per request, so such a
// value can never be stored as a connection base URL.
func isTemplatedBaseURL(raw string) bool {
	return strings.ContainsAny(raw, "{}")
}

// NormalizeBaseURL turns a raw OpenAPI servers[].url value into the base URL
// MusterFlow stores on a connection: path-only values ("/api/v3") are resolved
// against the spec origin, and bare hosts get an "https://" prefix. Absolute
// URLs are returned unchanged, as is an empty input.
func NormalizeBaseURL(raw, specURL string) string {
	if raw == "" {
		return ""
	}
	if isAbsoluteHTTPURL(raw) {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		// Path-only — resolve against the spec origin when there is one.
		if strings.HasPrefix(specURL, "http") {
			parts := strings.SplitN(specURL, "/", 4)
			if len(parts) >= 3 {
				return parts[0] + "//" + parts[2] + raw
			}
		}
		return raw
	}
	return "https://" + raw
}

// ResolveRefreshBaseURL returns the base URL a refresh should adopt, given the
// connection's current base URL, the refreshed spec's servers[0].url and the
// connection's spec URL.
//
// Only a concrete absolute http(s) server URL replaces the current value. A
// path-only ("/api/v3") or templated ("https://{host}/v1") server URL is not
// usable by itself, so the current value is preserved; when there is no current
// base URL yet, a relative server value is resolved against the spec origin
// exactly as Connect does.
func ResolveRefreshBaseURL(current, rawServerURL, specURL string) string {
	if rawServerURL == "" || isTemplatedBaseURL(rawServerURL) {
		return current
	}
	if isAbsoluteHTTPURL(rawServerURL) {
		return rawServerURL
	}
	if current != "" {
		return current
	}
	if normalized := NormalizeBaseURL(rawServerURL, specURL); isAbsoluteHTTPURL(normalized) {
		return normalized
	}
	return current
}
