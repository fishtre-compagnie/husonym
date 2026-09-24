// Package novalues reads what a connection's data looks like, never the data itself.
//
// The connection data service also streams rows, samples a column's values and previews a
// transformer on real values: all of those put production data in front of the model, and
// through it in front of whoever serves the model. This package holds that client behind an
// interface that leaves them out, so the rest of the MCP surface cannot reach them.
//
// What comes back is structure — names, types, constraints — and PII verdicts, which the API
// builds from samples it reads itself but reports as counts and labels, not values; and the
// default configuration of the system transformers, which is the same for every account.
package novalues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"google.golang.org/protobuf/encoding/protojson"
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

// catalogClient is the part of the transformer service this package may call: the default
// configuration of a system transformer, which carries no value.
type catalogClient interface {
	GetSystemTransformerBySource(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetSystemTransformerBySourceRequest],
	) (*connect.Response[mgmtv1alpha1.GetSystemTransformerBySourceResponse], error)
}

// validationClient is the part of the job service this package may call: checking mappings
// against the structure of their source, which reports names and reasons, never a value.
type validationClient interface {
	ValidateJobMappings(
		context.Context,
		*connect.Request[mgmtv1alpha1.ValidateJobMappingsRequest],
	) (*connect.Response[mgmtv1alpha1.ValidateJobMappingsResponse], error)
}

// Reader reads the structure of a connection's data, the PII detected in it, and the
// transformers that can be applied to it.
type Reader struct {
	client     dataClient
	catalog    catalogClient
	validation validationClient
	accountId  string
}

// New builds a Reader on its own clients, which are never handed out.
func New(httpClient connect.HTTPClient, baseURL, accountId string, opts ...connect.ClientOption) *Reader {
	return &Reader{
		client:     mgmtv1alpha1connect.NewConnectionDataServiceClient(httpClient, baseURL, opts...),
		catalog:    mgmtv1alpha1connect.NewTransformersServiceClient(httpClient, baseURL, opts...),
		validation: mgmtv1alpha1connect.NewJobServiceClient(httpClient, baseURL, opts...),
		accountId:  accountId,
	}
}

// ValidateMappings has the API check mappings against their source, the way it checks them for
// the UI: a column missing, a required one left out, a transformer that does not fit its
// column. The source says the type of job, which decides the transformers a column takes.
func (r *Reader) ValidateMappings(
	ctx context.Context,
	connectionId string,
	source *mgmtv1alpha1.JobSource,
	mappings []*mgmtv1alpha1.JobMapping,
	virtualForeignKeys []*mgmtv1alpha1.VirtualForeignConstraint,
) (*mgmtv1alpha1.ValidateJobMappingsResponse, error) {
	res, err := r.validation.ValidateJobMappings(ctx, connect.NewRequest(&mgmtv1alpha1.ValidateJobMappingsRequest{
		AccountId:          r.accountId,
		ConnectionId:       connectionId,
		JobSource:          source,
		Mappings:           mappings,
		VirtualForeignKeys: virtualForeignKeys,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
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

// ErrRunsCode is returned for a transformer that runs code of its own. Code written by the
// agent would run on the worker with the rows in hand and the network within reach
// (plans/mcp-husonym.md §7): the MCP surface does not hand it a way to.
var ErrRunsCode = errors.New(
	"runs code of its own, and code written by an agent is not run here: " +
		"pick a system transformer that does not",
)

// DefaultTransformer returns the configuration a system transformer starts with in the
// catalogue. It is the one way the MCP surface gets a transformer to apply, and it refuses
// those that run code.
func (r *Reader) DefaultTransformer(
	ctx context.Context,
	source mgmtv1alpha1.TransformerSource,
) (*mgmtv1alpha1.TransformerConfig, error) {
	res, err := r.catalog.GetSystemTransformerBySource(ctx, connect.NewRequest(
		&mgmtv1alpha1.GetSystemTransformerBySourceRequest{Source: source},
	))
	if err != nil {
		return nil, err
	}
	config := res.Msg.GetTransformer().GetConfig()
	if RunsCode(config) {
		return nil, ErrRunsCode
	}
	return config, nil
}

// RunsCode says whether a transformer's configuration carries code of its own to run: a field
// named code, whatever the kind of transformer — a kind added later with code is caught too.
// It errs on the side of yes when the configuration cannot be read.
func RunsCode(config *mgmtv1alpha1.TransformerConfig) bool {
	raw, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(config)
	if err != nil {
		return true
	}
	var byKind map[string]map[string]any
	if err := json.Unmarshal(raw, &byKind); err != nil {
		return true
	}
	for _, fields := range byKind {
		if _, ok := fields["code"]; ok {
			return true
		}
	}
	return false
}
