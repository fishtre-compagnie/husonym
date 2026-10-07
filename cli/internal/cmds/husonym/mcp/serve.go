package mcp_cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
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

	limits := apiTimeLimits()
	httpclient, accountId, err := signIn(ctx, apiKey, logger, limits.byDefault)
	if err != nil {
		return err
	}

	options := readers(httpclient, auth.GetHusonymUrl(), accountId, connect.WithInterceptors(limits.interceptor()))
	// The license is read with the client the readers use: same key, same limits.
	options.Allowed = newLicenseGate(mgmtv1alpha1connect.NewUserAccountServiceClient(
		httpclient, auth.GetHusonymUrl(), connect.WithInterceptors(limits.interceptor()),
	)).Allowed
	options.Version = version.Get().GitVersion
	options.Logger = logger
	return mcp_server.New(&options).Run(ctx, &mcp.StdioTransport{})
}

// signIn returns the client the server reaches the API with, and the account it answers for.
// The API is asked both within the limit: one that never answers must not hold the server
// before it has said a word to its client.
func signIn(ctx context.Context, apiKey string, logger *slog.Logger, limit time.Duration) (*http.Client, string, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	// Without a key the client would fall back on the token of a past "husonym login", which is
	// a person's session, not a credential handed to an agent. The refusal is decided where the
	// client asks whether the API requires authentication, once: asking twice would let an API
	// answer differently the second time and get the session after all.
	httpclient, err := auth.GetHusonymHttpClient(ctx, logger, auth.WithApiKey(&apiKey), auth.WithApiKeyOnly())
	if err != nil {
		return nil, "", err
	}
	userclient := mgmtv1alpha1connect.NewUserAccountServiceClient(httpclient, auth.GetHusonymUrl())
	accountId, err := auth.ResolveAccountIdFromFlag(ctx, userclient, nil, &apiKey, logger)
	if err != nil {
		return nil, "", err
	}
	return httpclient, accountId, nil
}

// readers builds the readers the server reaches the API through, for one account. Each makes
// its calls with the options given: the time limits of the calls among them.
func readers(httpclient connect.HTTPClient, url, accountId string, opts ...connect.ClientOption) mcp_server.Options {
	connections := maskedconn.New(httpclient, url, opts...)
	jobReader := jobs.New(httpclient, url, accountId, connections, opts...)
	return mcp_server.Options{
		Connections: connections,
		Data:        novalues.New(httpclient, url, accountId, opts...),
		Values:      rowvalues.New(httpclient, url, accountId, connections, jobReader, opts...),
		Jobs:        jobReader,
		AccountId:   accountId,
	}
}
