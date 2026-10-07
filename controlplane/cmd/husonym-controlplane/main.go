// Command husonym-controlplane is the control plane: it receives the usage reports of the
// product instances.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/spf13/cobra"
)

const databaseURLEnv = "CONTROLPLANE_DATABASE_URL"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "husonym-controlplane",
		Short:        "The Husonym control plane",
		SilenceUsage: true,
	}
	migrate := &cobra.Command{Use: "migrate", Short: "Manage the database schema"}
	migrate.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply the pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL := os.Getenv(databaseURLEnv)
			if databaseURL == "" {
				return fmt.Errorf("%s is not set", databaseURLEnv)
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
			return migrations.Up(cmd.Context(), databaseURL, logger)
		},
	})
	root.AddCommand(migrate)
	return root
}
