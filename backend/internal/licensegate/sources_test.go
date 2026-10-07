package licensegate

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

var (
	accountA = pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	accountB = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
)

const (
	idA = "01000000-0000-0000-0000-000000000000"
	idB = "02000000-0000-0000-0000-000000000000"
)

// stored is the job type as the job service writes it to the database.
func stored(t *testing.T, config *mgmtv1alpha1.JobTypeConfig) []byte {
	t.Helper()
	bits, err := json.Marshal(config)
	require.NoError(t, err)
	return bits
}

func syncJob(t *testing.T, account pgtype.UUID, options *pg_models.JobSourceOptions, schemas ...string) db_queries.ListJobSourcesOfInstanceRow {
	t.Helper()
	return db_queries.ListJobSourcesOfInstanceRow{
		AccountID:         account,
		ConnectionOptions: options,
		Schemas:           schemas,
		JobtypeConfig: stored(t, &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_Sync{
			Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{},
		}}),
	}
}

func postgres(connection string) *pg_models.JobSourceOptions {
	return &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: connection}}
}

func mysql(connection string) *pg_models.JobSourceOptions {
	return &pg_models.JobSourceOptions{MysqlOptions: &pg_models.MysqlSourceOptions{ConnectionId: connection}}
}

func Test_SourcesOf_OneSourcePerConnectionForPostgresMssqlAndDynamo(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, postgres("pg"), "public"),
		syncJob(t, accountA, &pg_models.JobSourceOptions{MssqlOptions: &pg_models.MssqlSourceOptions{ConnectionId: "ms"}}, "dbo"),
		syncJob(t, accountA, &pg_models.JobSourceOptions{DynamoDBOptions: &pg_models.DynamoDBSourceOptions{ConnectionId: "dy"}}),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "dy"},
		{AccountId: idA, ConnectionId: "ms"},
		{AccountId: idA, ConnectionId: "pg"},
	}, SourcesOf(jobs))
}

func Test_SourcesOf_TwoJobsOnOnePostgresConnectionAreOneSource(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, postgres("pg"), "public"),
		syncJob(t, accountA, postgres("pg"), "other"),
	}
	require.Equal(t, []Source{{AccountId: idA, ConnectionId: "pg"}}, SourcesOf(jobs))
}

func Test_SourcesOf_MysqlAndMongoCountOneSourcePerSchema(t *testing.T) {
	mongo := &pg_models.JobSourceOptions{MongoDbOptions: &pg_models.MongoDbSourceOptions{ConnectionId: "mg"}}
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, mysql("my"), "shop"),
		syncJob(t, accountA, mysql("my"), "crm"),
		syncJob(t, accountA, mongo, "events"),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "mg", Database: "events"},
		{AccountId: idA, ConnectionId: "my", Database: "crm"},
		{AccountId: idA, ConnectionId: "my", Database: "shop"},
	}, SourcesOf(jobs))
}

func Test_SourcesOf_MysqlDeduplicatesSchemasWithinAndAcrossJobs(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, mysql("my"), "shop"),
		syncJob(t, accountA, mysql("my"), "shop"),
	}
	require.Equal(t, []Source{{AccountId: idA, ConnectionId: "my", Database: "shop"}}, SourcesOf(jobs))
}

func Test_SourcesOf_MysqlWithoutUsableSchemaCountsTheConnection(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, mysql("my")),
		syncJob(t, accountA, mysql("other")),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "my"},
		{AccountId: idA, ConnectionId: "other"},
	}, SourcesOf(jobs))
}

// A connection seen once without a database and once with one is two entries: the first job does
// not say which database it reads, so it is not assumed to be the second one.
func Test_SourcesOf_MysqlWithAndWithoutDatabaseAreTwoEntries(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, mysql("my")),
		syncJob(t, accountA, mysql("my"), "shop"),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "my"},
		{AccountId: idA, ConnectionId: "my", Database: "shop"},
	}, SourcesOf(jobs))
}

func Test_SourcesOf_AccountsAreCountedSeparately(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountB, postgres("pg"), "public"),
		syncJob(t, accountA, postgres("pg"), "public"),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "pg"},
		{AccountId: idB, ConnectionId: "pg"},
	}, SourcesOf(jobs))
}

func Test_SourcesOf_OutputIsSorted(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountB, postgres("a")),
		syncJob(t, accountA, postgres("z")),
		syncJob(t, accountA, mysql("m"), "b"),
		syncJob(t, accountA, mysql("m"), "a"),
		syncJob(t, accountA, postgres("b")),
	}
	require.Equal(t, []Source{
		{AccountId: idA, ConnectionId: "b"},
		{AccountId: idA, ConnectionId: "m", Database: "a"},
		{AccountId: idA, ConnectionId: "m", Database: "b"},
		{AccountId: idA, ConnectionId: "z"},
		{AccountId: idB, ConnectionId: "a"},
	}, SourcesOf(jobs))
}

func Test_SourcesOf_GenerationJobsDoNotCount(t *testing.T) {
	fk := "pg"
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, &pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{FkSourceConnectionId: &fk}}),
		syncJob(t, accountA, &pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{AiConnectionId: "ai", FkSourceConnectionId: &fk}}),
	}
	require.Empty(t, SourcesOf(jobs))
}

func Test_SourcesOf_PiiDetectJobsDoNotCount(t *testing.T) {
	job := syncJob(t, accountA, postgres("pg"), "public")
	job.JobtypeConfig = stored(t, &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
		PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
	}})
	require.Empty(t, SourcesOf([]db_queries.ListJobSourcesOfInstanceRow{job}))
}

// A job created without a type stores an empty object: it is a synchronization job.
func Test_SourcesOf_JobWithoutTypeCounts(t *testing.T) {
	job := syncJob(t, accountA, postgres("pg"), "public")
	job.JobtypeConfig = []byte("{}")
	require.Equal(t, []Source{{AccountId: idA, ConnectionId: "pg"}}, SourcesOf([]db_queries.ListJobSourcesOfInstanceRow{job}))
}

// A job that cannot be classified counts: it must not hide.
func Test_SourcesOf_UndecodableJobTypeCounts(t *testing.T) {
	job := syncJob(t, accountA, postgres("pg"), "public")
	job.JobtypeConfig = []byte("not json")
	require.Equal(t, []Source{{AccountId: idA, ConnectionId: "pg"}}, SourcesOf([]db_queries.ListJobSourcesOfInstanceRow{job}))
}

// Every field of the source options is a decision: either it is a source the license counts, or
// it is not. A field in neither list is an engine nobody decided about, and its jobs would go
// uncounted.
func Test_SourceOptionsFieldsAreAllDecided(t *testing.T) {
	counted := []string{"PostgresOptions", "MysqlOptions", "MongoDbOptions", "DynamoDBOptions", "MssqlOptions"}
	notCounted := []string{"GenerateOptions", "AiGenerateOptions"}

	typ := reflect.TypeFor[pg_models.JobSourceOptions]()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		require.Truef(t, slices.Contains(counted, name) != slices.Contains(notCounted, name),
			"JobSourceOptions.%s must be in exactly one of the counted and not-counted lists of this test: "+
				"decide in sources.go (and in ListJobSourcesOfInstance if it has databases) whether it is a source",
			name)
	}
	require.Equal(t, len(counted)+len(notCounted), typ.NumField(), "a listed field no longer exists")
}

func Test_SourcesOf_NoSourceOptionsOrConnectionDoNotCount(t *testing.T) {
	jobs := []db_queries.ListJobSourcesOfInstanceRow{
		syncJob(t, accountA, nil),
		syncJob(t, accountA, &pg_models.JobSourceOptions{}),
		syncJob(t, accountA, postgres("")),
		syncJob(t, accountA, mysql(""), "shop"),
	}
	require.Empty(t, SourcesOf(jobs))
}
