// Package mcp_server serves Husonym to an agent over the Model Context Protocol.
//
// It answers for one account, with the credentials of whoever started it. It reaches the API
// through the readers under this tree only, to read as to write, never through a Connect client
// of its own: see
// imports_test.go for the rule, and maskedconn, novalues, rowvalues and jobs for why.
package mcp_server

import (
	"context"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/rowvalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options is what the server needs to answer for one account.
type Options struct {
	Connections *maskedconn.Reader
	Data        *novalues.Reader
	Values      *rowvalues.Reader
	Jobs        *jobs.Reader
	AccountId   string
	Version     string
	Logger      *slog.Logger
}

// progressInterval is how often a client that asked is told a call is still worked on. A
// client may give up on a call it does not hear from — after a minute, for some — and the
// longest calls here take up to three: it is told well within that.
const progressInterval = 15 * time.Second

// New returns a server with every tool registered, ready to run on a transport.
func New(opts Options) *mcp.Server {
	return newServer(opts, progressInterval)
}

// newServer is New, telling of a call still worked on as often as said.
func newServer(opts Options, progressEvery time.Duration) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "husonym", Title: "Husonym", Version: opts.Version},
		&mcp.ServerOptions{Logger: opts.Logger},
	)
	server.AddReceivingMiddleware(tellsProgress(progressEvery))
	addListConnections(server, opts.Connections, opts.AccountId)
	addDescribeConnection(server, opts.Connections)
	addCheckConnection(server, opts.Connections)
	addIntrospectSchema(server, opts.Data)
	addSuggestMappings(server, opts.Data)
	addPreviewColumn(server, opts.Data, opts.Values)
	addCreateJob(server, opts.Connections, opts.Data, opts.Jobs)
	addUpdateJobMappings(server, opts.Data, opts.Jobs)
	addPreflightJob(server, opts.Jobs)
	addRunJob(server, opts.Jobs)
	addGetRunStatus(server, opts.Jobs)
	addGetRunFailure(server, opts.Values)
	return server
}

// tellsProgress tells the client of a tool call, at each interval, that the call is still
// worked on — when the client asked to be told, by sending a progress token with the call.
// The protocol lets a client wait longer for a call it hears from. The client is told nothing
// once the call has answered.
func tellsProgress(every time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil || call.Params.GetProgressToken() == nil || call.Session == nil {
				return next(ctx, method, req)
			}
			token := call.Params.GetProgressToken()

			answered, told := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(told)
				ticker := time.NewTicker(every)
				defer ticker.Stop()
				started := time.Now()
				for {
					select {
					case <-answered:
						return
					case <-ctx.Done():
						return
					case <-ticker.C:
						// A client that cannot be told is one the call will fail to answer too.
						_ = call.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
							ProgressToken: token,
							Progress:      time.Since(started).Seconds(),
							Message:       "still working on " + call.Params.Name,
						})
					}
				}
			}()
			// The last word on the call is its answer: no progress is told after it.
			defer func() {
				close(answered)
				<-told
			}()
			return next(ctx, method, req)
		}
	}
}
