package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/wojons/muster/pkg/request"
	"github.com/wojons/muster/pkg/response"
	"github.com/spf13/cobra"
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
	AuthToken  string            // optional bearer token
	Output     string            // output file path (auto-detects format from extension)
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

	// Set auth
	if opts.AuthToken != "" {
		builder.SetHeader("Authorization", "Bearer "+opts.AuthToken)
	}

	return builder, nil
}

// ExecuteAndFormat executes the request and formats the output.
func ExecuteAndFormat(cmd *cobra.Command, builder *request.Builder, opts ExecuteOptions) error {
	ctx := context.Background()
	resp, err := builder.Execute(ctx)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Resolve format: --format flag > --output extension > default
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
		defer f.Close()
		out = f
	}

	// Raw mode — just dump the body
	if opts.Raw || len(body) == 0 {
		fmt.Fprintln(out, string(body))
		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %s", resp.Status)
		}
		return nil
	}

	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		// Not JSON — print raw
		fmt.Fprintln(out, string(body))
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
		fmt.Fprintln(out, string(pretty))
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
			return fmt.Errorf("format Parquet: %w — try --format json instead", err)
		}
	case "table":
		printTable(out, data)
	case "yaml":
		// Fall back to JSON for now — yaml formatting will come later
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("format JSON: %w", err)
		}
		fmt.Fprintln(out, string(pretty))
	default:
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("format JSON: %w", err)
		}
		fmt.Fprintln(out, string(pretty))
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
			fmt.Fprintf(w, "%s\t%v\t\n", key, formatValue(val))
		}
	case []interface{}:
		if len(v) == 0 {
			fmt.Fprintln(out, "(empty)")
			return
		}
		// Collect all unique keys
		keys := collectKeys(v)
		// Print header
		fmt.Fprintf(w, "%s\t\n", strings.Join(keys, "\t"))
		// Print rows
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				var row []string
				for _, k := range keys {
					row = append(row, formatValue(m[k]))
				}
				fmt.Fprintf(w, "%s\t\n", strings.Join(row, "\t"))
			}
		}
	default:
		fmt.Fprintf(w, "%v\t\n", v)
	}
	w.Flush()
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
	s := fmt.Sprintf("%v", v)
	// Truncate long values for table display
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// ensure import of response package (compile check)
var _ = response.HTTPError{}