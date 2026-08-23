package mcp

import (
	"context"
	"time"
)

// DefaultRefreshBackoff is the exponential backoff schedule used by
// RefreshWithRetry when no explicit schedule is provided. It starts at 1s,
// doubles each attempt, and caps at 30s. After the schedule is exhausted the
// last (capped) value repeats indefinitely.
//
// Schedule: 1s, 2s, 4s, 8s, 16s, 30s, 30s, 30s, ...
var DefaultRefreshBackoff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

// RefreshWithRetry repeatedly calls refresh until it succeeds (returns nil) or
// ctx is cancelled. On each failed attempt it calls onWarning (if non-nil) with
// the error, then waits for the next backoff duration before retrying. Once
// the explicit backoff schedule is exhausted, the last (capped) entry repeats
// indefinitely.
//
// This recovers from transient MCP tool refresh failures (network down, spec
// host unreachable at boot) without requiring a restart. See GAP-016.
func RefreshWithRetry(ctx context.Context, refresh func() error, backoffs []time.Duration, onWarning func(error)) error {
	if len(backoffs) == 0 {
		backoffs = DefaultRefreshBackoff
	}
	capDelay := backoffs[len(backoffs)-1]

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := refresh()
		if err == nil {
			return nil
		}

		if onWarning != nil {
			onWarning(err)
		}

		var delay time.Duration
		if attempt < len(backoffs) {
			delay = backoffs[attempt]
		} else {
			delay = capDelay
		}
		attempt++

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
