// Package novalues reads what a connection's data looks like, never the data itself.
//
// The connection data service also streams rows, samples a column's values and previews a
// transformer on real values: all of those put production data in front of the model, and
// through it in front of whoever serves the model. This package holds that client behind an
// interface that leaves them out, so the rest of the MCP surface cannot reach them.
//
// What comes back is structure — names, types, constraints — and PII verdicts, which the API
// builds from samples it reads itself but reports as counts and labels, not values.
package novalues

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// dataClient is the part of the connection data service this package may call: nothing in it
// answers with a value read from a row. reader_test.go pins the list, so that widening it is a
// decision someone takes, not an accident.
type dataClient interface {
	GetAllSchemasAndTables(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetAllSchemasAndTablesRequest],
	) (*connect.Response[mgmtv1alpha1.GetAllSchemasAndTablesResponse], error)
	GetConnectionSchema(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetConnectionSchemaRequest],
	) (*connect.Response[mgmtv1alpha1.GetConnectionSchemaResponse], error)
	GetConnectionTableConstraints(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetConnectionTableConstraintsRequest],
	) (*connect.Response[mgmtv1alpha1.GetConnectionTableConstraintsResponse], error)
	DetectPiiInConnectionData(
		context.Context,
		*connect.Request[mgmtv1alpha1.DetectPiiInConnectionDataRequest],
	) (*connect.Response[mgmtv1alpha1.DetectPiiInConnectionDataResponse], error)
}

// Reader reads the structure of a connection's data, and the PII detected in it.
type Reader struct {
	client dataClient
}

// New builds a Reader on its own connection data client, which is never handed out.
func New(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *Reader {
	return &Reader{client: mgmtv1alpha1connect.NewConnectionDataServiceClient(httpClient, baseURL, opts...)}
}

// Tables returns every table of a connection.
func (r *Reader) Tables(
	ctx context.Context,
	connectionId string,
) ([]*mgmtv1alpha1.GetAllSchemasAndTablesResponse_Table, error) {
	res, err := r.client.GetAllSchemasAndTables(ctx, connect.NewRequest(&mgmtv1alpha1.GetAllSchemasAndTablesRequest{
		ConnectionId: connectionId,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetTables(), nil
}

// Columns returns every column of a connection, each with the PII the API infers from its
// name and type.
func (r *Reader) Columns(ctx context.Context, connectionId string) ([]*mgmtv1alpha1.DatabaseColumn, error) {
	res, err := r.client.GetConnectionSchema(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionSchemaRequest{
		ConnectionId: connectionId,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetSchemas(), nil
}

// Constraints returns the keys of every table of a connection.
func (r *Reader) Constraints(
	ctx context.Context,
	connectionId string,
) (*mgmtv1alpha1.GetConnectionTableConstraintsResponse, error) {
	res, err := r.client.GetConnectionTableConstraints(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionTableConstraintsRequest{ConnectionId: connectionId}),
	)
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// ScanPii has the API scan a sample of one table for PII, and returns its verdict on each
// column: the name and the content, reconciled by the API.
func (r *Reader) ScanPii(
	ctx context.Context,
	connectionId, schema, table string,
) ([]*mgmtv1alpha1.ColumnPiiVerdict, error) {
	res, err := r.client.DetectPiiInConnectionData(ctx, connect.NewRequest(&mgmtv1alpha1.DetectPiiInConnectionDataRequest{
		ConnectionId: connectionId,
		Schema:       schema,
		Table:        table,
	}))
	if err != nil {
		// Only the code is passed on: a sampling error can quote the value it choked on.
		return nil, fmt.Errorf("the API could not scan %s.%s: %s", schema, table, connect.CodeOf(err))
	}
	return res.Msg.GetVerdicts(), nil
}
