package mcp_server

import (
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func Test_DescribeConnection(t *testing.T) {
	t.Parallel()

	t.Run("describes the connection with its secrets masked", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{}
		session := connectClient(t, connections, &fakeDataService{})

		res := callTool(t, session, "describe_connection", map[string]any{"connection_id": connectionId})

		require.Len(t, connections.getRequests, 1)
		require.Equal(t, connectionId, connections.getRequests[0].GetId())
		require.True(t, connections.getRequests[0].GetExcludeSensitive(), "the connection was asked for with its secrets")

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"id": "`+connectionId+`", "name": "production", "category": "PostgreSQL",
			"config": {"pg_config": {"connection": {
				"host": "db.internal", "port": 5432, "name": "shop", "user": "husonym", "pass": "********"
			}}},
			"created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-02T08:00:00Z"
		}`, string(structured))

		whole, err := json.Marshal(res)
		require.NoError(t, err)
		require.NotContains(t, string(whole), clearPassword)
	})

	t.Run("says why when the connection is not there", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{
			err: connect.NewError(connect.CodeNotFound, errors.New("unable to find connection by id")),
		}, &fakeDataService{})

		message := callToolError(t, session, "describe_connection", map[string]any{"connection_id": connectionId})
		require.Contains(t, message, connectionId)
		require.Contains(t, message, "not_found")
	})
}
