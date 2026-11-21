package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// stringLiteralLabels returns the string literals used as the label list of
// every `switch <tagIdent> { case "…": }` statement inside fn.
func stringLiteralLabels(t *testing.T, fn *ast.FuncDecl, tagIdent string) []string {
	t.Helper()
	var labels []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		ident, ok := sw.Tag.(*ast.Ident)
		if !ok || ident.Name != tagIdent {
			return true
		}
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s case label %s: %v", tagIdent, lit.Value, err)
				}
				labels = append(labels, v)
			}
		}
		return false
	})
	return labels
}

func findFunc(t *testing.T, filename, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("%s not found in %s", name, filename)
	return nil
}

// dispatchedOutputFormats reads the formats ExecuteAndFormat actually renders:
// the case labels of its `switch format` statement. It is read from the source
// so the test tracks the real dispatch, not a second hand-maintained list.
func dispatchedOutputFormats(t *testing.T) []string {
	t.Helper()
	labels := stringLiteralLabels(t, findFunc(t, "execute.go", "ExecuteAndFormat"), "format")
	if len(labels) == 0 {
		t.Fatal("ExecuteAndFormat has no switch on format: cannot verify output-format handling")
	}
	return labels
}

// detectedOutputFormats reads the formats DetectFormat can select from an output
// file extension (formats.go).
func detectedOutputFormats(t *testing.T) []string {
	t.Helper()
	fn := findFunc(t, "formats.go", "DetectFormat")
	var formats []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, body := range clause.Body {
				ret, ok := body.(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				lit, ok := ret.Results[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote DetectFormat result %s: %v", lit.Value, err)
				}
				// "" is the "unknown extension" sentinel, not a format.
				if v != "" {
					formats = append(formats, v)
				}
			}
		}
		return false
	})
	if len(formats) == 0 {
		t.Fatal("DetectFormat returns no format literals: cannot verify --output-file detection")
	}
	return formats
}

// TestOutputFormatDrift guards the advertised output-format surface against the
// implemented one. app.SupportedOutputFormats is rendered verbatim into every
// generated command's --output help; it must name exactly the formats the CLI
// can render, or the help misleads users about what the tool accepts (DF-051).
func TestOutputFormatDrift(t *testing.T) {
	advertised := app.SupportedOutputFormats
	handled := dispatchedOutputFormats(t)
	detected := detectedOutputFormats(t)

	union := append([]string(nil), advertised...)
	for _, format := range handled {
		if !slices.Contains(union, format) {
			union = append(union, format)
		}
	}
	slices.Sort(union)

	for _, format := range union {
		t.Run(format, func(t *testing.T) {
			inAdvertised := slices.Contains(advertised, format)
			inHandled := slices.Contains(handled, format)
			if inAdvertised && !inHandled {
				t.Errorf("format %q is advertised in app.SupportedOutputFormats (rendered into --output help) but internal/cli.ExecuteAndFormat does not handle it", format)
			}
			if inHandled && !inAdvertised {
				t.Errorf("format %q is handled by internal/cli.ExecuteAndFormat but missing from app.SupportedOutputFormats, so the generated --output help never mentions it", format)
			}
		})
	}

	// --output-file extension detection may only select advertised formats: a
	// detected format the help does not list would be reachable but undiscoverable.
	for _, format := range detected {
		if !slices.Contains(advertised, format) {
			t.Errorf("format %q is auto-detected from an output file extension (internal/cli.DetectFormat) but is not advertised in app.SupportedOutputFormats", format)
		}
	}

	// The declared default must itself be renderable.
	if !slices.Contains(handled, app.DefaultOutputFormat) {
		t.Errorf("default output format %q is not handled by internal/cli.ExecuteAndFormat", app.DefaultOutputFormat)
	}
}
