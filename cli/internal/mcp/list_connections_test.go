package mcp_server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	accountId     = "7f2c1e4a-0000-4000-8000-000000000001"
	clearPassword = "hunter2-in-clear"
)

// fakeConnectionService answers like the API does: the password comes back in clear unless
// the request asks to leave the secrets out.
type fakeConnectionService struct {
	mgmtv1alpha1connect.UnimplementedConnectionServiceHandler

	err error

	mu       sync.Mutex
	requests []*mgmtv1alpha1.GetConnectionsRequest
}

func (f *fakeConnectionService) GetConnections(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetConnectionsRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionsResponse], error) {
	f.mu.Lock()
	f.requests = append(f.requests, req.Msg)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}

	pass := clearPassword
	if req.Msg.GetExcludeSensitive() {
		pass = "********"
	}
	created := timestamppb.New(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
	updated := timestamppb.New(time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC))
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionsResponse{
		Connections: []*mgmtv1alpha1.Connection{
			{
				Id:   "b",
				Name: "production",
				ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
					Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
						PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
							ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Connection{
								Connection: &mgmtv1alpha1.PostgresConnection{
									Host: "db.internal", User: "husonym", Pass: pass,
								},
							},
						},
					},
				},
				CreatedAt: created,
				UpdatedAt: updated,
			},
			{
				Id:   "a",
				Name: "local",
				ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
					Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{}},
				},
				CreatedAt: created,
				UpdatedAt: updated,
			},
		},
	}), nil
}

func (f *fakeConnectionService) seen() []*mgmtv1alpha1.GetConnectionsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// connectClient stands the fake API up, serves the MCP server on it, and returns a client
// session talking to that server.
func connectClient(t *testing.T, service *fakeConnectionService) *mcp.ClientSession {
	t.Helper()
	ctx := t.Context()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewConnectionServiceHandler(service))
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	server := New(Options{
		Connections: maskedconn.New(api.Client(), api.URL),
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

func Test_ListConnections(t *testing.T) {
	t.Parallel()

	t.Run("is the only tool", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{})

		tools, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		require.Equal(t, "list_connections", tools.Tools[0].Name)
		require.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("lists the account's connections, sorted, without their secrets", func(t *testing.T) {
		t.Parallel()
		service := &fakeConnectionService{}
		session := connectClient(t, service)

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_connections"})
		require.NoError(t, err)
		require.False(t, res.IsError)

		requests := service.seen()
		require.Len(t, requests, 1)
		require.Equal(t, accountId, requests[0].GetAccountId())
		require.True(t, requests[0].GetExcludeSensitive(), "the connections were asked for with their secrets")

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"connections": [
			{"id": "a", "name": "local", "category": "MySQL",
			 "created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-02T08:00:00Z"},
			{"id": "b", "name": "production", "category": "PostgreSQL",
			 "created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-02T08:00:00Z"}
		]}`, string(structured))

		whole, err := json.Marshal(res)
		require.NoError(t, err)
		require.NotContains(t, string(whole), clearPassword)
	})

	t.Run("says why when the API refuses", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{
			err: connect.NewError(connect.CodePermissionDenied, errors.New("cannot view connections")),
		})

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_connections"})
		require.NoError(t, err, "a refusal is a tool error for the model to read, not a protocol error")
		require.True(t, res.IsError)
		require.Len(t, res.Content, 1)
		text, ok := res.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.Contains(t, text.Text, accountId)
		require.Contains(t, text.Text, "permission_denied")
		require.Contains(t, text.Text, "cannot view connections")
	})
}
