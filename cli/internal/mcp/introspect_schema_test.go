package mcp_server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_IntrospectSchema(t *testing.T) {
	t.Parallel()

	t.Run("without tables, lists them", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

		res := callTool(t, session, "introspect_schema", map[string]any{"connection_id": connectionId})

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"tables": [
			{"table": "public.locked"}, {"table": "public.orders"}, {"table": "public.users"}
		]}`, string(structured))
	})

	t.Run("with tables, gives their columns and keys both ways", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

		res := callTool(t, session, "introspect_schema", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users", "public.orders", "public.users"},
		})

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"tables": [
			{
				"table": "public.orders",
				"columns": [
					{"name": "id", "data_type": "uuid", "nullable": false},
					{"name": "user_id", "data_type": "uuid", "nullable": false},
					{"name": "note", "data_type": "text", "nullable": true}
				],
				"primary_key": ["id"],
				"foreign_keys": [
					{"columns": ["user_id"], "references": "public.users", "referenced_columns": ["id"], "nullable": false}
				]
			},
			{
				"table": "public.users",
				"columns": [
					{"name": "id", "data_type": "uuid", "nullable": false},
					{"name": "email", "data_type": "character varying", "nullable": false, "character_max_length": 255},
					{"name": "birth_date", "data_type": "text", "nullable": true}
				],
				"primary_key": ["id"],
				"unique": [["email"]],
				"referenced_by": [
					{"table": "public.orders", "columns": ["user_id"], "referenced_columns": ["id"]}
				]
			}
		]}`, string(structured))
	})

	t.Run("refuses a table that is not there, and says how to find them", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

		message := callToolError(t, session, "introspect_schema", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users", "public.nope"},
		})
		require.Contains(t, message, "public.nope")
		require.NotContains(t, message, "public.users")
		require.Contains(t, message, "introspect_schema without tables")
	})

	t.Run("refuses more tables than a call describes", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

		tables := make([]string, 0, maxTablesPerCall+1)
		for i := range maxTablesPerCall + 1 {
			tables = append(tables, fmt.Sprintf("public.t%d", i))
		}
		message := callToolError(t, session, "introspect_schema", map[string]any{
			"connection_id": connectionId,
			"tables":        tables,
		})
		require.Contains(t, message, "split the call")
	})
}
