package sqlmanager_postgres

import (
	"context"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_GetTableConstraintsBySchema_CheckConstraints(t *testing.T) {
	querier := pg_queries.NewMockQuerier(t)
	querier.EXPECT().GetNonForeignKeyTableConstraintsBySchema(mock.Anything, mock.Anything, []string{"public"}).
		Return([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow{
			{SchemaName: "public", TableName: "people", ConstraintName: "people_pkey", ConstraintType: "p", ConstraintColumns: []string{"id"}},
			{
				SchemaName: "public", TableName: "people", ConstraintName: "people_dob_check", ConstraintType: "c",
				ConstraintDefinition: `CHECK (((dob)::text ~ '^\d{4}-\d{2}-\d{2}$'::text))`,
			},
			{
				SchemaName: "public", TableName: "people", ConstraintName: "people_age_check", ConstraintType: "c",
				ConstraintDefinition: `CHECK ((age >= 0))`,
			},
		}, nil)
	querier.EXPECT().GetForeignKeyConstraintsBySchemas(mock.Anything, mock.Anything, []string{"public"}).
		Return(nil, nil)
	querier.EXPECT().GetUniqueIndexesBySchema(mock.Anything, mock.Anything, []string{"public"}).
		Return(nil, nil)

	constraints, err := NewManager(querier, nil, func() {}).GetTableConstraintsBySchema(context.Background(), []string{"public"})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"public.people": {
		`CHECK (((dob)::text ~ '^\d{4}-\d{2}-\d{2}$'::text))`,
		`CHECK ((age >= 0))`,
	}}, constraints.CheckConstraints)
	require.Equal(t, map[string][]string{"public.people": {"id"}}, constraints.PrimaryKeyConstraints)
}
