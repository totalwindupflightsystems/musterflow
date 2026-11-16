package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/musterflow/internal/auth"
)

// generatedAuthFlag is the flag the engine's generated commands use to select a
// stored credential.
const generatedAuthFlag = "auth"

// bridgeGeneratedAuth makes the generated `--auth` flag resolve credentials
// stored by `musterflow auth add`.
//
// The engine resolves --auth from the system keychain (service muster-cli),
// while MusterFlow stores credentials in config.yaml, so a musterflow-stored
// credential never reached the request (DF-040). Every generated command that
// carries the flag is wrapped: the credential is handed to the engine's own
// resolution path before the request is built, and an unknown name fails with
// MusterFlow's real `auth add` flags instead of the engine's --name/--value
// hint.
func bridgeGeneratedAuth(cmd *cobra.Command) {
	if cmd.RunE != nil && cmd.Flags().Lookup(generatedAuthFlag) != nil {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			name, err := c.Flags().GetString(generatedAuthFlag)
			if err != nil {
				return fmt.Errorf("read --%s: %w", generatedAuthFlag, err)
			}
			if strings.TrimSpace(name) != "" {
				cfg, err := loadConfig()
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				if err := auth.EnsureEngineCredential(cfg, name); err != nil {
					return err
				}
			}
			return inner(c, args)
		}
	}
	for _, sub := range cmd.Commands() {
		bridgeGeneratedAuth(sub)
	}
}
