// MCP-side retry pinning (DF-044).
//
// Tool execution reaches the engine through musterMcp.ExecuteHTTP, which
// builds the request with pkg/request and sends it through builder.Execute —
// the same path as the CLI's generated commands, including the
// WithNonIdempotentReplay opt-in the builder applies whenever
// MUSTER_MAX_RETRIES is above one attempt. MCP tools have no --retries flag,
// so every write (POST/PATCH) is pinned to exactly one attempt; the engine's
// default attempt ceiling still governs idempotent reads.
package mcp

import (
	musterMcp "github.com/wojons/muster/pkg/mcp"

	"github.com/totalwindupflightsystems/musterflow/internal/retrypin"
)

// executeHTTPUnderRetryPin runs ExecuteHTTP with the DF-044 pin applied for
// the tool's method (environment restored afterwards).
func executeHTTPUnderRetryPin(config musterMcp.ExecutionConfig, args map[string]interface{}) ([]byte, error) {
	var result []byte
	var err error
	if pinErr := retrypin.Around(config.Method, retrypin.NotSet, func() error {
		result, err = musterMcp.ExecuteHTTP(config.BaseURL, config.Method, config.Path, args)
		return err
	}); pinErr != nil && err == nil {
		return nil, pinErr
	}
	return result, err
}
