package sqlmanager_mysql

import (
	"context"
	"encoding/json"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_GetTableConstraintsBySchema_CheckConstraints(t *testing.T) {
	querier := mysql_queries.NewMockQuerier(t)
	querier.EXPECT().GetTableConstraintsBySchemas(mock.Anything, mock.Anything, []string{"shop"}).
		Return([]*mysql_queries.GetTableConstraintsBySchemasRow{
			{
				SchemaName: "shop", TableName: "people", ConstraintName: "PRIMARY", ConstraintType: "PRIMARY KEY",
				ConstraintColumns: json.RawMessage(`["id"]`), CheckClause: []uint8(""),
			},
			{
				SchemaName: "shop", TableName: "people", ConstraintName: "people_chk_1", ConstraintType: "CHECK",
				ConstraintColumns: json.RawMessage(`[null]`),
				CheckClause:       []uint8("regexp_like(`dob`,_utf8mb4'^[0-9]{4}-[0-9]{2}-[0-9]{2}$')"),
			},
		}, nil)

	manager := &MysqlManager{resolvedQuerier: querier}
	constraints, err := manager.GetTableConstraintsBySchema(context.Background(), []string{"shop"})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"shop.people": {
		"regexp_like(`dob`,_utf8mb4'^[0-9]{4}-[0-9]{2}-[0-9]{2}$')",
	}}, constraints.CheckConstraints)
	require.Equal(t, map[string][]string{"shop.people": {"id"}}, constraints.PrimaryKeyConstraints)
}
