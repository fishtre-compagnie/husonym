// Command husonym-controlplane is the control plane: it receives the usage reports of the
// product instances.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/controlplane/registryimport"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const databaseURLEnv = "CONTROLPLANE_DATABASE_URL"

func main() {
	if err := newRootCmd(license.EmbeddedKeyring).Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd builds the command; keyring gives the public keys the registry is verified against.
func newRootCmd(keyring func() (license.Keyring, error)) *cobra.Command {
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
	root.AddCommand(migrate, newImportRegistryCmd(keyring), newServeCmd())
	return root
}

func newImportRegistryCmd(keyring func() (license.Keyring, error)) *cobra.Command {
	var registryPath string
	cmd := &cobra.Command{
		Use:   "import-registry",
		Short: "Load the registry of issued licenses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// LoadRegistry reads a missing file as an empty registry, which suits the issuer
			// but here would hide a mistyped path behind three zeros.
			if _, err := os.Stat(registryPath); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("registry file %s does not exist", registryPath)
			}
			databaseURL := os.Getenv(databaseURLEnv)
			if databaseURL == "" {
				return fmt.Errorf("%s is not set", databaseURLEnv)
			}
			registry, err := license.LoadRegistry(registryPath)
			if err != nil {
				return err
			}
			ring, err := keyring()
			if err != nil {
				return err
			}
			pool, err := pgxpool.New(cmd.Context(), databaseURL)
			if err != nil {
				return errors.New("unable to open the database")
			}
			defer pool.Close()

			store := cpstore.New(pool)
			result, err := registryimport.Run(cmd.Context(), store, intake.New(store, time.Now), registry, ring)
			// The import stands even when the promotion after it failed: the counts are told.
			// Only counts: the registry holds customer names and license keys.
			fmt.Fprintf(cmd.OutOrStdout(), "added: %d\nalready there: %d\nrefused: %d\n",
				result.Added, result.AlreadyThere, result.Refused)
			if err != nil {
				return err
			}
			if result.Refused > 0 {
				return errors.New("some entries were refused")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryPath, "registry", "", "path of the license registry file")
	_ = cmd.MarkFlagRequired("registry")
	return cmd
}
