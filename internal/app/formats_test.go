package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// outputFormatsFromUsage extracts the format names the generator renders inside
// a generated --output usage string, e.g. "Output format (table, json)".
func outputFormatsFromUsage(usage string) []string {
	open := strings.Index(usage, "(")
	closeIdx := strings.LastIndex(usage, ")")
	if open < 0 || closeIdx <= open {
		return nil
	}
	parts := strings.Split(usage[open+1:closeIdx], ",")
	formats := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			formats = append(formats, p)
		}
	}
	return formats
}

// TestSupportedOutputFormats proves the authoritative declaration names exactly
// the six renderable formats, in the documented order.
func TestSupportedOutputFormats(t *testing.T) {
	want := []string{"table", "json", "yaml", "csv", "jsonl", "parquet"}
	if !slices.Equal(SupportedOutputFormats, want) {
		t.Fatalf("SupportedOutputFormats = %v, want %v", SupportedOutputFormats, want)
	}
	if DefaultOutputFormat != "table" {
		t.Fatalf("DefaultOutputFormat = %q, want %q", DefaultOutputFormat, "table")
	}
}

// TestGenerateCommandConfigCarriesAllFormats proves the config fed to the
// generator advertises the authoritative format list rather than a stale
// subset, and that the list is handed over as a copy (a generator mutating its
// config cannot corrupt the package-level declaration).
func TestGenerateCommandConfigCarriesAllFormats(t *testing.T) {
	cfg := GenerateCommandConfig(&APIConnection{Name: "formats-api"})
	if !slices.Equal(cfg.SupportedFormats, SupportedOutputFormats) {
		t.Fatalf("config SupportedFormats = %v, want %v", cfg.SupportedFormats, SupportedOutputFormats)
	}
	if cfg.DefaultFormat != "table" {
		t.Errorf("config DefaultFormat = %q, want %q", cfg.DefaultFormat, "table")
	}

	cfg.SupportedFormats[0] = "mutated"
	if SupportedOutputFormats[0] != "table" {
		t.Fatalf("package declaration aliased by generator config: SupportedOutputFormats[0] = %q", SupportedOutputFormats[0])
	}
}

// TestGeneratedOutputHelpAdvertisesEveryFormat builds the real command tree for
// a fixture spec and asserts every generated operation's -o/--output help lists
// all declared formats. This is the regression guard for DF-051: the help text
// used to advertise only table/json/yaml while the CLI rendered six formats.
func TestGeneratedOutputHelpAdvertisesEveryFormat(t *testing.T) {
	r := newTestRegistry(t)
	dir := t.TempDir()
	specPath := writeTempSpec(t, dir, "two.json", twoResourceSpec)

	res, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	idx, err := ReadPlanIndex(r.DataDir(), res.Connection)
	if err != nil {
		t.Fatalf("ReadPlanIndex: %v", err)
	}
	ops, err := idx.ResourceOperations(r.DataDir(), res.Connection, "items")
	if err != nil {
		t.Fatalf("ResourceOperations(items): %v", err)
	}
	commands, err := CommandsFromPlan(idx.Plan(ops), res.Connection)
	if err != nil {
		t.Fatalf("CommandsFromPlan: %v", err)
	}

	var leaves []*cobra.Command
	for _, group := range commands {
		leaves = append(leaves, group.Commands()...)
	}
	if len(leaves) == 0 {
		t.Fatalf("fixture produced no operation commands (groups: %v)", commandNames(commands))
	}

	for _, leaf := range leaves {
		flag := leaf.Flags().Lookup("output")
		if flag == nil {
			t.Errorf("%s: no -o/--output flag on generated command", leaf.Name())
			continue
		}
		advertised := outputFormatsFromUsage(flag.Usage)
		for _, format := range SupportedOutputFormats {
			if !slices.Contains(advertised, format) {
				t.Errorf("%s: --output help %q does not advertise supported format %q (advertised: %v)",
					leaf.Name(), flag.Usage, format, advertised)
			}
		}
		for _, format := range advertised {
			if !slices.Contains(SupportedOutputFormats, format) {
				t.Errorf("%s: --output help advertises %q, which is not in SupportedOutputFormats",
					leaf.Name(), format)
			}
		}
		if flag.DefValue != DefaultOutputFormat {
			t.Errorf("%s: --output default = %q, want %q", leaf.Name(), flag.DefValue, DefaultOutputFormat)
		}
	}
}
