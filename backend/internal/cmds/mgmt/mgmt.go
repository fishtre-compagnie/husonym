package mgmt_cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	migrate_cmd "github.com/fishtre-compagnie/husonym/backend/internal/cmds/mgmt/migrate"
	run_cmd "github.com/fishtre-compagnie/husonym/backend/internal/cmds/mgmt/run"
	serve "github.com/fishtre-compagnie/husonym/backend/internal/cmds/mgmt/serve"
	husonymlogger "github.com/fishtre-compagnie/husonym/backend/pkg/logger"
)

func Execute() {
	cobra.OnInitialize(func() { initConfig() })

	rootCmd := &cobra.Command{
		Use:   "mgmt",
		Short: "Terminal app that is used to manage the Husonym API system.",
		Long:  "",
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SilenceErrors = true
		},
	}

	rootCmd.SetVersionTemplate(`{{printf "%s\n" .Version}}`)

	// Wire up subcommands here
	rootCmd.AddCommand(serve.NewCmd())
	rootCmd.AddCommand(migrate_cmd.NewCmd())
	rootCmd.AddCommand(run_cmd.NewCmd())

	logger, _ := husonymlogger.NewLoggers()

	err := rootCmd.Execute()
	if err != nil {
		logger.Error(fmt.Sprintf("error executing root command: %v", err))
		os.Exit(1)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig(
// logger logr.Logger,
) {
	viper.AddConfigPath(".")
	viper.SetConfigType("dotenv")

	viper.AutomaticEnv() // read in environment variables that match

	viper.SetConfigName(".env")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		} else {
			panic(err)
		}
	}
	envType := viper.GetString("HUSONYM_ENV")
	if envType != "" {
		viper.SetConfigName(fmt.Sprintf(".env.%s", envType))
		if err := viper.MergeInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			} else {
				panic(err)
			}
		}

		viper.SetConfigName(fmt.Sprintf(".env.%s.secrets", envType))
		if err := viper.MergeInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			} else {
				panic(err)
			}
		}
	}
}
