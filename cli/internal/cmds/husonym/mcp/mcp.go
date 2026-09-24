package mcp_cmd

import (
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Parent command for the Model Context Protocol server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newServeCmd())
	return cmd
}
