package mcp_cmd

import (
	"context"
	"fmt"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/auth"
	cli_logger "github.com/fishtre-compagnie/husonym/cli/internal/logger"
	mcp_server "github.com/fishtre-compagnie/husonym/cli/internal/mcp"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/rowvalues"
	"github.com/fishtre-compagnie/husonym/cli/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve Husonym to an agent over the Model Context Protocol, on stdio",
		Long: fmt.Sprintf(
			"Serve Husonym to an agent over the Model Context Protocol, on stdin and stdout.\n\n"+
				"Meant to be started by an MCP client, which passes the API key in $%s: over stdio, "+
				"the MCP specification has a server take its credentials from the environment. "+
				"The key's account is the one the server answers for.",
			auth.ApiKeyEnvVarName,
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := cmd.Flags().GetString("api-key")
			if err != nil {
				return err
			}
			debugMode, err := cmd.Flags().GetBool("debug")
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return serve(cmd.Context(), apiKey, debugMode)
		},
	}
}

func serve(ctx context.Context, apiKey string, debugMode bool) error {
	// The logger writes to stderr, which is just as well: stdout carries the protocol.
	logger := cli_logger.NewSLogger(cli_logger.GetCharmLevelOrDefault(debugMode))

	// Without a key the client would fall back on the token of a past "husonym login", which is
	// a person's session, not a credential handed to an agent. The refusal is decided where the
	// client asks whether the API requires authentication, once: asking twice would let an API
	// answer differently the second time and get the session after all.
	httpclient, err := auth.GetHusonymHttpClient(ctx, logger, auth.WithApiKey(&apiKey), auth.WithApiKeyOnly())
	if err != nil {
		return err
	}
	husonymurl := auth.GetHusonymUrl()

	userclient := mgmtv1alpha1connect.NewUserAccountServiceClient(httpclient, husonymurl)
	accountId, err := auth.ResolveAccountIdFromFlag(ctx, userclient, nil, &apiKey, logger)
	if err != nil {
		return err
	}

	connections := maskedconn.New(httpclient, husonymurl)
	jobReader := jobs.New(httpclient, husonymurl, accountId, connections)
	server := mcp_server.New(mcp_server.Options{
		Connections: connections,
		Data:        novalues.New(httpclient, husonymurl, accountId),
		Values:      rowvalues.New(httpclient, husonymurl, accountId, connections, jobReader),
		Jobs:        jobReader,
		AccountId:   accountId,
		Version:     version.Get().GitVersion,
		Logger:      logger,
	})
	return server.Run(ctx, &mcp.StdioTransport{})
}
