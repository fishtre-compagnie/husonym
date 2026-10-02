package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// A domain without constraint is read, and told without any.
func Test_PostgresManager_ReadsADomainWithoutConstraint(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx := context.Background()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	db, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE DOMAIN app.amount AS int;
		CREATE DOMAIN app.price AS int CONSTRAINT price_positive CHECK (VALUE > 0);
		CREATE TABLE app.t (id int PRIMARY KEY, a app.amount, p app.price);`)
	require.NoError(t, err)

	manager := postgres.NewManager(pg_queries.New(), db, func() {})
	datatypes, err := manager.GetDataTypesByTables(ctx, []*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "t"}})
	require.NoError(t, err)

	constraints := map[string][]*sqlmanager_shared.DomainConstraint{}
	for _, domain := range datatypes.Domains {
		constraints[domain.Name] = domain.Constraints
	}
	require.Len(t, constraints, 2)
	require.Empty(t, constraints["amount"])
	require.Equal(t,
		[]*sqlmanager_shared.DomainConstraint{{Name: "price_positive", Definition: "CHECK ((VALUE > 0))"}},
		constraints["price"])
}
