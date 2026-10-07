package usagereport

import (
	"encoding/json"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// The inventory of an instance whose every name holds the marker: the counts are the ones
// expected, and the marker is nowhere in the inventory, in a report that carries it or in what
// the reading logged.
func Test_InventoryReader_CountsTheInstanceAndCarriesNoName(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx, output := logged(t)
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(t.Context()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	queries := db_queries.New()
	db := husonymdb.New(pool, queries)
	roles, err := rbac.New(ctx, pool, testutil.GetTestLogger(t))
	require.NoError(t, err)
	store := usagestore.New(db)
	now := time.Date(2026, 10, 7, 0, 5, 0, 0, time.UTC)

	// Every name a customer could enter, each with the marker.
	names := []string{}
	named := func(name string) string {
		names = append(names, name)
		return name
	}

	first, err := queries.CreateTeamAccount(ctx, pool, named(leak+"-first-account"))
	require.NoError(t, err)
	second, err := queries.CreateTeamAccount(ctx, pool, named(leak+"-second-account"))
	require.NoError(t, err)

	// Three people and the user of an API key, which is nobody.
	person := func(email string, accounts ...db_queries.HusonymApiAccount) db_queries.HusonymApiUser {
		t.Helper()
		user, err := queries.CreateNonMachineUser(ctx, pool)
		require.NoError(t, err)
		_, err = queries.CreateIdentityProviderAssociation(ctx, pool, db_queries.CreateIdentityProviderAssociationParams{
			UserID: user.ID, ProviderSub: named("sub-of-" + email), ProviderIss: named("https://" + leak + ".example.com"),
		})
		require.NoError(t, err)
		_, err = pool.Exec(ctx,
			`UPDATE husonym_api.user_identity_provider_associations SET name = $2, email = $3 WHERE user_id = $1`,
			user.ID, named("Person "+email), named(email),
		)
		require.NoError(t, err)
		for _, account := range accounts {
			require.NoError(t, queries.CreateAccountUserAssociation(ctx, pool, db_queries.CreateAccountUserAssociationParams{
				AccountID: account.ID, UserID: user.ID,
			}))
		}
		return user
	}
	admin := person("admin@"+leak+".example.com", first, second)
	viewer := person("viewer@"+leak+".example.com", first)
	person("without@"+leak+".example.com", second)
	_, err = queries.CreateMachineUser(ctx, pool)
	require.NoError(t, err)

	give := func(user db_queries.HusonymApiUser, account db_queries.HusonymApiAccount, role mgmtv1alpha1.AccountRole) {
		t.Helper()
		require.NoError(t, roles.SetRole(ctx, rbac.NewPgUser(user.ID), rbac.NewAccount(husonymdb.UUIDString(account.ID)), role))
	}
	give(admin, first, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)
	give(admin, second, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)
	give(viewer, first, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	// One was seen today, another forty days ago.
	require.NoError(t, store.UserSeen(ctx, husonymdb.UUIDString(admin.ID), now))
	require.NoError(t, store.UserSeen(ctx, husonymdb.UUIDString(viewer.ID), now.AddDate(0, 0, -40)))

	connection := func(account db_queries.HusonymApiAccount, name string, config *pg_models.ConnectionConfig) db_queries.HusonymApiConnection {
		t.Helper()
		created, err := queries.CreateConnection(ctx, pool, db_queries.CreateConnectionParams{
			Name: named(name), AccountID: account.ID, ConnectionConfig: config, CreatedByID: admin.ID, UpdatedByID: admin.ID,
		})
		require.NoError(t, err)
		return created
	}
	production := connection(first, leak+"-production", postgresConnection())
	lower := connection(first, leak+"-lower", mysqlConnection())
	model := connection(second, leak+"-model", &pg_models.ConnectionConfig{
		OpenAiConfig: &pg_models.OpenAiConnectionConfig{ApiKey: leak, ApiUrl: "https://" + leak + ".example.com"},
	})
	// A connection no job names: it has no role, and is not counted.
	connection(second, leak+"-unused", postgresConnection())

	transformer, err := queries.CreateUserDefinedTransformer(ctx, pool, db_queries.CreateUserDefinedTransformerParams{
		Name:              named(leak + "-transformer"),
		Description:       named("what " + leak + " does"),
		Source:            int32(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL),
		AccountID:         first.ID,
		TransformerConfig: &pg_models.TransformerConfig{GenerateEmail: &pg_models.GenerateEmailConfig{}},
		CreatedByID:       admin.ID,
		UpdatedByID:       admin.ID,
	})
	require.NoError(t, err)

	job := func(
		account db_queries.HusonymApiAccount, name, cron string, options *pg_models.JobSourceOptions, mappings ...*pg_models.JobMapping,
	) db_queries.HusonymApiJob {
		t.Helper()
		if mappings == nil {
			mappings = []*pg_models.JobMapping{}
		}
		created, err := queries.CreateJob(ctx, pool, db_queries.CreateJobParams{
			Name:               named(name),
			AccountID:          account.ID,
			ConnectionOptions:  options,
			Mappings:           mappings,
			CronSchedule:       pgtype.Text{String: cron, Valid: cron != ""},
			CreatedByID:        admin.ID,
			UpdatedByID:        admin.ID,
			WorkflowOptions:    &pg_models.WorkflowOptions{},
			SyncOptions:        &pg_models.ActivityOptions{},
			VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
			JobtypeConfig:      []byte("{}"),
		})
		require.NoError(t, err)
		return created
	}
	destination := func(job db_queries.HusonymApiJob, connection db_queries.HusonymApiConnection) {
		t.Helper()
		_, err := queries.CreateJobConnectionDestination(ctx, pool, db_queries.CreateJobConnectionDestinationParams{
			JobID: job.ID, ConnectionID: connection.ID, Options: &pg_models.JobDestinationOptions{},
		})
		require.NoError(t, err)
	}
	email := configOf(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL)
	schema, users, orders := named(leak+"_schema"), named(leak+"_users"), named(leak+"_orders")

	// A scheduled synchronization, from a database to another, that subsets one of its tables.
	synchronization := job(first, leak+"-nightly", "0 3 * * *",
		postgresFrom(husonymdb.UUIDString(production.ID), named(leak+"_tenant = 'acme'")),
		mapping(t, schema, users, named(leak+"_email"), email),
		mapping(t, schema, users, named(leak+"_nickname"), userDefined(husonymdb.UUIDString(transformer.ID))),
		passthrough(t, schema, orders, named(leak+"_reference")),
	)
	destination(synchronization, lower)
	require.NoError(t, queries.InsertJobSourceColumns(ctx, pool, db_queries.InsertJobSourceColumnsParams{
		JobId:     synchronization.ID,
		Schemas:   []string{schema, schema, schema},
		Tables:    []string{users, users, orders},
		Columns:   []string{leak + "_email", leak + "_nickname", leak + "_reference"},
		DataTypes: []string{"character varying(255)", named(leak + "_nickname_domain"), "integer"},
	}))
	// A generation, which reads nothing and writes to the database the first one reads.
	generation := job(first, leak+"-seed", "",
		&pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}},
		mapping(t, schema, users, named(leak+"_login"), email),
	)
	destination(generation, production)
	// A generation by a model.
	byModel := job(second, leak+"-invented", "",
		&pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{AiConnectionId: husonymdb.UUIDString(model.ID)}},
		passthrough(t, schema, orders, named(leak+"_label")),
	)
	// A job whose mappings are not the list the product stores: it is left out, and the rest
	// is counted. What its account uses cannot be read either, as the license reads it job by
	// job: the account is left out of the features, and of nothing else.
	damaged := job(second, leak+"-damaged", "0 4 * * *", postgresFrom(husonymdb.UUIDString(model.ID), "x = 1"))
	_, err = pool.Exec(ctx,
		`UPDATE husonym_api.jobs SET mappings = jsonb_build_object($2::text, 1) WHERE id = $1`, damaged.ID, named(leak+"_key"),
	)
	require.NoError(t, err)

	// The first account declares an identity provider of its own.
	setting, err := json.Marshal(map[string]any{"oidcProvider": map[string]string{
		"issuer": named("https://sso." + leak + ".example.com"), "clientId": named(leak + "-client"),
	}})
	require.NoError(t, err)
	_, err = queries.UpsertAccountSetting(ctx, pool, db_queries.UpsertAccountSettingParams{
		AccountID: first.ID, Config: setting, CreatedByUserID: admin.ID,
	})
	require.NoError(t, err)

	reader := NewInventoryReader(db, licensegate.NewUsageReader(db, roles), roles, store, true)
	inventory, err := reader.Read(ctx, now)
	require.NoError(t, err)

	one := 1
	inUse := map[license.Feature]bool{
		license.FeatureCustomTransformers: true, // the transformer of the nickname
		license.FeatureSubsetting:         true,
		license.FeatureScheduling:         true,
		license.FeatureRbac:               true, // a member who is not an administrator
		license.FeatureSso:                true,
	}
	require.Equal(t, &Inventory{
		Connections: []telemetry.ConnectionCount{
			{Type: "mysql", Role: "destination", Count: 1},
			{Type: "openai", Role: "source", Count: 1},
			// The same connection, read by a job and written to by another.
			{Type: "postgres", Role: "destination", Count: 1},
			{Type: "postgres", Role: "source", Count: 1},
		},
		Jobs: telemetry.Jobs{
			ByKind: []telemetry.JobKindCount{
				{Kind: "ai_generate", Scheduled: false, Count: 1},
				{Kind: "generate", Scheduled: false, Count: 1},
				{Kind: "sync", Scheduled: true, Count: 1},
			},
			Tables: 4, Columns: 5, WithSubset: 1,
		},
		Transformers: telemetry.Transformers{
			System: []telemetry.TransformerColumns{
				{Name: "generate_email", Columns: 2},
				{Name: "passthrough", Columns: 2},
			},
			UserDefined: 1, UserDefinedColumns: 1,
		},
		ColumnTypes: []telemetry.ColumnTypeCount{
			{Family: "integer", Columns: 1},
			{Family: "other", Columns: 1},
			{Family: "text", Columns: 1},
		},
		Features: featureUses(inUse),
		Users: telemetry.Users{
			Accounts: 2, Users: 3, Active30d: &one,
			ByRole: []telemetry.RoleCount{
				{Role: "admin", Count: 2},
				{Role: "job_viewer", Count: 1},
				{Role: "none", Count: 1},
			},
		},
		AccountOidcProviders: 1,
		SourceTypeOfJob: map[string]string{
			husonymdb.UUIDString(synchronization.ID): "postgres",
			husonymdb.UUIDString(byModel.ID):         "openai",
		},
	}, inventory)

	// Without authentication nobody is ever seen, and the count is absent rather than zero.
	unauthenticated, err := NewInventoryReader(db, licensegate.NewUsageReader(db, roles), roles, store, false).Read(ctx, now)
	require.NoError(t, err)
	require.Nil(t, unauthenticated.Users.Active30d)

	// What the instance tells of itself beside its inventory: the sources the license counts,
	// and the major version of this very database.
	instance := NewInstanceReader(db, nil)
	sources, err := instance.SourcesCount(ctx)
	require.NoError(t, err)
	usage, err := licensegate.NewUsageReader(db, roles).Of(ctx, husonymdb.UUIDString(first.ID))
	require.NoError(t, err)
	require.Positive(t, sources)
	require.Equal(t, usage.SourcesInInstance, sources)
	var wantMajor int
	require.NoError(t, pool.QueryRow(ctx, `SELECT split_part(current_setting('server_version'), '.', 1)::int`).Scan(&wantMajor))
	major, err := instance.PostgresMajor(ctx)
	require.NoError(t, err)
	require.Equal(t, wantMajor, major)
	require.Greater(t, major, 9)

	asJSON, err := json.Marshal(inventory)
	require.NoError(t, err)
	report := &telemetry.Report{
		SchemaVersion: telemetry.SchemaVersion,
		Day:           "2026-10-06",
		GeneratedAt:   now.Format(time.RFC3339),
		Identification: telemetry.Identification{
			KeyFingerprint: "00000000000000000000000000000000000000000000000000000000000000ab",
			LicenseID:      "8f2a41c09b7e63d5",
			InstanceID:     "00000000-0000-0000-0000-0000000000aa",
			LicenseState:   "valid",
		},
		Version: telemetry.Version{Husonym: "v0.3.0"},
		Diagnostics: &telemetry.Diagnostics{
			Installation: telemetry.Installation{Kind: "helm", OS: "linux", Arch: "amd64"},
			Configuration: telemetry.Configuration{
				AccountOIDCProviders: int(inventory.AccountOidcProviders), RunLogs: "none",
			},
			Connections:  inventory.Connections,
			Jobs:         inventory.Jobs,
			Transformers: inventory.Transformers,
			ColumnTypes:  inventory.ColumnTypes,
			Features:     inventory.Features,
			Runs:         telemetry.Runs{RowsRead: "lt_1k", RowsDiscarded: "lt_1k"},
			Users:        inventory.Users,
		},
	}
	document, err := report.Marshal()
	require.NoError(t, err)
	// The inventory fits the closed lists of the report.
	require.NoError(t, telemetry.Validate(document))

	require.Contains(t, output.String(), husonymdb.UUIDString(damaged.ID))
	require.Contains(t, output.String(), husonymdb.UUIDString(second.ID))
	require.Greater(t, len(names), 30)
	for where, text := range map[string]string{
		"the inventory": string(asJSON), "the report": string(document), "the logs": output.String(),
	} {
		require.NotContains(t, text, leak, where)
		for _, name := range names {
			require.NotContains(t, text, name, where)
		}
	}
}
