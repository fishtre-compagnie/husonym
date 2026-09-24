package connections_cmd

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/fatih/color"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/auth"
	"github.com/fishtre-compagnie/husonym/cli/internal/connection"
	cli_logger "github.com/fishtre-compagnie/husonym/cli/internal/logger"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := cmd.Flags().GetString("api-key")
			if err != nil {
				return err
			}

			accountId, err := cmd.Flags().GetString("account-id")
			if err != nil {
				return err
			}

			debugMode, err := cmd.Flags().GetBool("debug")
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return listConnections(cmd.Context(), debugMode, &apiKey, &accountId)
		},
	}
	cmd.Flags().
		String("account-id", "", "Account to list connections for. Defaults to account id in cli context")
	return cmd
}

func listConnections(
	ctx context.Context,
	debugMode bool,
	apiKey,
	accountIdFlag *string,
) error {
	logger := cli_logger.NewSLogger(cli_logger.GetCharmLevelOrDefault(debugMode))

	husonymurl := auth.GetHusonymUrl()
	httpclient, err := auth.GetHusonymHttpClient(ctx, logger, auth.WithApiKey(apiKey))
	if err != nil {
		return err
	}

	userclient := mgmtv1alpha1connect.NewUserAccountServiceClient(httpclient, husonymurl)

	accountId, err := auth.ResolveAccountIdFromFlag(ctx, userclient, accountIdFlag, apiKey, logger)
	if err != nil {
		return err
	}

	connectionclient := mgmtv1alpha1connect.NewConnectionServiceClient(httpclient, husonymurl)

	connections, err := getConnections(ctx, connectionclient, accountId)
	if err != nil {
		return err
	}

	fmt.Println() //nolint:forbidigo
	printConnectionsTable(connections)
	fmt.Println() //nolint:forbidigo
	return nil
}

func getConnections(
	ctx context.Context,
	connectionclient mgmtv1alpha1connect.ConnectionServiceClient,
	accountId string,
) ([]*mgmtv1alpha1.Connection, error) {
	res, err := connectionclient.GetConnections(
		ctx,
		connect.NewRequest[mgmtv1alpha1.GetConnectionsRequest](&mgmtv1alpha1.GetConnectionsRequest{
			AccountId: accountId,
		}),
	)
	if err != nil {
		return nil, err
	}
	return res.Msg.GetConnections(), nil
}

func printConnectionsTable(
	connections []*mgmtv1alpha1.Connection,
) {
	tbl := table.
		New("Id", "Name", "Category", "Created At", "Updated At").
		WithHeaderFormatter(
			color.New(color.FgGreen, color.Underline).SprintfFunc(),
		).
		WithFirstColumnFormatter(
			color.New(color.FgYellow).SprintfFunc(),
		)

	for idx := range connections {
		conn := connections[idx]
		tbl.AddRow(
			conn.Id,
			conn.Name,
			connection.Category(conn.GetConnectionConfig()),
			conn.CreatedAt.AsTime().Local().Format(time.RFC3339),
			conn.UpdatedAt.AsTime().Local().Format(time.RFC3339),
		)
	}
	tbl.Print()
}
