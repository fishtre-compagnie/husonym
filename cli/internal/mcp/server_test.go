package mcp_server

import (
	"context"
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
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	accountId     = "7f2c1e4a-0000-4000-8000-000000000001"
	connectionId  = "7f2c1e4a-0000-4000-8000-0000000000c1"
	clearPassword = "hunter2-in-clear"
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
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
		Connection: postgresConnection(req.Msg.GetId(), "production", req.Msg.GetExcludeSensitive()),
	}), nil
}

func postgresConnection(id, name string, excludeSensitive bool) *mgmtv1alpha1.Connection {
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
							Host: "db.internal", Port: 5432, Name: "shop", User: "husonym", Pass: pass,
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
	scanFailure map[string]error
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
	ctx := t.Context()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewConnectionServiceHandler(connections))
	mux.Handle(mgmtv1alpha1connect.NewConnectionDataServiceHandler(data))
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	server := New(Options{
		Connections: maskedconn.New(api.Client(), api.URL),
		Data:        novalues.New(api.Client(), api.URL),
		AccountId:   accountId,
		Version:     "test",
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
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
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		// No tool writes before API keys carry a scope (plans/mcp-husonym.md §5.1).
		require.True(t, tool.Annotations.ReadOnlyHint, "%s is not read-only", tool.Name)
	}
	require.ElementsMatch(t, []string{
		"describe_connection", "introspect_schema", "list_connections", "suggest_mappings",
	}, names)
}
