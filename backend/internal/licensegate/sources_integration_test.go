package licensegate

import (
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

func columnsOf(schema string, columns ...string) []*pg_models.JobMapping {
	mappings := make([]*pg_models.JobMapping, 0, len(columns))
	for _, column := range columns {
		mappings = append(mappings, &pg_models.JobMapping{Schema: schema, Table: "t", Column: column})
	}
	return mappings
}

// The license covers the instance, so the list of sources has to come from the jobs of every
// account at once, and the schemas the query computes from the mappings have to be the ones the
// count needs.
func Test_ListJobSourcesOfInstance_CrossesAccountsAndGivesSchemas(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(t.Context()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))

	queries := db_queries.New()
	user, err := queries.SetAnonymousUser(ctx, container.DB)
	require.NoError(t, err)
	first, err := queries.CreateTeamAccount(ctx, container.DB, "first")
	require.NoError(t, err)
	second, err := queries.CreateTeamAccount(ctx, container.DB, "second")
	require.NoError(t, err)

	createJob := func(account db_queries.HusonymApiAccount, name string, options *pg_models.JobSourceOptions, mappings []*pg_models.JobMapping) {
		t.Helper()
		_, err := queries.CreateJob(ctx, container.DB, db_queries.CreateJobParams{
			Name:               name,
			AccountID:          account.ID,
			ConnectionOptions:  options,
			Mappings:           mappings,
			CreatedByID:        user.ID,
			UpdatedByID:        user.ID,
			WorkflowOptions:    &pg_models.WorkflowOptions{},
			SyncOptions:        &pg_models.ActivityOptions{},
			VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
			JobtypeConfig:      []byte("{}"),
		})
		require.NoError(t, err)
	}

	// Two schemas with several columns each, and a mapping with no schema at all.
	mysqlMappings := append(columnsOf("shop", "a", "b", "c"), columnsOf("crm", "a", "b")...)
	mysqlMappings = append(mysqlMappings, columnsOf("", "orphan")...)
	createJob(first, "mysql", mysql("my"), mysqlMappings)
	createJob(first, "mongo", &pg_models.JobSourceOptions{MongoDbOptions: &pg_models.MongoDbSourceOptions{ConnectionId: "mg"}}, columnsOf("events", "a"))
	createJob(second, "postgres", postgres("pg"), columnsOf("public", "a", "b"))
	createJob(second, "mysql-no-mappings", mysql("my2"), []*pg_models.JobMapping{})
	// Mappings that are a JSON null, not an array: the query must not try to read elements out of
	// them. The queries of the product cannot store one (a nil slice is sent as a SQL NULL, which
	// the column refuses), so it is written directly.
	createJob(second, "mysql-null-mappings", mysql("my3"), []*pg_models.JobMapping{})
	_, err = container.DB.Exec(ctx, `UPDATE husonym_api.jobs SET mappings = 'null'::jsonb WHERE name = 'mysql-null-mappings'`)
	require.NoError(t, err)

	tx, err := container.DB.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })
	require.NoError(t, queries.LockLicenseSources(ctx, tx))

	rows, err := queries.ListJobSourcesOfInstance(ctx, tx)
	require.NoError(t, err)
	require.Len(t, rows, 5)

	var schemas = map[string][]string{}
	for _, row := range rows {
		switch {
		case row.ConnectionOptions.MysqlOptions != nil:
			schemas[row.ConnectionOptions.MysqlOptions.ConnectionId] = row.Schemas
		case row.ConnectionOptions.MongoDbOptions != nil:
			schemas[row.ConnectionOptions.MongoDbOptions.ConnectionId] = row.Schemas
		case row.ConnectionOptions.PostgresOptions != nil:
			schemas[row.ConnectionOptions.PostgresOptions.ConnectionId] = row.Schemas
		}
	}
	require.Equal(t, map[string][]string{
		"my":  {"crm", "shop"},
		"mg":  {"events"},
		"pg":  {},
		"my2": {},
		"my3": {},
	}, schemas)

	firstId, secondId := husonymdb.UUIDString(first.ID), husonymdb.UUIDString(second.ID)
	require.ElementsMatch(t, []Source{
		{AccountId: firstId, ConnectionId: "my", Database: "crm"},
		{AccountId: firstId, ConnectionId: "my", Database: "shop"},
		{AccountId: firstId, ConnectionId: "mg", Database: "events"},
		{AccountId: secondId, ConnectionId: "pg"},
		{AccountId: secondId, ConnectionId: "my2"},
		{AccountId: secondId, ConnectionId: "my3"},
	}, SourcesOf(rows))
}
