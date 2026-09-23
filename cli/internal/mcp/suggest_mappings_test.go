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
				"columns": [{"column": "id", "sensitive": false}],
				"scan_error": "deadline_exceeded: table is locked"
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
