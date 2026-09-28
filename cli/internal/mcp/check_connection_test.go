package mcp_server

import (
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func Test_CheckConnection(t *testing.T) {
	t.Parallel()

	t.Run("asks what the role needs, and gives the findings", func(t *testing.T) {
		t.Parallel()
		remedy := `GRANT SELECT ON "public"."orders" TO "reader"`
		connections := &fakeConnectionService{check: &mgmtv1alpha1.CheckConnectionConfigByIdResponse{
			IsConnected: true,
			Privileges:  []*mgmtv1alpha1.ConnectionRolePrivilege{{Grantee: "reader", Schema: "public", Table: "users"}},
			Checks: []*mgmtv1alpha1.ConnectionCheck{
				{
					Kind:    mgmtv1alpha1.ConnectionCheck_KIND_FOREIGN_KEY_SUSPENSION,
					Level:   mgmtv1alpha1.ConnectionCheck_LEVEL_WARNING,
					Message: "production cannot suspend foreign keys, which Athanor does on PostgreSQL",
				},
				{
					Kind:    mgmtv1alpha1.ConnectionCheck_KIND_READABLE,
					Level:   mgmtv1alpha1.ConnectionCheck_LEVEL_BLOCKING,
					Table:   "public.orders",
					Missing: []string{"SELECT"},
					Message: "production cannot read public.orders",
					Remedy:  &remedy,
				},
			},
		}}
		session := connectClient(t, connections, &fakeDataService{})

		res := callTool(t, session, "check_connection", map[string]any{
			"connection_id": connectionId,
			"role":          "source",
			"tables": []map[string]any{
				{"schema": "public", "table": "users", "columns": []string{"id", "email"}},
				{"schema": "public", "table": "orders"},
			},
		})

		require.Len(t, connections.checkRequests, 1)
		require.True(t, proto.Equal(&mgmtv1alpha1.CheckConnectionConfigByIdRequest{
			Id: connectionId,
			Scope: &mgmtv1alpha1.ConnectionCheckScope{
				Role: mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_SOURCE,
				Tables: []*mgmtv1alpha1.ConnectionCheckTable{
					{Schema: "public", Table: "users", Columns: []string{"id", "email"}},
					{Schema: "public", Table: "orders"},
				},
			},
		}, connections.checkRequests[0]), "sent: %v", connections.checkRequests[0])

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"connection_id": "`+connectionId+`",
			"connected": true,
			"findings": [
				{
					"level": "blocking", "kind": "readable", "table": "public.orders", "missing": ["SELECT"],
					"message": "production cannot read public.orders",
					"remedy": "GRANT SELECT ON \"public\".\"orders\" TO \"reader\""
				},
				{
					"level": "warning", "kind": "foreign_key_suspension",
					"message": "production cannot suspend foreign keys, which Athanor does on PostgreSQL"
				}
			]
		}`, string(structured))
	})

	t.Run("a destination is checked with what the run does to it", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{check: &mgmtv1alpha1.CheckConnectionConfigByIdResponse{IsConnected: true}}
		session := connectClient(t, connections, &fakeDataService{})

		callTool(t, session, "check_connection", map[string]any{
			"connection_id":          destinationId,
			"role":                   "destination",
			"engine":                 "benthos",
			"init_table_schema":      true,
			"truncate_before_insert": true,
		})

		require.Len(t, connections.checkRequests, 1)
		require.True(t, proto.Equal(&mgmtv1alpha1.ConnectionCheckScope{
			Role:                 mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION,
			Engine:               mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
			InitTableSchema:      true,
			TruncateBeforeInsert: true,
		}, connections.checkRequests[0].GetScope()))
	})

	t.Run("a connection that fails says so without the driver's words", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{check: &mgmtv1alpha1.CheckConnectionConfigByIdResponse{
			IsConnected:     false,
			ConnectionError: new(driverError),
		}}
		session := connectClient(t, connections, &fakeDataService{})

		res := callTool(t, session, "check_connection", map[string]any{"connection_id": connectionId, "role": "source"})

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"connection_id": "`+connectionId+`", "connected": false, "findings": []}`, string(structured))
	})

	t.Run("an error the API did not write is replaced", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{checkErr: errors.New("unable to create sql connection: " + driverError)}
		session := connectClient(t, connections, &fakeDataService{})

		message := callToolError(t, session, "check_connection", map[string]any{"connection_id": connectionId, "role": "source"})

		require.NotContains(t, message, clearPassword)
		require.NotContains(t, message, "db.internal")
		require.Contains(t, message, "could not end")
	})

	t.Run("an error of the API's own making reaches the model", func(t *testing.T) {
		t.Parallel()
		connections := &fakeConnectionService{checkErr: connect.NewError(connect.CodePermissionDenied,
			errors.New("the API key lacks the permission connection:view_sensitive"))}
		session := connectClient(t, connections, &fakeDataService{})

		message := callToolError(t, session, "check_connection", map[string]any{"connection_id": connectionId, "role": "source"})

		require.Contains(t, message, "connection:view_sensitive")
	})

	t.Run("refuses what it cannot ask", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string]map[string]any{
			"no role":                      {"connection_id": connectionId},
			"an unknown role":              {"connection_id": connectionId, "role": "reader"},
			"the unspecified role":         {"connection_id": connectionId, "role": "unspecified"},
			"an unknown engine":            {"connection_id": connectionId, "role": "source", "engine": "spark"},
			"the unspecified engine":       {"connection_id": connectionId, "role": "source", "engine": "unspecified"},
			"a source emptied":             {"connection_id": connectionId, "role": "source", "truncate_before_insert": true},
			"a source given tables to add": {"connection_id": connectionId, "role": "source", "init_table_schema": true},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				connections := &fakeConnectionService{check: &mgmtv1alpha1.CheckConnectionConfigByIdResponse{}}
				session := connectClient(t, connections, &fakeDataService{})

				callToolError(t, session, "check_connection", args)

				require.Empty(t, connections.checkRequests, "nothing is asked of the API")
			})
		}
	})
}
