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
//
// It also has the API check a connection, which returns no configuration at all, but the
// error of the database driver when the connection fails: that error may quote where and how
// it connects, and is left out here.
package maskedconn

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// connectionClient is the part of the connection service this package may call. Each
// method here that answers with a connection must be sent with exclude_sensitive, and one
// that answers with the error of a database driver must have it left out; reader_test.go pins
// the list, so that widening it is a decision someone takes, not an accident.
type connectionClient interface {
	GetConnections(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetConnectionsRequest],
	) (*connect.Response[mgmtv1alpha1.GetConnectionsResponse], error)
	GetConnection(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetConnectionRequest],
	) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error)
	CheckConnectionConfigById(
		context.Context,
		*connect.Request[mgmtv1alpha1.CheckConnectionConfigByIdRequest],
	) (*connect.Response[mgmtv1alpha1.CheckConnectionConfigByIdResponse], error)
}

// Reader reads the connections of an account, with their secrets masked by the API.
type Reader struct {
	client connectionClient
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

// Get returns one connection, secrets masked.
func (r *Reader) Get(ctx context.Context, connectionId string) (*mgmtv1alpha1.Connection, error) {
	res, err := r.client.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{
		Id:               connectionId,
		ExcludeSensitive: true,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetConnection(), nil
}

// Check is what the API found when it logged in to a connection.
type Check struct {
	// Connected says whether the API could log in and read what the account may do.
	Connected bool
	// Findings are what the connection cannot do that the role asked about needs.
	Findings []*mgmtv1alpha1.ConnectionCheck
}

// Check has the API log in to a connection and, given a scope, say what the connection
// cannot do that its role in a job needs. Why a connection failed is left out: the API
// hands back the error of the database driver, which may quote where and how it connects.
func (r *Reader) Check(ctx context.Context, connectionId string, scope *mgmtv1alpha1.ConnectionCheckScope) (Check, error) {
	res, err := r.client.CheckConnectionConfigById(ctx, connect.NewRequest(&mgmtv1alpha1.CheckConnectionConfigByIdRequest{
		Id:    connectionId,
		Scope: scope,
	}))
	if err != nil {
		return Check{}, checkError(err)
	}
	return Check{Connected: res.Msg.GetIsConnected(), Findings: res.Msg.GetChecks()}, nil
}

// checkError keeps the error of a check where the API writes it itself, and replaces the
// others, which may carry the error of the database driver.
func checkError(err error) error {
	// Cut short here or at the API, a call may carry the error it was cut in.
	if connect.CodeOf(err) == connect.CodeDeadlineExceeded {
		return errors.New("the check of the connection did not end in time")
	}
	if !connect.IsWireError(err) {
		return fmt.Errorf("the API could not be reached (%s)", connect.CodeOf(err))
	}
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument,
		connect.CodeNotFound,
		connect.CodePermissionDenied,
		connect.CodeUnauthenticated:
		return err
	default:
		return fmt.Errorf(
			"the check of the connection could not end (%s): testing the connection in the UI shows why",
			connect.CodeOf(err),
		)
	}
}
