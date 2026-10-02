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

// A domain is told with its default, its nullability and all its constraints. One statement
// creates it with all of them but its default, which is set after, and each statement that
// reconciles a domain is one PostgreSQL accepts.
func Test_PostgresManager_Domains(t *testing.T) {
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

	createTable := `CREATE TABLE app.t (id int PRIMARY KEY, a app.amount, p app."Plain", c app.counted);`
	_, err = db.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE DOMAIN app.amount AS numeric(10,2) DEFAULT 1 NOT NULL
			CONSTRAINT positive CHECK (VALUE > 0)
			CONSTRAINT "Small" CHECK (VALUE < 1000);
		ALTER DOMAIN app.amount ADD CONSTRAINT lazy CHECK (VALUE <> 5) NOT VALID;
		CREATE DOMAIN app."Plain" AS text;
		CREATE SEQUENCE app.counter;
		CREATE DOMAIN app.counted AS bigint DEFAULT nextval('app.counter');`+createTable)
	require.NoError(t, err)

	manager := postgres.NewManager(pg_queries.New(), db, func() {})
	tables := []*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "t"}}
	domains := func(t *testing.T) map[string]*sqlmanager_shared.DomainDataType {
		t.Helper()
		datatypes, err := manager.GetDataTypesByTables(ctx, tables)
		require.NoError(t, err)
		byName := map[string]*sqlmanager_shared.DomainDataType{}
		for _, domain := range datatypes.Domains {
			byName[domain.Name] = domain
		}
		return byName
	}
	exec := func(t *testing.T, statements ...string) {
		t.Helper()
		for _, statement := range statements {
			_, err := db.ExecContext(ctx, statement)
			require.NoError(t, err, statement)
		}
	}

	baseTypes := func(t *testing.T) map[string]string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `
			SELECT t.typname, format_type(t.typbasetype, t.typtypmod)
			FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
			WHERE n.nspname = 'app' AND t.typtype = 'd'`)
		require.NoError(t, err)
		defer rows.Close()
		byName := map[string]string{}
		for rows.Next() {
			var name, baseType string
			require.NoError(t, rows.Scan(&name, &baseType))
			byName[name] = baseType
		}
		require.NoError(t, rows.Err())
		return byName
	}

	asCreated := domains(t)
	require.Len(t, asCreated, 3)
	require.False(t, asCreated["amount"].IsNullable)
	require.Equal(t, "1", asCreated["amount"].Default)
	lazy := &sqlmanager_shared.DomainConstraint{Name: "lazy", Definition: "CHECK ((VALUE <> (5)::numeric)) NOT VALID"}
	require.ElementsMatch(t, []*sqlmanager_shared.DomainConstraint{
		{Name: "positive", Definition: "CHECK ((VALUE > (0)::numeric))"},
		{Name: "Small", Definition: "CHECK ((VALUE < (1000)::numeric))"},
		lazy,
	}, asCreated["amount"].Constraints)
	require.True(t, asCreated["Plain"].IsNullable)
	require.Empty(t, asCreated["Plain"].Default)
	require.Empty(t, asCreated["Plain"].Constraints)
	require.Equal(t, "nextval('app.counter'::regclass)", asCreated["counted"].Default)
	baseTypesAsCreated := baseTypes(t)
	require.Equal(t, map[string]string{"amount": "numeric(10,2)", "Plain": "text", "counted": "bigint"}, baseTypesAsCreated)

	t.Run("one statement creates the domain, its default and what is not validated come after", func(t *testing.T) {
		datatypes, err := manager.GetSchemaTableDataTypes(ctx, tables)
		require.NoError(t, err)
		require.Len(t, datatypes.Domains, 3, "one statement by domain, whatever its constraints")

		// The sequence the default of a domain calls is not there when the domain is created.
		exec(t, `DROP TABLE app.t; DROP DOMAIN app.amount; DROP DOMAIN app."Plain"; DROP DOMAIN app.counted;
			DROP SEQUENCE app.counter;`)
		for _, domain := range datatypes.Domains {
			exec(t, domain.Definition)
		}
		exec(t, `CREATE SEQUENCE app.counter;`, createTable)

		created := domains(t)
		require.Len(t, created, 3)
		require.Equal(t, baseTypesAsCreated, baseTypes(t))
		require.Equal(t, asCreated["Plain"].Fingerprint, created["Plain"].Fingerprint)
		require.False(t, created["amount"].IsNullable)
		require.Empty(t, created["amount"].Default)
		require.Empty(t, created["counted"].Default)
		require.ElementsMatch(t, []*sqlmanager_shared.DomainConstraint{
			{Name: "positive", Definition: "CHECK ((VALUE > (0)::numeric))"},
			{Name: "Small", Definition: "CHECK ((VALUE < (1000)::numeric))"},
		}, created["amount"].Constraints)

		// What a reconciliation does next: the defaults, and the constraint the source has more.
		exec(t,
			postgres.BuildUpdateDomainDefaultStatement("app", "amount", asCreated["amount"].Default),
			postgres.BuildUpdateDomainDefaultStatement("app", "counted", asCreated["counted"].Default),
		)
		exec(t, postgres.BuildDomainConstraintStatements("app", "amount", map[string]string{lazy.Name: lazy.Definition}, nil)...)

		reconciled := domains(t)
		for name, domain := range asCreated {
			require.Equal(t, domain.Fingerprint, reconciled[name].Fingerprint, "the domain %s is not as it was", name)
		}
	})

	t.Run("the statements that reconcile a domain", func(t *testing.T) {
		exec(t, postgres.BuildDomainConstraintStatements(
			"app", "amount",
			map[string]string{"Small": "CHECK (VALUE < 100)", "odd": "CHECK (VALUE <> 13)"},
			[]string{"Small", "positive", "lazy"},
		)...)
		exec(t,
			postgres.BuildUpdateDomainNotNullStatement("app", "amount", true),
			postgres.BuildDropDomainDefaultStatement("app", "amount"),
			postgres.BuildUpdateDomainNotNullStatement("app", "Plain", false),
			postgres.BuildUpdateDomainDefaultStatement("app", "Plain", "'none'::text"),
		)

		reconciled := domains(t)
		require.True(t, reconciled["amount"].IsNullable)
		require.Empty(t, reconciled["amount"].Default)
		require.ElementsMatch(t, []*sqlmanager_shared.DomainConstraint{
			{Name: "Small", Definition: "CHECK ((VALUE < (100)::numeric))"},
			{Name: "odd", Definition: "CHECK ((VALUE <> (13)::numeric))"},
		}, reconciled["amount"].Constraints)
		require.False(t, reconciled["Plain"].IsNullable)
		require.Equal(t, "'none'::text", reconciled["Plain"].Default)
		require.NotEqual(t, asCreated["amount"].Fingerprint, reconciled["amount"].Fingerprint)
		require.NotEqual(t, asCreated["Plain"].Fingerprint, reconciled["Plain"].Fingerprint)
	})
}
