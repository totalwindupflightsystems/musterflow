// Package cli provides API subcommand loading and spec-parsing infrastructure.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// apiCommandState tracks lazy generation of cobra commands for a connected API.
// Commands are generated on first access via sync.Once so startup stays fast.
type apiCommandState struct {
	once sync.Once
	err  error
}

var apiCommands = make(map[string]*apiCommandState)
var apiCommandsMu sync.Mutex

func loadAPISubcommands(root *cobra.Command, registry *app.Registry) {
	for _, conn := range registry.List() {
		apiCmd := createAPISubcommand(conn)
		root.AddCommand(apiCmd)
	}
}

// loadSpecData fetches a spec from a URL or reads it from a local file path.
func loadSpecData(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}

// createAPISubcommand creates a cobra command that lazily loads subcommands
// for the given API connection.
func createAPISubcommand(conn *app.APIConnection) *cobra.Command {
	cmd := &cobra.Command{
		Use:                conn.Name,
		Short:              fmt.Sprintf("Commands for %s API", conn.Name),
		Long:               conn.Description,
		DisableFlagParsing: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ensureAPILoaded(cmd, conn)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureAPILoaded(cmd, conn); err != nil {
				return err
			}
			if len(args) == 0 {
				return cmd.Help()
			}
			// Subcommands are now loaded. Find the target subcommand.
			target, remaining, e := cmd.Find(args)
			if e != nil {
				return e
			}
			if target == nil || target == cmd {
				return cmd.Help()
			}
			// Re-execute through cobra's pipeline starting at the target.
			// target is NOT our API command (which has DisableFlagParsing),
			// so this won't recurse — target handles its own flag parsing.
			target.SetArgs(remaining)
			return target.Execute()
		},
	}
	// Override help so lazy-loaded subcommands appear in --help output.
	// cobra resolves the command tree before PersistentPreRunE fires, so we
	// trigger the lazy load from the help func too.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		_ = ensureAPILoaded(c, conn)
		// If args reference a subcommand, find and show its help.
		searchArgs := args
		for len(searchArgs) > 0 && searchArgs[0] == c.Name() {
			searchArgs = searchArgs[1:]
		}
		if len(searchArgs) > 0 {
			target, _, e := c.Find(searchArgs)
			if e == nil && target != nil && target != c {
				fmt.Fprint(target.OutOrStdout(), target.UsageString())
				return
			}
		}
		fmt.Fprint(c.OutOrStdout(), c.UsageString())
	})
	// ValidArgsFunction provides dynamic completion for lazily-generated
	// API subcommands. Cobra's V2 bash completion (and built-in fish/zsh
	// completion) calls the binary with __complete at completion time,
	// which triggers this function to load and enumerate subcommands.
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if err := ensureAPILoaded(cmd, conn); err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, sub := range cmd.Commands() {
			names = append(names, sub.Name())
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

// ensureAPILoaded lazily generates cobra commands for a connected API on first
// access via sync.Once. Safe to call from multiple paths (PersistentPreRunE,
// help func). Subsequent calls are no-ops.
func ensureAPILoaded(cmd *cobra.Command, conn *app.APIConnection) error {
	connID := conn.ID
	apiCommandsMu.Lock()
	state, exists := apiCommands[connID]
	if !exists {
		state = &apiCommandState{}
		apiCommands[connID] = state
	}
	apiCommandsMu.Unlock()

	state.once.Do(func() {
		state.err = loadAPICommands(cmd, conn)
	})
	return state.err
}

func loadAPICommands(parent *cobra.Command, conn *app.APIConnection) error {
	data, err := loadSpecData(conn.SpecURL)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	ctx := context.Background()
	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	// Clear per-operation server URLs so the generator falls back to
	// g.config.BaseURL (which is the fully-resolved base URL from the
	// connection, including scheme and host). Many specs have relative
	// server URLs like "/api/v3" that can't be used directly.
	doc := result.Document
	if doc != nil {
		doc.Servers = nil
		if doc.Paths != nil {
			for _, pathItem := range doc.Paths.Map() {
				if pathItem != nil {
					pathItem.Servers = nil
					clearOperationServers(pathItem.Get)
					clearOperationServers(pathItem.Post)
					clearOperationServers(pathItem.Put)
					clearOperationServers(pathItem.Patch)
					clearOperationServers(pathItem.Delete)
					clearOperationServers(pathItem.Head)
					clearOperationServers(pathItem.Options)
				}
			}
		}
	}

	genCfg := app.GenerateCommandConfig(conn)
	gen, err := generator.NewGenerator(*genCfg)
	if err != nil {
		return fmt.Errorf("create generator: %w", err)
	}

	commands, err := gen.GenerateCommands(doc)
	if err != nil {
		return fmt.Errorf("generate commands: %w", err)
	}

	for _, cmd := range commands {
		parent.AddCommand(cmd)
	}

	fmt.Fprintf(os.Stderr, "  ✓ Generated %d commands for %s\n", len(commands), conn.Name)
	return nil
}

// clearOperationServers clears the per-operation Servers field so the
// generator falls back to the config-level BaseURL.
func clearOperationServers(op *openapi3.Operation) {
	if op != nil {
		op.Servers = nil
	}
}
