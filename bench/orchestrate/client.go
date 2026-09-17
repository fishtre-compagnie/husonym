// Package orchestrate drives the Husonym API the way a user would: it declares the bench
// servers as connections, creates one job per case and engine, runs it and waits for its
// final status. Going through the API and Temporal keeps the comparison honest: both
// engines are measured on the whole path a real job takes.
package orchestrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/bench/env"
)

// Client talks to the Husonym API on behalf of the bench account.
type Client struct {
	conns     mgmtv1alpha1connect.ConnectionServiceClient
	jobs      mgmtv1alpha1connect.JobServiceClient
	accountID string
}

// NewClient opens the personal account of the API, which runs without authentication in
// the development stack.
func NewClient(ctx context.Context, apiURL string) (*Client, error) {
	users := mgmtv1alpha1connect.NewUserAccountServiceClient(http.DefaultClient, apiURL)
	if _, err := users.SetUser(ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRequest{})); err != nil {
		return nil, fmt.Errorf("orchestrate: set user: %w", err)
	}
	account, err := users.SetPersonalAccount(ctx, connect.NewRequest(&mgmtv1alpha1.SetPersonalAccountRequest{}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: set personal account: %w", err)
	}
	return &Client{
		conns:     mgmtv1alpha1connect.NewConnectionServiceClient(http.DefaultClient, apiURL),
		jobs:      mgmtv1alpha1connect.NewJobServiceClient(http.DefaultClient, apiURL),
		accountID: account.Msg.GetAccountId(),
	}, nil
}

// EnsureConnection returns the id of the connection to this server, creating it when the
// account has none. Connection names are unique per account, so a bench run reuses the
// connections of the previous one; the name carries a digest of the server settings, so
// changed settings never reuse a stale connection.
func (c *Client) EnsureConnection(ctx context.Context, role string, server *env.Server) (string, error) {
	digest := sha256.Sum256(fmt.Appendf(nil, "%s:%d/%s/%s/%s",
		server.WorkerHost, server.WorkerPort, server.User, server.Password, server.Database))
	name := "bench-" + role + "-" + hex.EncodeToString(digest[:4])
	existing, err := c.conns.GetConnections(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionsRequest{
		AccountId: c.accountID,
	}))
	if err != nil {
		return "", fmt.Errorf("orchestrate: list connections: %w", err)
	}
	for _, conn := range existing.Msg.GetConnections() {
		if conn.GetName() == name {
			return conn.GetId(), nil
		}
	}
	created, err := c.conns.CreateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
		AccountId: c.accountID,
		Name:      name,
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
				ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Connection{Connection: &mgmtv1alpha1.MysqlConnection{
					User:     server.User,
					Pass:     server.Password,
					Protocol: "tcp",
					Host:     server.WorkerHost,
					Port:     server.WorkerPort,
					Name:     server.Database,
				}},
			}},
		},
	}))
	if err != nil {
		return "", fmt.Errorf("orchestrate: create connection %s: %w", name, err)
	}
	return created.Msg.GetConnection().GetId(), nil
}
