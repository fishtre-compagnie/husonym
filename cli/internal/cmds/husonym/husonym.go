package husonym_cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	accounts_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/accounts"
	connections_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/connections"
	jobs_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/jobs"
	login_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/login"
	sync_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/sync"
	version_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/version"
	whoami_cmd "github.com/fishtre-compagnie/husonym/cli/internal/cmds/husonym/whoami"
	"github.com/fishtre-compagnie/husonym/cli/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/metadata"
)

const (
	husonymDirName           = ".husonym"
	cliSettingsFileNameNoExt = "config"
	cliSettingsFileExt       = "yaml"

	apiKeyEnvVarName = "HUSONYM_API_KEY" //nolint:gosec
	apiKeyFlag       = "api-key"
)

func Execute() {
	rootCmd := &cobra.Command{
		Use:   "husonym",
		Short: "Terminal UI that interfaces with the Husonym system.",
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SilenceErrors = true
			cmd.SetContext(metadata.NewOutgoingContext(cmd.Context(), version.Get().GrpcMetadata()))
		},
	}

	var cfgFilePath string
	cobra.OnInitialize(
		func() { migrateOldConfig(cfgFilePath) },
		func() { initConfig(cfgFilePath) },
		func() {
			apiKey, err := rootCmd.Flags().GetString(apiKeyFlag)
			if err != nil {
				panic(err)
			}
			envApiKey := viper.GetString(apiKeyEnvVarName)
			if apiKey == "" && envApiKey != "" {
				err = rootCmd.Flags().Set(apiKeyFlag, envApiKey)
				if err != nil {
					panic(err)
				}
			}
		},
	)

	rootCmd.Version = version.Get().GitVersion
	rootCmd.SetVersionTemplate(`{{printf "%s\n" .Version}}`)

	rootCmd.PersistentFlags().StringVar(
		&cfgFilePath, "config", "", fmt.Sprintf("config file (default is $HOME/%s/%s.%s)", husonymDirName, cliSettingsFileNameNoExt, cliSettingsFileExt),
	)
	rootCmd.PersistentFlags().
		String(apiKeyFlag, "", fmt.Sprintf("Husonym API Key. Takes precedence over $%s", apiKeyEnvVarName))

	rootCmd.PersistentFlags().Bool("debug", false, "Run in debug mode")

	rootCmd.AddCommand(jobs_cmd.NewCmd())
	rootCmd.AddCommand(version_cmd.NewCmd())
	rootCmd.AddCommand(whoami_cmd.NewCmd())
	rootCmd.AddCommand(login_cmd.NewCmd())
	rootCmd.AddCommand(sync_cmd.NewCmd())
	rootCmd.AddCommand(accounts_cmd.NewCmd())
	rootCmd.AddCommand(connections_cmd.NewCmd())

	cobra.CheckErr(rootCmd.Execute())
}

// Hack: This method attempts to migrate the old husonym-cli file to the new default location
func migrateOldConfig(cfgFilePath string) {
	if cfgFilePath != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	oldPath := filepath.Join(home, ".husonym-cli.yaml")
	_, err = os.Stat(oldPath)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	err = os.Mkdir(filepath.Join(home, husonymDirName), 0755)
	if err != nil {
		return
	}
	err = os.Rename(
		oldPath,
		filepath.Join(
			home,
			husonymDirName,
			fmt.Sprintf("%s.%s", cliSettingsFileNameNoExt, cliSettingsFileExt),
		),
	)
	if err != nil {
		return
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig(cfgFilePath string) {
	if cfgFilePath != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFilePath)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}

		fullHusonymSettingsDir := filepath.Join(home, husonymDirName)
		husonymConfigDir := os.Getenv("HUSONYM_CONFIG_DIR") // helpful for tools such as direnv and people who want it somewhere interesting
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")       // linux users expect this to be respected

		viper.AddConfigPath(".")
		viper.AddConfigPath(fullHusonymSettingsDir)
		viper.AddConfigPath(home)
		if husonymConfigDir != "" {
			viper.AddConfigPath(husonymConfigDir)
		}
		if xdgConfigHome != "" {
			viper.AddConfigPath(xdgConfigHome)
		}

		viper.SetConfigType(cliSettingsFileExt)
		viper.SetConfigName(cliSettingsFileNameNoExt)
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	err := viper.ReadInConfig()
	if err != nil {
		if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
			return
		}
	}
}
