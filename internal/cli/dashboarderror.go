package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// dashboardTokenConfigKey is the config-file key `config set dashboard_token`
// writes. The DF-052 error text names it next to the environment variable, so
// both ways to present the token are visible in one message.
const dashboardTokenConfigKey = "dashboard_token"

// isDashboardAuthStatus reports whether a dashboard HTTP status means the
// request was rejected because the CLI failed to authenticate (a wrong or
// missing token). Everything else — timeouts, connection errors, 404s, 5xx —
// keeps its existing failure mode and must not be reclassified as auth.
func isDashboardAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

// dashboardAuthFailure is the typed error returned by dashboardAuthError, so
// callers can classify the failure with errors.As without string matching
// (isDashboardAuthError).
type dashboardAuthFailure struct {
	msg string
}

func (e *dashboardAuthFailure) Error() string { return e.msg }

func isDashboardAuthError(err error) bool {
	var want *dashboardAuthFailure
	return errors.As(err, &want)
}

// dashboardAuthErr records a dashboard authentication rejection observed while
// building the command tree. When the dashboard fetch is rejected (401/403,
// DF-052), loadAPISubcommands registers no per-API subcommands and cobra then
// reports `unknown command "<api>" for "musterflow"` — the symptom, never the
// cause. main's ResolveExecutionError substitutes this error for that shape.
//
// dashboardAuthError sets it as a side effect (an auth rejection anywhere is
// the same failure the caller hit); SetDashboardAuthError(nil) clears it, so
// tests can isolate scenarios.
var dashboardAuthErr error

// SetDashboardAuthError records or clears (nil) the dashboard authentication
// failure used by ResolveExecutionError.
func SetDashboardAuthError(err error) {
	dashboardAuthErr = err
}

// DashboardAuthError returns the recorded dashboard authentication failure,
// or nil when none was observed.
func DashboardAuthError() error {
	return dashboardAuthErr
}

// dashboardAuthError renders the DF-052 authentication failure: the tokened
// dashboard rejected the CLI's request. The error names the cause and both
// ways to present the token; it must never surface as an unknown-command
// error or as a registry-fallback outcome. relayed, when non-empty, is the
// dashboard's own JSON error body (DF-013/DF-035) — quoted so the dashboard's
// cause stays verbatim.
func dashboardAuthError(status int, relayed string) error {
	msg := fmt.Sprintf(
		"dashboard authentication failed (HTTP %d): the CLI must present the dashboard token — export %s or set %s in the config, then retry",
		status, config.DashboardTokenEnvVar, dashboardTokenConfigKey,
	)
	if relayed != "" {
		msg += " (dashboard said: " + relayed + ")"
	}
	err := &dashboardAuthFailure{msg: msg}
	dashboardAuthErr = err
	return err
}

// dashboardStatusError converts a non-success dashboard status at the sites
// that do not decode an error body. 401/403 name the authentication failure
// (DF-052); every other status keeps the historical bare wording.
func dashboardStatusError(status int) error {
	if isDashboardAuthStatus(status) {
		return dashboardAuthError(status, "")
	}
	return fmt.Errorf("dashboard returned HTTP %d", status)
}

// dashboardActionError is dashboardStatusError with the routed verbs' action
// prefix and the dashboard's relayed JSON error body (empty when the body
// carried none). For non-auth statuses the wording matches the existing
// decode-and-relay pattern exactly: `action: <relayed>` when a body was
// decoded, `action: dashboard returned HTTP <d>` otherwise. The empty-relayed
// non-auth arm is the historical "dashboard returned HTTP %d" text.
func dashboardActionError(action string, status int, relayed string) error {
	if isDashboardAuthStatus(status) {
		return dashboardAuthError(status, relayed)
	}
	if relayed != "" {
		return fmt.Errorf("%s: %s", action, relayed)
	}
	return fmt.Errorf("%s: dashboard returned HTTP %d", action, status)
}
