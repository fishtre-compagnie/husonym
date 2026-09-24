package mcp_server

import (
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func Test_ListConnections(t *testing.T) {
	t.Parallel()

	t.Run("lists the account's connections, sorted, without their secrets", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{}
		session := connectClient(t, connections, &fakeDataService{})

		res := callTool(t, session, "list_connections", nil)

		require.Len(t, connections.listRequests, 1)
		require.Equal(t, accountId, connections.listRequests[0].GetAccountId())
		require.True(t, connections.listRequests[0].GetExcludeSensitive(), "the connections were asked for with their secrets")

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"connections": [
			{"id": "a", "name": "local", "category": "MySQL",
			 "created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-02T08:00:00Z"},
			{"id": "`+connectionId+`", "name": "production", "category": "PostgreSQL",
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
		}, &fakeDataService{})

		message := callToolError(t, session, "list_connections", nil)
		require.Contains(t, message, accountId)
		require.Contains(t, message, "permission_denied")
		require.Contains(t, message, "cannot view connections")
	})
}
