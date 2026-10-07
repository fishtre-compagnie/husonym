package usagereport

import (
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// noRoles is an instance where nobody was given a role.
type noRoles struct{}

func (noRoles) Roles([]rbac.User, rbac.Account) map[rbac.User]mgmtv1alpha1.AccountRole { return nil }

// What the instance tells of itself beside its inventory, against a real database: the sources
// as the license counts them, across the accounts, and the major version of this very database.
func Test_InstanceReader_CountsTheSourcesAndReadsTheVersionOfTheDatabase(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := migratedPool(ctx, t)
	queries := db_queries.New()
	db := husonymdb.New(pool, queries)

	user, err := queries.CreateNonMachineUser(ctx, pool)
	require.NoError(t, err)
	first, err := queries.CreateTeamAccount(ctx, pool, "first")
	require.NoError(t, err)
	second, err := queries.CreateTeamAccount(ctx, pool, "second")
	require.NoError(t, err)
	job := func(account db_queries.HusonymApiAccount, name string, options *pg_models.JobSourceOptions, mappings ...*pg_models.JobMapping) {
		t.Helper()
		if mappings == nil {
			mappings = []*pg_models.JobMapping{}
		}
		_, err := queries.CreateJob(ctx, pool, db_queries.CreateJobParams{
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
	mysqlFrom := &pg_models.JobSourceOptions{MysqlOptions: &pg_models.MysqlSourceOptions{ConnectionId: connectionTwo}}
	// One database read by two jobs, a MySQL server of which two databases are read, in two
	// accounts, and a generation, which reads nothing: three sources.
	job(first, "one", postgresFrom(connectionOne))
	job(first, "again", postgresFrom(connectionOne))
	job(second, "two", mysqlFrom, passthrough(t, "shop", "t", "a"), passthrough(t, "crm", "t", "a"))
	job(second, "seed", &pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}})

	instance := NewInstanceReader(db, nil)
	sources, err := instance.SourcesCount(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, sources)
	// It is the number the license counts, whichever account asks.
	for _, account := range []db_queries.HusonymApiAccount{first, second} {
		usage, err := licensegate.NewUsageReader(db, noRoles{}).Of(ctx, husonymdb.UUIDString(account.ID))
		require.NoError(t, err)
		require.Equal(t, usage.SourcesInInstance, sources)
	}

	var wantMajor int
	require.NoError(t, pool.QueryRow(ctx, `SELECT split_part(current_setting('server_version'), '.', 1)::int`).Scan(&wantMajor))
	major, err := instance.PostgresMajor(ctx)
	require.NoError(t, err)
	require.Equal(t, wantMajor, major)
	require.Greater(t, major, 9)
}
