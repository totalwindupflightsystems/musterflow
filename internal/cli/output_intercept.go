// Package cli provides output format interception for generated commands.
//
// The engine (github.com/wojons/muster) only supports table, json, and yaml
// output formats natively.  CSV, JSONL, and Parquet are handled musterflow-side
// by intercepting the generated leaf's execution: force the engine to render
// JSON, capture the output, and transform it with the local writers in
// formats.go / parquet_stub.go.  The --output-file flag (registered on root
// as a persistent flag) is also handled here so that output is written to the
// specified file regardless of format.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// parseFlagFromArgs scans args for a string flag and returns its value.
// Supports "--long <val>", "-short <val>", "--long=<val>", "-short=<val>".
// Returns the value and true if found, "" and false otherwise.
func parseFlagFromArgs(args []string, longName, shortName string) (string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--"+longName || (shortName != "" && args[i] == "-"+shortName) {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(args[i], "--"+longName+"=") {
			return strings.TrimPrefix(args[i], "--"+longName+"="), true
		}
		if shortName != "" && strings.HasPrefix(args[i], "-"+shortName+"=") {
			return strings.TrimPrefix(args[i], "-"+shortName+"="), true
		}
	}
	return "", false
}

// parseBoolFlagFromArgs scans args for a boolean flag.
// Returns true if the flag is present and not explicitly set to false/0.
func parseBoolFlagFromArgs(args []string, longName, shortName string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == "--"+longName || (shortName != "" && args[i] == "-"+shortName) {
			return true
		}
		if strings.HasPrefix(args[i], "--"+longName+"=") {
			v := strings.TrimPrefix(args[i], "--"+longName+"=")
			return v == "true" || v == "1"
		}
		if shortName != "" && strings.HasPrefix(args[i], "-"+shortName+"=") {
			v := strings.TrimPrefix(args[i], "-"+shortName+"=")
			return v == "true" || v == "1"
		}
	}
	return false
}

// stripFlagFromArgs returns a copy of args with the specified flag and its
// value removed.  Handles "--long <val>", "-short <val>", "--long=<val>",
// "-short=<val>" forms.  For string flags, the next token is consumed as the
// value if it does not start with "-".  For boolean flags (isBool=true), only
// the flag token itself is removed (boolean flags don't take a separate value
// token in the "--flag" form).
func stripFlagFromArgs(args []string, longName, shortName string, isBool ...bool) []string {
	boolFlag := len(isBool) > 0 && isBool[0]
	var result []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--"+longName || (shortName != "" && args[i] == "-"+shortName) {
			if !boolFlag && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++ // skip value
			}
			continue
		}
		if strings.HasPrefix(args[i], "--"+longName+"=") {
			continue
		}
		if shortName != "" && strings.HasPrefix(args[i], "-"+shortName+"=") {
			continue
		}
		result = append(result, args[i])
	}
	return result
}

// isExtendedFormat returns true for output formats the engine does not
// support natively (csv, jsonl, parquet).  These require musterflow-side
// interception: force JSON from the engine, then transform with local writers.
func isExtendedFormat(format string) bool {
	return format == "csv" || format == "jsonl" || format == "parquet"
}

// isEngineNativeFormat returns true for formats the engine renders itself, so
// a --output-file extension match only needs -o <format> passed through
// (table is the engine's default and needs no flag at all).
func isEngineNativeFormat(format string) bool {
	return format == "json" || format == "yaml"
}

// forceEngineOutputFormat makes the generated leaf render format: it prepends
// -o <format> to args and pins the leaf's "output" flag after cobra parses
// flags, because flag state from the outer parse can bleed through cobra's
// caching.  The returned restore func must be deferred by the caller.
func forceEngineOutputFormat(target *cobra.Command, args []string, format string) ([]string, func()) {
	cleanArgs := append([]string{"-o", format}, args...)

	originalPreRunE := target.PreRunE
	target.PreRunE = func(cmd *cobra.Command, a []string) error {
		_ = cmd.Flags().Set("output", format)
		if originalPreRunE != nil {
			return originalPreRunE(cmd, a)
		}
		return nil
	}
	return cleanArgs, func() { target.PreRunE = originalPreRunE }
}

// createOutputFile creates the output file and its parent directories.
func createOutputFile(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	return f, nil
}

// interceptOutput checks whether the requested output format or --output-file
// requires musterflow-side interception.  If so, it executes the target and
// transforms the output, returning intercepted=true.  If not, it returns
// intercepted=false and the caller should execute the target itself.
//
// Extended formats (csv, jsonl, parquet): force the engine to render JSON by
// setting the "output" flag to "json" and stripping -o from the args, capture
// the JSON in a buffer, parse it, and transform with the local writers
// (writeCSV/writeJSONL/writeParquet).  If --output-file is also set, write the
// transformed output to that file; otherwise write to stdout.
//
// --output-file with a non-extended format (table, json, yaml) or --raw:
// redirect the engine's stdout to the file so the engine's native output goes
// directly to the file.  When the format came from the file extension
// (.json/.yaml) and no explicit -o was given, the engine is told to render that
// format instead of its default table (DF-049).
//
// Format resolution order: explicit -o flag > --output-file extension > default "table".
// Destination: --output-file wins for the destination; stdout otherwise.
func interceptOutput(target *cobra.Command, remaining []string) (bool, error) {
	// 1. Determine the requested format from -o/--output flag.
	format, hasFormat := parseFlagFromArgs(remaining, "output", "o")

	// 2. Determine the output file from --output-file flag or package var.
	outputFile, _ := parseFlagFromArgs(remaining, "output-file", "")
	if outputFile == "" {
		outputFile = outputFlag // package var from root.go
	}

	// 3. If --output-file is set and no explicit -o flag, detect format
	//    from the file extension.
	if outputFile != "" && !hasFormat {
		if d := DetectFormat(outputFile); d != "" {
			format = d
		}
	}

	// 4. Default format is "table" (matches the engine's default).
	if format == "" {
		format = "table"
	}

	// 5. Check for --raw flag (engine dumps raw body, skip format transform).
	isRaw := parseBoolFlagFromArgs(remaining, "raw", "r")

	extendedFmt := isExtendedFormat(format)
	hasOutFile := outputFile != ""

	// 6. No interception needed: engine handles table/json/yaml to stdout.
	if !extendedFmt && !hasOutFile {
		return false, nil
	}

	// 7. Save original stdout and restore on return.
	originalOut := target.OutOrStdout()
	defer target.SetOut(originalOut)

	// 8. Open output file if needed.
	var fileOut io.Writer
	if hasOutFile {
		f, err := createOutputFile(outputFile)
		if err != nil {
			return true, err
		}
		defer func() { _ = f.Close() }()
		fileOut = f
	}

	// 9. Extended format (csv/jsonl/parquet) without --raw: force JSON from
	//    the engine, capture, parse, and transform with local writers.
	if extendedFmt && !isRaw {
		// The target must have an "output" flag (generated leaves do).
		if target.Flags().Lookup("output") == nil {
			return false, nil
		}

		// Strip -o/--output from args so we can replace it with -o json
		// (forcing the engine to render JSON, which we then transform).
		// Also strip --output-file since we've already handled it.
		cleanArgs := stripFlagFromArgs(remaining, "output", "o")
		cleanArgs = stripFlagFromArgs(cleanArgs, "output-file", "")
		// Prepend -o json and pin the flag after cobra parses it.
		cleanArgs, restorePreRun := forceEngineOutputFormat(target, cleanArgs, "json")
		defer restorePreRun()
		target.SetArgs(cleanArgs)

		// Capture the engine's JSON output.
		var buf bytes.Buffer
		target.SetOut(&buf)

		if err := target.Execute(); err != nil {
			return true, err
		}

		// Determine the destination for the transformed output.
		out := fileOut
		if out == nil {
			out = originalOut
		}

		// Parse the captured JSON.
		var data interface{}
		if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
			// The engine wrote non-JSON (e.g. an error message or raw text).
			// Write it verbatim to the destination.
			_, _ = fmt.Fprint(out, buf.String())
			return true, nil
		}

		// Transform with the appropriate local writer.
		switch format {
		case "csv":
			if err := writeCSV(out, data); err != nil {
				return true, fmt.Errorf("format CSV: %w", err)
			}
		case "jsonl":
			if err := writeJSONL(out, data); err != nil {
				return true, fmt.Errorf("format JSONL: %w", err)
			}
		case "parquet":
			if err := writeParquet(out, data); err != nil {
				return true, fmt.Errorf("format Parquet: %w", err)
			}
		}

		return true, nil
	}

	// 10. Non-extended format (or --raw) with --output-file: redirect the
	//     engine's stdout to the file and let it execute normally.
	//     Strip --output-file from args since we've already handled it.
	cleanArgs := stripFlagFromArgs(remaining, "output-file", "")

	// When the format came from the file extension (no explicit -o), the engine
	// still has to be told what to render: otherwise --output-file out.json
	// receives the engine's default table, which contradicts the flag's
	// "format auto-detected from extension" promise (DF-049).  --raw and the
	// extended formats settled their own paths above.
	restorePreRun := func() {}
	if !hasFormat && !isRaw && isEngineNativeFormat(format) && target.Flags().Lookup("output") != nil {
		cleanArgs, restorePreRun = forceEngineOutputFormat(target, cleanArgs, format)
	}
	defer restorePreRun()

	target.SetArgs(cleanArgs)

	if fileOut != nil {
		target.SetOut(fileOut)
	}

	if err := target.Execute(); err != nil {
		return true, err
	}

	return true, nil
}
