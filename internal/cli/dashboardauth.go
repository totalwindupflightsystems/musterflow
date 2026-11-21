package cli

import (
	"net/http"
	"net/url"
	"sync"
)

// dashboardToken is the bearer token a running dashboard requires
// (config `dashboard_token`, or MUSTERFLOW_DASHBOARD_TOKEN). Empty means the
// dashboard has auth disabled, which is the loopback default.
var dashboardToken string

// SetDashboardToken sets the bearer token used on requests that route through a
// running dashboard, and installs the scoped transport (idempotent).
func SetDashboardToken(token string) {
	dashboardToken = token
	installDashboardAuthTransport()
}

var dashboardAuthOnce sync.Once

// installDashboardAuthTransport wraps http.DefaultClient's transport so that
// requests bound for the dashboard carry the bearer token. Every dashboard
// call in this package goes through http.Get/http.Post/http.DefaultClient.Do,
// so one wrapper covers them all.
//
// The header is scoped to the dashboard's own origin on purpose: the same
// client fetches third-party OpenAPI specs (`connect`, `refresh`), and a
// blanket header would hand the dashboard token to every spec host.
func installDashboardAuthTransport() {
	dashboardAuthOnce.Do(func() {
		base := http.DefaultClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		http.DefaultClient.Transport = dashboardAuthTransport{base: base}
	})
}

type dashboardAuthTransport struct{ base http.RoundTripper }

func (t dashboardAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if dashboardToken != "" && sameOrigin(req.URL, dashboardBaseURL) {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+dashboardToken)
	}
	return t.base.RoundTrip(req)
}

// sameOrigin reports whether target is on the same scheme and host as base.
// Comparing full URL strings would treat "http://host:9876.example.com/…" as a
// dashboard URL.
func sameOrigin(target *url.URL, base string) bool {
	if target == nil || base == "" {
		return false
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return false
	}
	return target.Scheme == b.Scheme && target.Host == b.Host
}
