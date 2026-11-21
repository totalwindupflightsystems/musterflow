package dashboard

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// bearerPrefix is the only Authorization scheme the dashboard accepts. The
// scheme is matched case-insensitively (RFC 9110 §11.1); the token itself is
// compared in constant time.
const bearerPrefix = "Bearer "

// AuthDisabledWarning is printed exactly once at startup when the dashboard
// serves on loopback with no dashboard token configured. Loopback is not
// exposure, so the server keeps running — loudly.
const AuthDisabledWarning = "WARNING: dashboard auth disabled — set dashboard_token to require a bearer token"

// ListenAddr resolves the address the dashboard binds from the
// --dashboard-addr flag value (which may be empty) and the configured port.
//
// The rules exist because "host-less address" has two very different meanings:
// to the flag it reads as "just pick a port", while to net.Listen an empty host
// means every interface. This function never returns a host-less address:
//
//   - "" (flag absent)      → 127.0.0.1:<defaultPort>
//   - ":port"               → 127.0.0.1:<port>
//   - "host:port"           → host:port
//   - "[v6]:port"           → [v6]:port
//   - "host" / "port"       → error naming the accepted forms
//
// Port 0 is accepted (ephemeral port, used by in-process tests).
func ListenAddr(requested string, defaultPort int) (string, error) {
	if requested == "" {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(defaultPort)), nil
	}

	host, portStr, err := net.SplitHostPort(requested)
	if err != nil {
		return "", fmt.Errorf("invalid --dashboard-addr %q: want host:port or :port", requested)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return "", fmt.Errorf("invalid --dashboard-addr %q: %q is not a port between 0 and 65535", requested, portStr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// IsLoopbackAddr reports whether addr binds a loopback interface only:
// "localhost", 127.0.0.0/8 or ::1. Anything unparseable — including a
// host-less address — is reported as NOT loopback, so the fail-closed checks
// treat it as exposure.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// BaseURL renders the browser-facing base URL of a listen address. A loopback
// bind is rendered as http://localhost:<port> — the URL a user expects in the
// startup banner, the MCP endpoint and flow webhook URLs — while any other
// host is rendered verbatim.
func BaseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Defensive: an address we cannot parse is rendered as given rather
		// than guessed at.
		return "http://" + addr
	}
	if host == "" || isLoopbackHost(host) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// AuthEnabled reports whether a bearer token is configured, i.e. whether the
// protected routes (/api/* except /api/health, /mcp, /hooks/*) reject
// unauthenticated callers.
func (s *Server) AuthEnabled() bool {
	return s.token != ""
}

// authorize reports whether r may reach a protected route. With no token
// configured (loopback bind only — see NewServerWithToken) every caller is
// allowed; otherwise the request must carry "Authorization: Bearer <token>".
func (s *Server) authorize(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	if len(header) < len(bearerPrefix) ||
		!strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return false
	}
	presented := strings.TrimSpace(header[len(bearerPrefix):])
	// Constant time: the comparison must not leak how much of the token
	// matched. ConstantTimeCompare also returns 0 on a length mismatch.
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) == 1
}

// requireAuth wraps a handler with the bearer-token check. A rejected request
// gets a fixed 401 body: no registry, flow or definition data is ever echoed
// on an auth failure.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="musterflow"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}
