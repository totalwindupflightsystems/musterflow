package main

import (
	"fmt"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// portChoice is the outcome of dashboard port selection for `start`.
type portChoice struct {
	// Port is the port the dashboard should bind.
	Port int
	// Note explains why a port other than the requested one was chosen. It is
	// empty when the requested port was free and used as-is.
	Note string
}

// selectStartPort decides which port the dashboard binds.
//
// An occupied port means one of two very different things:
//
//   - A MusterFlow dashboard already answers there. Refuse. Bypassing it would
//     run a second dashboard against a second store and silently split the CLI
//     and the user's data between two ports, so the operator is told to reuse
//     the running dashboard or ask for a different port explicitly.
//   - Any other process holds the port. Walk the documented range
//     [port, port+9] (config.FindPort) and serve on the first free port,
//     reporting the requested port, the foreign occupant, and the chosen port.
//
// occupied reports whether a MusterFlow dashboard answers on a port; it is a
// parameter so the walk above the requested port is never consulted in the
// dashboard-occupied case, which the tests rely on.
func selectStartPort(base int, occupied func(port int) bool) (portChoice, error) {
	if occupied(base) {
		return portChoice{}, fmt.Errorf(
			"another MusterFlow dashboard is already running on port %d — reuse it, or pass --dashboard-addr :<port> to serve on a different port",
			base)
	}

	port, err := config.FindPort(base)
	if err != nil {
		return portChoice{}, fmt.Errorf(
			"%w — free a port in that range, or pass --dashboard-addr :<port> to serve on a different port", err)
	}

	choice := portChoice{Port: port}
	if port != base {
		choice.Note = fmt.Sprintf("port %d busy (foreign process) — serving on %d", base, port)
	}
	return choice, nil
}

// dashboardPortOccupied reports whether a MusterFlow dashboard answers on port,
// using the same health-and-JSON-shape check the CLI uses for routing (DF-013):
// a bare TCP listener or any foreign HTTP server is not a dashboard.
func dashboardPortOccupied(port int) bool {
	return isMusterflowDashboard(fmt.Sprintf("127.0.0.1:%d", port))
}
