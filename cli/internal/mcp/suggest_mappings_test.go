package mcp_server

import (
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func Test_SuggestMappings(t *testing.T) {
	t.Parallel()

	t.Run("from names alone, without reading a value", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{}
		session := connectClient(t, &fakeConnectionService{}, data)

		res := callTool(t, session, "suggest_mappings", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users", "public.orders"},
		})

		require.Empty(t, data.scannedTables(), "no scan was asked for")
		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"tables": [
			{
				"table": "public.orders",
				"columns": [
					{"column": "id", "sensitive": false, "keys": ["primary"]},
					{"column": "user_id", "sensitive": false, "keys": ["foreign"]},
					{"column": "note", "sensitive": false}
				]
			},
			{
				"table": "public.users",
				"columns": [
					{"column": "id", "sensitive": false, "keys": ["primary", "referenced"]},
					{"column": "email", "sensitive": true, "category": "email",
					 "suggested_transformer": "generate_email", "confidence": "confirmed", "method": "column_name",
					 "evidence": "reconnu par le nom de colonne « email »"},
					{"column": "birth_date", "sensitive": true, "category": "birth_date",
					 "suggested_transformer": "transform_javascript", "confidence": "confirmed", "method": "column_name",
					 "evidence": "reconnu par le nom de colonne « birth_date »"}
				]
			}
		]}`, string(structured))
	})

	t.Run("with a content scan, which the name wins over unless the format is in doubt", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{scanFailure: map[string]error{
			"public.locked": connect.NewError(connect.CodeDeadlineExceeded, errors.New("table is locked")),
		}}
		session := connectClient(t, &fakeConnectionService{}, data)

		res := callTool(t, session, "suggest_mappings", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users", "public.orders", "public.locked"},
			"scan_content":  true,
		})

		require.ElementsMatch(t, []string{"public.users", "public.orders", "public.locked"}, data.scannedTables())
		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"tables": [
			{
				"table": "public.locked",
				"columns": [{"column": "id", "sensitive": false, "content_not_analyzed": true}],
				"scan_error": "the API could not scan public.locked: deadline_exceeded"
			},
			{
				"table": "public.orders",
				"columns": [
					{"column": "id", "sensitive": false, "keys": ["primary"]},
					{"column": "user_id", "sensitive": false, "keys": ["foreign"]},
					{"column": "note", "sensitive": true, "category": "person_name",
					 "suggested_transformer": "transform_pii_text", "confidence": "needs_review", "method": "content",
					 "evidence": "PERSON reconnu par analyse de contenu sur 6/20 valeurs (score moyen 0.85)"}
				]
			},
			{
				"table": "public.users",
				"columns": [
					{"column": "id", "sensitive": false, "keys": ["primary", "referenced"]},
					{"column": "email", "sensitive": true, "category": "email",
					 "suggested_transformer": "generate_email", "confidence": "confirmed", "method": "column_name",
					 "evidence": "reconnu par le nom de colonne « email »"},
					{"column": "birth_date", "sensitive": true, "category": "birth_date",
					 "suggested_transformer": "transform_javascript", "confidence": "needs_review", "method": "format",
					 "evidence": "format ambigu sur 20 valeurs : jj/mm/aaaa ou mm/jj/aaaa"}
				]
			}
		]}`, string(structured))
	})

	// A column the scan could not analyze is not known to be free of personal data: the agent
	// is told which, beside what the name of each says.
	t.Run("tells the columns the content scan could not analyze", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{notAnalyzed: map[string][]string{"public.orders": {"user_id"}, "public.users": {"email"}}}
		session := connectClient(t, &fakeConnectionService{}, data)

		res := callTool(t, session, "suggest_mappings", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users", "public.orders"},
			"scan_content":  true,
		})

		var out suggestMappingsOutput
		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(structured, &out))
		notAnalyzed := map[string]bool{}
		sensitive := map[string]bool{}
		for _, table := range out.Tables {
			require.Empty(t, table.ScanError)
			for _, column := range table.Columns {
				notAnalyzed[table.Table+"."+column.Column] = column.ContentNotAnalyzed
				sensitive[table.Table+"."+column.Column] = column.Sensitive
			}
		}
		require.True(t, notAnalyzed["public.orders.user_id"])
		require.False(t, sensitive["public.orders.user_id"])
		require.True(t, notAnalyzed["public.users.email"])
		require.True(t, sensitive["public.users.email"], "the name of the column still speaks")
		require.False(t, notAnalyzed["public.orders.note"])
		require.False(t, notAnalyzed["public.users.id"])

		// Without a scan, nothing is said of the content.
		res = callTool(t, session, "suggest_mappings", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{"public.users"},
		})
		structured, err = json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NotContains(t, string(structured), "content_not_analyzed")
	})

	t.Run("needs at least one table", func(t *testing.T) {
		t.Parallel()
		session := connectClient(t, &fakeConnectionService{}, &fakeDataService{})

		message := callToolError(t, session, "suggest_mappings", map[string]any{
			"connection_id": connectionId,
			"tables":        []string{},
		})
		require.Contains(t, message, "no table given")
	})
}
