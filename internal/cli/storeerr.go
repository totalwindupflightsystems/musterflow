package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// storeOpenErr is why the API registry could not be opened; main records it
// before the command tree runs. nil means the registry is healthy.
var storeOpenErr error

// SetStoreError records a registry store-open failure so commands that read the
// registry fail closed instead of reporting an empty registry (DF-045). main
// calls it after registry.Load() fails; pass nil to clear it.
func SetStoreError(err error) {
	storeOpenErr = err
}

// StoreUnavailable returns the actionable error for an unusable registry, or
// nil when the registry is healthy. A DuckDB lock conflict names the
// lock-holding process and both ways to route around it.
func StoreUnavailable() error {
	if storeOpenErr == nil {
		return nil
	}
	lock, ok := app.AsStoreLock(storeOpenErr)
	if !ok {
		return fmt.Errorf("cannot read the API registry: %w", storeOpenErr)
	}
	return lockConflictError(lock)
}

// lockConflictError renders the DF-045 message. It has to answer three
// questions in one breath: who holds the lock, why that makes the registry look
// empty, and the exact command that restores routing.
func lockConflictError(lock *app.StoreLock) error {
	configuredPort := 0
	if cfg, err := loadConfig(); err == nil {
		configuredPort = cfg.Port
	}

	var b strings.Builder
	b.WriteString("cannot read the API registry: ")
	if lock.DBPath != "" {
		fmt.Fprintf(&b, "%s is", lock.DBPath)
	} else {
		b.WriteString("the local database is")
	}
	switch {
	case lock.PID > 0 && lock.Holder != "":
		fmt.Fprintf(&b, " locked by another musterflow process (PID %d, %s).\n", lock.PID, lock.Holder)
	case lock.PID > 0:
		fmt.Fprintf(&b, " locked by another musterflow process (PID %d).\n", lock.PID)
	default:
		b.WriteString(" locked by another musterflow process.\n")
	}
	b.WriteString("That process holds DuckDB's single-writer lock — normally the dashboard — so the CLI cannot see the connected APIs and will not report the registry as empty.\n")

	b.WriteString("Fix: ")
	if lock.Port > 0 {
		fmt.Fprintf(&b, "run `musterflow config set port %d`", lock.Port)
		if configuredPort > 0 && configuredPort != lock.Port {
			fmt.Fprintf(&b, " (config currently says %d)", configuredPort)
		}
		fmt.Fprintf(&b, ", or pass `--dashboard-addr 127.0.0.1:%d` to this command", lock.Port)
	} else {
		b.WriteString("run `musterflow config set port <port>` with the dashboard's port, or pass `--dashboard-addr host:port` to this command")
	}
	b.WriteString(" to route through the running dashboard.")

	return errors.New(b.String())
}

// ResolveExecutionError replaces cobra's "unknown command" error with the real
// cause when a recorded failure explains why the command could not exist. The
// registry-open failure (DF-045, storeOpenErr) and the dashboard authentication
// rejection (DF-052, dashboardAuthErr) both prevent the per-API subcommands
// from being registered, so every generated command collapses into
// `unknown command "<api>" for "musterflow"` — the symptom, not the cause.
// Unknown subcommands of real commands (e.g. `config bogus`) are left alone:
// they are not registry-dependent.
func ResolveExecutionError(execErr error) error {
	if execErr == nil {
		return execErr
	}
	msg := execErr.Error()
	if strings.HasPrefix(msg, "unknown command ") && strings.Contains(msg, `for "musterflow"`) {
		if authErr := DashboardAuthError(); authErr != nil {
			return authErr
		}
		if err := StoreUnavailable(); err != nil {
			return err
		}
	}
	return execErr
}
