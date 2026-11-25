package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"

	"github.com/totalwindupflightsystems/musterflow/internal/retrypin"
)

// retriesFlagName is the root persistent flag that carries the caller's retry
// preference down to every generated operation leaf (cobra inherits persistent
// flags). Its default is retrypin.NotSet: no explicit preference, which pins
// DF-044's default — writes (POST/PATCH) send exactly one request, idempotent
// methods keep the engine's default attempt ceiling.
const retriesFlagName = "retries"

// retriesFlagUsage documents the flag's per-value semantics.
const retriesFlagUsage = "retry control for API calls: 0 disables all retries, 1 sends exactly one attempt, N>=2 also allows POST/PATCH to be replayed on transient failures (may duplicate a write when a backend commits-then-errors). Default: GET-type calls retry, POST/PATCH send exactly one request"

// retriesFlagValue resolves the inherited --retries flag for cmd, returning
// retrypin.NotSet when the flag is absent or untouched.
func retriesFlagValue(cmd *cobra.Command) int {
	f := cmd.Flag(retriesFlagName)
	if f == nil || !f.Changed {
		return retrypin.NotSet
	}
	n, err := strconv.Atoi(strings.TrimSpace(f.Value.String()))
	if err != nil || n < 0 {
		return retrypin.NotSet
	}
	return n
}

// executeLeafUnderRetryPin runs one generated operation leaf with the DF-044
// pin held around the whole run. The leaf's HTTP happens inside the engine
// (pkg/generator → pkg/request/builder.Execute → pkg/client), which resolves
// the attempt ceiling from MUSTER_MAX_RETRIES per request, so holding one
// environment value around target.Execute — and around the interception paths
// that re-run the leaf themselves — is the only musterflow-owned seam here.
// The method comes from the annotation the generator stamps on every leaf
// (DF-007); a leaf without it is treated as a read (engine default).
func executeLeafUnderRetryPin(target *cobra.Command, run func() error) error {
	return retrypin.Around(target.Annotations[generator.AnnotationOperationMethod], retriesFlagValue(target), run)
}
