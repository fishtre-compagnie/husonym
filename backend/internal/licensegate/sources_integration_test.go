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

// The license covers the instance, so the list of sources has to come from the jobs of every
// account at once, and the lock has to be takeable.
func Test_ListJobSourcesOfInstance_CrossesAccounts(t *testing.T) {
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

	var accounts []db_queries.HusonymApiAccount
	for _, slug := range []string{"first", "second"} {
		account, err := queries.CreateTeamAccount(ctx, container.DB, slug)
		require.NoError(t, err)
		_, err = queries.CreateJob(ctx, container.DB, db_queries.CreateJobParams{
			Name:               "job-" + slug,
			AccountID:          account.ID,
			ConnectionOptions:  &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: "conn-" + slug}},
			Mappings:           []*pg_models.JobMapping{},
			CreatedByID:        user.ID,
			UpdatedByID:        user.ID,
			WorkflowOptions:    &pg_models.WorkflowOptions{},
			SyncOptions:        &pg_models.ActivityOptions{},
			VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
			JobtypeConfig:      []byte("{}"),
		})
		require.NoError(t, err)
		accounts = append(accounts, account)
	}

	tx, err := container.DB.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })
	require.NoError(t, queries.LockLicenseSources(ctx, tx))

	rows, err := queries.ListJobSourcesOfInstance(ctx, tx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	sources := SourcesOf(rows)
	require.ElementsMatch(t, []Source{
		{AccountId: husonymdb.UUIDString(accounts[0].ID), ConnectionId: "conn-first"},
		{AccountId: husonymdb.UUIDString(accounts[1].ID), ConnectionId: "conn-second"},
	}, sources)
}
