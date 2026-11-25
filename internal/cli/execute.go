package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/request"
	"github.com/wojons/muster/pkg/response"

	"github.com/totalwindupflightsystems/musterflow/internal/auth"
	"github.com/totalwindupflightsystems/musterflow/internal/retrypin"
)

// ExecuteOptions configures an API call from CLI flags.
type ExecuteOptions struct {
	Method     string
	BaseURL    string
	Path       string
	PathParams map[string]string // param name → value from positional args
	QueryFlags map[string]string // flag name → param name for query params
	BodyFlags  map[string]string // flag name → body property name
	Format     string            // "table", "json", "yaml", "csv", "jsonl", "parquet"
	Raw        bool              // output raw response
	AuthToken  string            // optional bearer token (explicit override)
	Output     string            // output file path (auto-detects format from extension)
	// TimeoutSeconds caps the whole request (PERF-001): when > 0 the call
	// carries a context deadline and builder.WithTimeout is applied. 0 keeps
	// today's engine behavior (30 s per attempt, up to 3 attempts).
	TimeoutSeconds int
	// Auto-resolved auth
	AuthManager *auth.Manager // auth manager for auto-resolved credentials
	APIID       string        // API ID for credential lookup
	// Retries resolves the retry policy for this call (DF-044): nil = not
	// set — writes (POST/PATCH) send exactly one request and idempotent
	// methods keep the engine's default attempt ceiling; 0 hard-disables
	// retries for every method; N >= 2 is the explicit opt-in that lets
	// writes replay (which may duplicate a write on a backend that
	// commits-then-errors). See internal/retrypin.
	Retries *int
}

// BuildRequest constructs an *http.Request from ExecuteOptions and cobra command flags.
func BuildRequest(cmd *cobra.Command, opts ExecuteOptions) (*request.Builder, error) {
	builder := request.NewBuilder(opts.BaseURL, opts.Path, opts.Method)

	// Set path params
	for paramName, flagName := range opts.PathParams {
		val, err := cmd.Flags().GetString(flagName)
		if err != nil {
			return nil, fmt.Errorf("get flag %s: %w", flagName, err)
		}
		if val != "" {
			builder.WithPathParam(paramName, val)
		}
	}

	// Set query params from flags that were explicitly changed
	for flagName, paramName := range opts.QueryFlags {
		if cmd.Flag(flagName) != nil && cmd.Flag(flagName).Changed {
			val := cmd.Flag(flagName).Value.String()
			builder.WithQueryParam(paramName, val)
		}
	}

	// Set JSON body from body flags that were explicitly changed
	if len(opts.BodyFlags) > 0 {
		bodyMap := make(map[string]interface{})
		hasBody := false
		for flagName, propName := range opts.BodyFlags {
			if cmd.Flag(flagName) != nil && cmd.Flag(flagName).Changed {
				val := cmd.Flag(flagName).Value.String()
				bodyMap[propName] = val
				hasBody = true
			}
		}
		if hasBody {
			bodyJSON, err := json.Marshal(bodyMap)
			if err != nil {
				return nil, fmt.Errorf("marshal body: %w", err)
			}
			builder.JSONBody(string(bodyJSON))
			builder.SetHeader("Content-Type", "application/json")
		}
	}

	// Auto-resolve auth from auth manager if not explicitly provided
	if opts.AuthToken == "" && opts.APIID != "" {
		mgr := opts.AuthManager
		if mgr == nil {
			mgr = authMgr // fall back to global auth manager
		}
		if mgr != nil {
			if cred, err := mgr.Get(opts.APIID); err == nil && cred.IsConfigured() {
				auth.InjectAuthHeader(cred, func(k, v string) {
					builder.SetHeader(k, v)
				})
			}
		}
	}

	// Set auth from explicit flag (always takes precedence over auto-resolved)
	if opts.AuthToken != "" {
		builder.SetHeader("Authorization", "Bearer "+opts.AuthToken)
	}

	return builder, nil
}

// ExecuteAndFormat executes the request and formats the output.
//
// PERF-001: the context carries SIGINT/SIGTERM notification so a stalled
// upstream (a TCP black-hole peer) is interrupted instead of hanging the
// process, and — when opts.TimeoutSeconds is set — a deadline capping the
// WHOLE call (the engine's built-in timeout is per attempt, and its retry
// loop makes 3 × 30 s = ~90 s stall the unconfigured default). The seams
// below exist so tests never send real signals.
func ExecuteAndFormat(cmd *cobra.Command, builder *request.Builder, opts ExecuteOptions) error {
	ctx, stop := newExecContext()
	defer stop()
	if opts.TimeoutSeconds > 0 {
		applyBuilderTimeout(builder, opts.TimeoutSeconds)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	resp, err := executeUnderRetryPin(builder, opts, ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return interruptedError()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return timedOutError(opts.TimeoutSeconds)
		}
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Resolve format: explicit -o/--output format > --output-file extension > default
	format := opts.Format
	if outputFlag != "" {
		opts.Output = outputFlag
	}
	if format == "" && opts.Output != "" {
		if detected := DetectFormat(opts.Output); detected != "" {
			format = detected
		}
	}
	if format == "" {
		format = "table"
	}

	// Resolve output destination: --output > stdout
	out := cmd.OutOrStdout()
	if out == nil {
		out = os.Stdout
	}
	if opts.Output != "" {
		f, err := os.Create(opts.Output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer func() { _ = f.Close() }()
		out = f
	}

	// Raw mode — just dump the body
	if opts.Raw || len(body) == 0 {
		_, _ = fmt.Fprintln(out, string(body))
		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %s", resp.Status)
		}
		return nil
	}

	// Try to parse as JSON
	var data interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		// Not JSON — print raw
		_, _ = fmt.Fprintln(out, string(body))
		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %s", resp.Status)
		}
		return nil
	}

	// Format output
	switch format {
	case "json":
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("format JSON: %w", err)
		}
		_, _ = fmt.Fprintln(out, string(pretty))
	case "csv":
		if err := writeCSV(out, data); err != nil {
			return fmt.Errorf("format CSV: %w", err)
		}
	case "jsonl":
		if err := writeJSONL(out, data); err != nil {
			return fmt.Errorf("format JSONL: %w", err)
		}
	case "parquet":
		if err := writeParquet(out, data); err != nil {
			return fmt.Errorf("format Parquet: %w — try -o json or --output-file <file>.json instead", err)
		}
	case "table":
		printTable(out, data)
	case "yaml":
		// Fall back to JSON for now — yaml formatting will come later
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("format JSON: %w", err)
		}
		_, _ = fmt.Fprintln(out, string(pretty))
	default:
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("format JSON: %w", err)
		}
		_, _ = fmt.Fprintln(out, string(pretty))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return nil
}

// printTable prints JSON data as an aligned table.
func printTable(out io.Writer, data interface{}) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	switch v := data.(type) {
	case map[string]interface{}:
		for key, val := range v {
			_, _ = fmt.Fprintf(w, "%s	%v	\n", key, formatValue(val))
		}
	case []interface{}:
		if len(v) == 0 {
			_, _ = fmt.Fprintln(out, "(empty)")
			return
		}
		// Collect all unique keys
		keys := collectKeys(v)
		// Print header
		_, _ = fmt.Fprintf(w, "%s	\n", strings.Join(keys, "	"))
		// Print rows
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				var row []string
				for _, k := range keys {
					row = append(row, formatValue(m[k]))
				}
				_, _ = fmt.Fprintf(w, "%s	\n", strings.Join(row, "	"))
			}
		}
	default:
		_, _ = fmt.Fprintf(w, "%v	\n", v)
	}
	_ = w.Flush()
}

func collectKeys(items []interface{}) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			for k := range m {
				if !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
		}
	}
	return keys
}

func formatValue(v interface{}) string {
	if v == nil {
		return "-"
	}
	var s string
	switch n := v.(type) {
	case json.Number:
		s = string(n)
	case float64:
		s = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", n), "0"), ".")
	default:
		s = fmt.Sprintf("%v", v)
	}
	// Truncate long values for table display
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// ensure import of response package (compile check)
var _ = response.HTTPError{}

// newExecContext builds the context ExecuteAndFormat runs the request in.
// Production: SIGINT/SIGTERM-aware NotifyContext (PERF-001). Tests swap this
// seam to inject an already-cancelled/timed context instead of real signals.
var newExecContext = defaultNewExecContext

func defaultNewExecContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// applyBuilderTimeout forwards the configured timeout to the engine builder.
// NOTE: the engine's Builder.WithTimeout is currently implemented as a no-op
// (pkg/request/builder.go); the load-bearing cap is the context deadline set
// in ExecuteAndFormat, which the engine threads into every attempt. The seam
// exists so musterflow's half of the contract stays observable and so an
// engine that later honors WithTimeout is picked up without a musterflow
// change.
var applyBuilderTimeout = func(b *request.Builder, secs int) {
	if secs > 0 {
		b.WithTimeout(time.Duration(secs) * time.Second)
	}
}

// interruptedError names an abort caused by SIGINT/SIGTERM.
func interruptedError() error {
	return fmt.Errorf("canceled: interrupted by SIGINT before the upstream answered")
}

// executeUnderRetryPin sends the request with the DF-044 retry pin applied:
// internal/retrypin holds MUSTER_MAX_RETRIES around builder.Execute (the
// engine resolves the attempt ceiling from the environment per call) so that
// POST/PATCH default to exactly one attempt and --retries drives everything
// else. The variable is restored before this returns. The method comes from
// the options when set; callers that carry it only on the builder (the
// historical ExecuteAndFormat contract) fall back to builder.Method.
func executeUnderRetryPin(builder *request.Builder, opts ExecuteOptions, ctx context.Context) (*http.Response, error) {
	retries := retrypin.NotSet
	if opts.Retries != nil {
		retries = *opts.Retries
	}
	method := opts.Method
	if method == "" {
		method = builder.Method
	}
	var resp *http.Response
	var err error
	if pinErr := retrypin.Around(method, retries, func() error {
		resp, err = builder.Execute(ctx)
		return err
	}); pinErr != nil && err == nil {
		return nil, pinErr
	}
	return resp, err
}

// timedOutError names a config-timeout exhaustion on a stalled upstream.
func timedOutError(secs int) error {
	return fmt.Errorf("canceled: no upstream answer within the configured %d-second timeout — raise it with 'musterflow config set timeout <seconds>' or check the API's base URL", secs)
}
