// Package mcp_server serves Husonym to an agent over the Model Context Protocol.
//
// It answers for one account, with the credentials of whoever started it. What it reads, it
// reads through the readers under this tree, never through a Connect client of its own: see
// imports_test.go for the rule and maskedconn for why.
package mcp_server

import (
	"log/slog"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options is what the server needs to answer for one account.
type Options struct {
	Connections *maskedconn.Reader
	AccountId   string
	Version     string
	Logger      *slog.Logger
}

// New returns a server with every tool registered, ready to run on a transport.
func New(opts Options) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "husonym", Title: "Husonym", Version: opts.Version},
		&mcp.ServerOptions{Logger: opts.Logger},
	)
	addListConnections(server, opts.Connections, opts.AccountId)
	return server
}
