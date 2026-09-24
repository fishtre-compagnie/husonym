package mcp_server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/rowvalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	accountId     = "7f2c1e4a-0000-4000-8000-000000000001"
	connectionId  = "7f2c1e4a-0000-4000-8000-0000000000c1"
	clearPassword = "hunter2-in-clear"
	// aliasId is a second connection to the database of connectionId, under another name.
	aliasId = "7f2c1e4a-0000-4000-8000-0000000000c3"
)

var (
	createdAt = timestamppb.New(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
	updatedAt = timestamppb.New(time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC))
)

// fakeConnectionService answers like the API does: the password comes back in clear unless
// the request asks to leave the secrets out.
type fakeConnectionService struct {
	mgmtv1alpha1connect.UnimplementedConnectionServiceHandler

	err error

	mu           sync.Mutex
	listRequests []*mgmtv1alpha1.GetConnectionsRequest
	getRequests  []*mgmtv1alpha1.GetConnectionRequest
}

func (f *fakeConnectionService) GetConnections(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetConnectionsRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionsResponse], error) {
	f.mu.Lock()
	f.listRequests = append(f.listRequests, req.Msg)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionsResponse{
		Connections: []*mgmtv1alpha1.Connection{
			postgresConnection(connectionId, "production", req.Msg.GetExcludeSensitive()),
			{
				Id:   "a",
				Name: "local",
				ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
					Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{}},
				},
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
		},
	}), nil
}

func (f *fakeConnectionService) GetConnection(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetConnectionRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error) {
	f.mu.Lock()
	f.getRequests = append(f.getRequests, req.Msg)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	name := "staging"
	if req.Msg.GetId() == connectionId {
		name = "production"
	}
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
		Connection: postgresConnection(req.Msg.GetId(), name, req.Msg.GetExcludeSensitive()),
	}), nil
}

func postgresConnection(id, name string, excludeSensitive bool) *mgmtv1alpha1.Connection {
	host := "db.internal"
	if id != connectionId && id != aliasId {
		host = name + ".internal"
	}
	pass := clearPassword
	if excludeSensitive {
		pass = "********"
	}
	return &mgmtv1alpha1.Connection{
		Id:   id,
		Name: name,
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
				PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Connection{
						Connection: &mgmtv1alpha1.PostgresConnection{
							Host: host, Port: 5432, Name: "shop", User: "husonym", Pass: pass,
						},
					},
				},
			},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

// fakeDataService holds a small shop: users, the orders that point at them, and a table whose
// content scan always fails.
type fakeDataService struct {
	mgmtv1alpha1connect.UnimplementedConnectionDataServiceHandler

	mu          sync.Mutex
	scanned     []string
	previews    []*mgmtv1alpha1.PreviewColumnTransformerRequest
	scanFailure map[string]error
}

// fakeTransformersService knows a few system transformers, with their default configuration.
type fakeTransformersService struct {
	mgmtv1alpha1connect.UnimplementedTransformersServiceHandler
}

func (fakeTransformersService) GetSystemTransformerBySource(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetSystemTransformerBySourceRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemTransformerBySourceResponse], error) {
	preserveDomain, preserveLength := true, false
	var config *mgmtv1alpha1.TransformerConfig
	switch req.Msg.GetSource() {
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL:
		config = &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
			GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
		}}
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_EMAIL:
		config = &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{
			TransformEmailConfig: &mgmtv1alpha1.TransformEmail{PreserveDomain: &preserveDomain, PreserveLength: &preserveLength},
		}}
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT:
		config = &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
			TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{},
		}}
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH:
		config = &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
			PassthroughConfig: &mgmtv1alpha1.Passthrough{},
		}}
	default:
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown transformer"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemTransformerBySourceResponse{
		Transformer: &mgmtv1alpha1.SystemTransformer{Source: req.Msg.GetSource(), Config: config},
	}), nil
}

func (f *fakeDataService) GetAllSchemasAndTables(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetAllSchemasAndTablesRequest],
) (*connect.Response[mgmtv1alpha1.GetAllSchemasAndTablesResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetAllSchemasAndTablesResponse{
		Tables: []*mgmtv1alpha1.GetAllSchemasAndTablesResponse_Table{
			{SchemaName: "public", TableName: "users"},
			{SchemaName: "public", TableName: "locked"},
			{SchemaName: "public", TableName: "orders"},
		},
	}), nil
}

func (f *fakeDataService) GetConnectionSchema(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetConnectionSchemaRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionSchemaResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionSchemaResponse{Schemas: shopColumns()}), nil
}

// shopColumns are the columns of the shop, each with what its name says, as the API gives them.
func shopColumns() []*mgmtv1alpha1.DatabaseColumn {
	length := int32(255)
	byName := func(column, category string, source mgmtv1alpha1.TransformerSource) func(*mgmtv1alpha1.DatabaseColumn) {
		return func(c *mgmtv1alpha1.DatabaseColumn) {
			c.IsSensitive = true
			c.DataCategory = category
			c.SuggestedTransformerSource = source
			c.PiiConfidence = mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED
			c.PiiDetectionMethod = mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME
			c.PiiEvidence = "reconnu par le nom de colonne « " + column + " »"
		}
	}
	column := func(table, name, dataType, nullable string, opts ...func(*mgmtv1alpha1.DatabaseColumn)) *mgmtv1alpha1.DatabaseColumn {
		c := &mgmtv1alpha1.DatabaseColumn{Schema: "public", Table: table, Column: name, DataType: dataType, IsNullable: nullable}
		for _, opt := range opts {
			opt(c)
		}
		return c
	}
	return []*mgmtv1alpha1.DatabaseColumn{
		column("users", "id", "uuid", "NO"),
		column("users", "email", "character varying", "NO",
			byName("email", "email", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL),
			func(c *mgmtv1alpha1.DatabaseColumn) { c.CharacterMaximumLength = &length }),
		column("users", "birth_date", "text", "YES",
			byName("birth_date", "birth_date", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT)),
		column("orders", "id", "uuid", "NO"),
		column("orders", "user_id", "uuid", "NO"),
		column("orders", "note", "text", "YES"),
		column("locked", "id", "integer", "NO"),
	}
}

func (f *fakeDataService) GetConnectionTableConstraints(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetConnectionTableConstraintsRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionTableConstraintsResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionTableConstraintsResponse{
		PrimaryKeyConstraints: map[string]*mgmtv1alpha1.PrimaryConstraint{
			"public.users":  {Columns: []string{"id"}},
			"public.orders": {Columns: []string{"id"}},
		},
		ForeignKeyConstraints: map[string]*mgmtv1alpha1.ForeignConstraintTables{
			"public.orders": {Constraints: []*mgmtv1alpha1.ForeignConstraint{{
				Columns:     []string{"user_id"},
				NotNullable: []bool{true},
				ForeignKey:  &mgmtv1alpha1.ForeignKey{Table: "public.users", Columns: []string{"id"}},
			}}},
		},
		UniqueConstraints: map[string]*mgmtv1alpha1.UniqueConstraints{
			"public.users": {Constraints: []*mgmtv1alpha1.UniqueConstraint{{Columns: []string{"email"}}}},
		},
		// The same set as a unique index too, which the tool must not report twice.
		UniqueIndexes: map[string]*mgmtv1alpha1.UniqueIndexes{
			"public.users": {Indexes: []*mgmtv1alpha1.UniqueIndex{{Columns: []string{"email"}}}},
		},
	}), nil
}

func (f *fakeDataService) DetectPiiInConnectionData(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.DetectPiiInConnectionDataRequest],
) (*connect.Response[mgmtv1alpha1.DetectPiiInConnectionDataResponse], error) {
	table := tableKey(req.Msg.GetSchema(), req.Msg.GetTable())
	f.mu.Lock()
	f.scanned = append(f.scanned, table)
	f.mu.Unlock()
	if err := f.scanFailure[table]; err != nil {
		return nil, err
	}
	var detections []*mgmtv1alpha1.ColumnPiiDetection
	switch table {
	case "public.users":
		detections = []*mgmtv1alpha1.ColumnPiiDetection{{
			Schema: "public", Table: "users", Column: "birth_date",
			IsSensitive:        true,
			DataCategory:       "date",
			PiiConfidence:      mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
			PiiDetectionMethod: mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT,
			PiiEvidence:        "format ambigu sur 20 valeurs : jj/mm/aaaa ou mm/jj/aaaa",
		}}
	case "public.orders":
		detections = []*mgmtv1alpha1.ColumnPiiDetection{{
			Schema: "public", Table: "orders", Column: "note",
			IsSensitive:                true,
			DataCategory:               "person_name",
			SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT,
			PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
			PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
			PiiEvidence:                "PERSON reconnu par analyse de contenu sur 6/20 valeurs (score moyen 0.85)",
		}}
	}
	// The verdicts, as the API reconciles them: with the API's own rule, over every column.
	byColumn := map[string]*mgmtv1alpha1.ColumnPiiDetection{}
	for _, detection := range detections {
		byColumn[detection.GetColumn()] = detection
	}
	var verdicts []*mgmtv1alpha1.ColumnPiiVerdict
	for _, column := range shopColumns() {
		if tableKey(column.GetSchema(), column.GetTable()) == table {
			verdicts = append(verdicts, piidetect.Reconcile(column, byColumn[column.GetColumn()]))
		}
	}
	return connect.NewResponse(&mgmtv1alpha1.DetectPiiInConnectionDataResponse{
		Detections: detections,
		Verdicts:   verdicts,
	}), nil
}

func (f *fakeDataService) PreviewColumnTransformer(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.PreviewColumnTransformerRequest],
) (*connect.Response[mgmtv1alpha1.PreviewColumnTransformerResponse], error) {
	f.mu.Lock()
	f.previews = append(f.previews, req.Msg)
	f.mu.Unlock()
	return connect.NewResponse(&mgmtv1alpha1.PreviewColumnTransformerResponse{
		Values: []*mgmtv1alpha1.ColumnTransformerPreview{
			{
				Input:  &mgmtv1alpha1.ColumnSampleValue{Value: "jean.dupont@example.com"},
				Output: &mgmtv1alpha1.ColumnSampleValue{Value: "kx81@anon.test"},
			},
			{Input: &mgmtv1alpha1.ColumnSampleValue{IsNull: true}, Output: &mgmtv1alpha1.ColumnSampleValue{IsNull: true}},
		},
		DistinctInputs:  1,
		DistinctOutputs: 1,
	}), nil
}

func (f *fakeDataService) previewRequests() []*mgmtv1alpha1.PreviewColumnTransformerRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.previews)
}

func (f *fakeDataService) scannedTables() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.scanned)
}

// connectClient stands the fake API up, serves the MCP server on it, and returns a client
// session talking to that server.
func connectClient(
	t *testing.T,
	connections *fakeConnectionService,
	data *fakeDataService,
) *mcp.ClientSession {
	t.Helper()
	return connectClientWith(t, connections, data, nil, "")
}

// connectClientWith is connectClient with a client of the test's making, such as one that
// answers the questions the server puts to the person, speaking a protocol revision of its
// choosing (the latest when empty).
func connectClientWith(
	t *testing.T,
	connections *fakeConnectionService,
	data *fakeDataService,
	clientOptions *mcp.ClientOptions,
	protocolVersion string,
) *mcp.ClientSession {
	t.Helper()
	return connectAPI(t, fakeAPI{connections: connections, data: data, jobs: &fakeJobService{}}, clientOptions, protocolVersion)
}

// fakeAPI is the part of the API the MCP server calls.
type fakeAPI struct {
	connections *fakeConnectionService
	data        *fakeDataService
	jobs        *fakeJobService
}

// connectAPI is connectClientWith on a whole fake API.
func connectAPI(t *testing.T, fakes fakeAPI, clientOptions *mcp.ClientOptions, protocolVersion string) *mcp.ClientSession {
	t.Helper()
	ctx := t.Context()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewConnectionServiceHandler(fakes.connections))
	mux.Handle(mgmtv1alpha1connect.NewConnectionDataServiceHandler(fakes.data))
	mux.Handle(mgmtv1alpha1connect.NewTransformersServiceHandler(fakeTransformersService{}))
	mux.Handle(mgmtv1alpha1connect.NewJobServiceHandler(fakes.jobs))
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	connections := maskedconn.New(api.Client(), api.URL)
	jobReader := jobs.New(api.Client(), api.URL, accountId, connections)
	server := New(Options{
		Connections: connections,
		Data:        novalues.New(api.Client(), api.URL, accountId),
		Values:      rowvalues.New(api.Client(), api.URL, accountId, connections, jobReader),
		Jobs:        jobReader,
		AccountId:   accountId,
		Version:     "test",
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, clientOptions)
	session, err := client.Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool calls a tool that must succeed, and returns its structured result.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.False(t, res.IsError, "%s failed: %v", name, res.Content)
	return res
}

// callToolError calls a tool that must fail, and returns the message the model reads.
func callToolError(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "a refusal is a tool error for the model to read, not a protocol error")
	require.True(t, res.IsError)
	require.Len(t, res.Content, 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func Test_Catalogue(t *testing.T) {
	t.Parallel()
	session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var reading, writing []string
	for _, tool := range tools.Tools {
		if tool.Annotations.ReadOnlyHint {
			reading = append(reading, tool.Name)
		} else {
			writing = append(writing, tool.Name)
		}
	}
	require.ElementsMatch(t, []string{
		"describe_connection", "get_run_failure", "get_run_status", "introspect_schema", "list_connections",
		"preview_column", "suggest_mappings",
	}, reading)
	// Each of these is held by the scope of the API key (plans/mcp-husonym.md §5.1): the API
	// refuses what the key does not grant, and names the permission missing.
	require.ElementsMatch(t, []string{"create_job", "run_job", "update_job_mappings"}, writing)
}
