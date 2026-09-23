// Package maskedconn is the only way the MCP server reads connections.
//
// Every request it sends asks the API to leave the secrets out, and the API then masks them
// with the very code that masks them for a user who lacks connection:view_sensitive. The
// list of masked fields is therefore not a second list kept here: a field that escaped it
// would already be showing in the UI, so the MCP cannot fall behind on its own.
//
// Secrets are write-only on the MCP surface: they may be set, never read back. This package
// holds the connection client so that nothing else under cli/internal/mcp has to — the
// import test at the root of that tree refuses any other way in.
package maskedconn

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// Reader reads the connections of an account, with their secrets masked by the API.
type Reader struct {
	client mgmtv1alpha1connect.ConnectionServiceClient
}

// New builds a Reader on its own connection client, which is never handed out.
func New(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *Reader {
	return &Reader{client: mgmtv1alpha1connect.NewConnectionServiceClient(httpClient, baseURL, opts...)}
}

// List returns the connections of an account, secrets masked.
func (r *Reader) List(ctx context.Context, accountId string) ([]*mgmtv1alpha1.Connection, error) {
	res, err := r.client.GetConnections(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionsRequest{
		AccountId:        accountId,
		ExcludeSensitive: true,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetConnections(), nil
}
