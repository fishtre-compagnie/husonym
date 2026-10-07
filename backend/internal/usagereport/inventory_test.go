package usagereport

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// leak is in every name the tests give, and must be in nothing the inventory says or logs.
const leak = "zzleak"

const (
	jobOne   = "00000000-0000-0000-0000-0000000000a1"
	jobTwo   = "00000000-0000-0000-0000-0000000000a2"
	jobThree = "00000000-0000-0000-0000-0000000000a3"

	connectionOne   = "00000000-0000-0000-0000-0000000000c1"
	connectionTwo   = "00000000-0000-0000-0000-0000000000c2"
	connectionThree = "00000000-0000-0000-0000-0000000000c3"
	connectionFour  = "00000000-0000-0000-0000-0000000000c4"
)

func uuidOf(t testing.TB, id string) pgtype.UUID {
	t.Helper()
	value, err := husonymdb.ToUuid(id)
	require.NoError(t, err)
	return value
}

func stored(t testing.TB, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

// logged gives a context whose logger writes to the buffer returned.
func logged(t testing.TB) (context.Context, *bytes.Buffer) {
	t.Helper()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	return logger_interceptor.SetLoggerContext(t.Context(), logger), output
}

func configOf(source mgmtv1alpha1.TransformerSource) *mgmtv1alpha1.TransformerConfig {
	switch source {
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL:
		return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
			GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
		}}
	case mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH:
		return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
			PassthroughConfig: &mgmtv1alpha1.Passthrough{},
		}}
	}
	panic("no config for this source in the tests")
}

func userDefined(id string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: id},
	}}
}

// piiText is a PII-text transformer that hands what it finds to the given transformer.
func piiText(anonymizer *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
		TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{
			DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
				Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: anonymizer},
			}},
		},
	}}
}

func mapping(t testing.TB, schema, table, column string, config *mgmtv1alpha1.TransformerConfig) *pg_models.JobMapping {
	t.Helper()
	model := &pg_models.JobMapping{}
	require.NoError(t, model.FromDto(&mgmtv1alpha1.JobMapping{
		Schema: schema, Table: table, Column: column,
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: config},
	}))
	return model
}

func passthrough(t testing.TB, schema, table, column string) *pg_models.JobMapping {
	t.Helper()
	return mapping(t, schema, table, column, configOf(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH))
}

func postgresFrom(connectionId string, where ...string) *pg_models.JobSourceOptions {
	tables := make([]*pg_models.PostgresSourceTableOption, 0, len(where))
	for i := range where {
		tables = append(tables, &pg_models.PostgresSourceTableOption{Table: leak + "_table", WhereClause: &where[i]})
	}
	return &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{
		ConnectionId: connectionId,
		Schemas:      []*pg_models.PostgresSourceSchemaOption{{Schema: leak + "_schema", Tables: tables}},
	}}
}

func piiDetect(t testing.TB) []byte {
	t.Helper()
	return stored(t, &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{}},
	})
}

type jobRow = db_queries.ListJobsOfInstanceForUsageRow

func jobOf(t testing.TB, id string, options *pg_models.JobSourceOptions, mappings ...*pg_models.JobMapping) jobRow {
	t.Helper()
	if mappings == nil {
		mappings = []*pg_models.JobMapping{}
	}
	return jobRow{
		ID:                uuidOf(t, id),
		ConnectionOptions: stored(t, options),
		JobtypeConfig:     []byte("{}"),
		Mappings:          stored(t, mappings),
	}
}

func scheduled(row jobRow, cron string) jobRow {
	row.CronSchedule = pgtype.Text{String: cron, Valid: true}
	return row
}

func Test_readJobs_CountsByKindAndBySchedule(t *testing.T) {
	generation := &pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}}
	byModel := &pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{AiConnectionId: connectionOne}}
	detection := jobOf(t, jobOne, postgresFrom(connectionOne))
	detection.JobtypeConfig = piiDetect(t)

	read := readJobs(t.Context(), []jobRow{
		scheduled(jobOf(t, jobOne, postgresFrom(connectionOne)), "0 3 * * *"),
		scheduled(jobOf(t, jobTwo, postgresFrom(connectionOne)), "0 4 * * *"),
		// The schedule of a job that was given none, and no schedule at all.
		scheduled(jobOf(t, jobOne, postgresFrom(connectionOne)), job_util.UnscheduledCron),
		jobOf(t, jobOne, postgresFrom(connectionOne)),
		jobOf(t, jobOne, generation),
		scheduled(jobOf(t, jobOne, byModel), "0 5 * * *"),
		detection,
	})

	require.Equal(t, []telemetry.JobKindCount{
		{Kind: "ai_generate", Scheduled: true, Count: 1},
		{Kind: "generate", Scheduled: false, Count: 1},
		{Kind: "pii_detect", Scheduled: false, Count: 1},
		{Kind: "sync", Scheduled: false, Count: 2},
		{Kind: "sync", Scheduled: true, Count: 2},
	}, read.jobs.ByKind)
}

func Test_readJobs_SumsTheTablesAndTheColumnsOfEveryJob(t *testing.T) {
	read := readJobs(t.Context(), []jobRow{
		jobOf(t, jobOne, postgresFrom(connectionOne),
			passthrough(t, "s", "a", "c1"), passthrough(t, "s", "a", "c2"), passthrough(t, "s", "b", "c1"),
			// The same table name in another schema is another table.
			passthrough(t, "other", "a", "c1"),
		),
		// A table two jobs map is counted for each: nothing says they read the same database.
		jobOf(t, jobTwo, postgresFrom(connectionTwo), passthrough(t, "s", "a", "c1")),
	})

	require.Equal(t, 4, read.jobs.Tables)
	require.Equal(t, 5, read.jobs.Columns)
}

func Test_readJobs_CountsTheJobsWithAWhereClause(t *testing.T) {
	read := readJobs(t.Context(), []jobRow{
		jobOf(t, jobOne, postgresFrom(connectionOne, "", leak+" = 1")),
		// A clause of spaces subsets nothing, as the license gate has it.
		jobOf(t, jobTwo, postgresFrom(connectionOne, "  ")),
		jobOf(t, jobThree, postgresFrom(connectionOne)),
	})

	require.Equal(t, 1, read.jobs.WithSubset)
}

func Test_readJobs_CountsTheColumnsOfEachTransformer(t *testing.T) {
	email := configOf(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL)
	read := readJobs(t.Context(), []jobRow{
		jobOf(t, jobOne, postgresFrom(connectionOne),
			mapping(t, "s", "t", "a", email),
			mapping(t, "s", "t", "b", email),
			passthrough(t, "s", "t", "c"),
			mapping(t, "s", "t", "d", userDefined(leak)),
			// No transformer at all: a column, and nothing to count it under.
			mapping(t, "s", "t", "e", &mgmtv1alpha1.TransformerConfig{}),
		),
		jobOf(t, jobTwo, postgresFrom(connectionOne),
			mapping(t, "s", "t", "a", email),
			// What a PII text hands its findings to runs on the column too.
			mapping(t, "s", "t", "b", piiText(email)),
			mapping(t, "s", "t", "c", piiText(userDefined(leak))),
		),
	})

	require.Equal(t, []telemetry.TransformerColumns{
		{Name: "generate_email", Columns: 4},
		{Name: "passthrough", Columns: 1},
		{Name: "transform_pii_text", Columns: 2},
	}, read.system)
	require.Equal(t, 2, read.userDefinedColumns)
	require.Equal(t, 8, read.jobs.Columns)
}

// A transformer a column runs twice, itself and as what its PII text hands over to, is one
// column of that transformer.
func Test_readJobs_CountsAColumnOncePerTransformer(t *testing.T) {
	piiTwice := piiText(piiText(nil))
	read := readJobs(t.Context(), []jobRow{
		jobOf(t, jobOne, postgresFrom(connectionOne), mapping(t, "s", "t", "a", piiTwice)),
	})

	require.Equal(t, []telemetry.TransformerColumns{{Name: "transform_pii_text", Columns: 1}}, read.system)
}

func Test_readJobs_LeavesOutAJobThatCannotBeRead(t *testing.T) {
	for name, damage := range map[string]func(row *jobRow){
		"mappings that are not JSON":      func(row *jobRow) { row.Mappings = []byte(`[{"schema": "` + leak) },
		"mappings that are not a list":    func(row *jobRow) { row.Mappings = []byte(`{"` + leak + `": 1}`) },
		"a mapping of another shape":      func(row *jobRow) { row.Mappings = []byte(`[{"schema": {"` + leak + `": 1}}]`) },
		"a mapping that is null":          func(row *jobRow) { row.Mappings = []byte(`[null]`) },
		"a mapping without a transformer": func(row *jobRow) { row.Mappings = []byte(`[{"schema": "` + leak + `"}]`) },
		"source options that are null":    func(row *jobRow) { row.ConnectionOptions = []byte(`null`) },
		"source options of no engine":     func(row *jobRow) { row.ConnectionOptions = []byte(`{"` + leak + `": 1}`) },
		"source options of another shape": func(row *jobRow) { row.ConnectionOptions = []byte(`{"postgresOptions": "` + leak + `"}`) },
		"a type that cannot be read":      func(row *jobRow) { row.JobtypeConfig = []byte(`{"` + leak + `": 1}`) },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, output := logged(t)
			damaged := jobOf(t, jobTwo, postgresFrom(connectionTwo, "x = 1"), passthrough(t, "s", "t", "a"))
			damage(&damaged)

			read := readJobs(ctx, []jobRow{
				jobOf(t, jobOne, postgresFrom(connectionOne), passthrough(t, "s", "t", "a")),
				damaged,
				jobOf(t, jobThree, postgresFrom(connectionOne), passthrough(t, "s", "t", "a")),
			})

			require.Equal(t, []telemetry.JobKindCount{{Kind: "sync", Count: 2}}, read.jobs.ByKind)
			require.Equal(t, 2, read.jobs.Tables)
			require.Equal(t, 2, read.jobs.Columns)
			require.Zero(t, read.jobs.WithSubset)
			require.Equal(t, map[string]string{jobOne: connectionOne, jobThree: connectionOne}, read.sourceOfJob)
			require.Equal(t, map[string]bool{connectionOne: true}, read.sourceConnections)

			// The job is named by its id, and nothing of what it holds is written.
			require.Contains(t, output.String(), jobTwo)
			require.Contains(t, output.String(), `"level":"WARN"`)
			require.NotContains(t, output.String(), leak)
		})
	}
}

func Test_readJobs_TellsTheConnectionsEachSourceNames(t *testing.T) {
	foreignKeys := connectionThree
	for name, tc := range map[string]struct {
		options *pg_models.JobSourceOptions
		source  string
		all     []string
	}{
		"postgres": {postgresFrom(connectionOne), connectionOne, []string{connectionOne}},
		"mysql": {
			&pg_models.JobSourceOptions{MysqlOptions: &pg_models.MysqlSourceOptions{ConnectionId: connectionOne}},
			connectionOne, []string{connectionOne},
		},
		"mssql": {
			&pg_models.JobSourceOptions{MssqlOptions: &pg_models.MssqlSourceOptions{ConnectionId: connectionOne}},
			connectionOne, []string{connectionOne},
		},
		"mongodb": {
			&pg_models.JobSourceOptions{MongoDbOptions: &pg_models.MongoDbSourceOptions{ConnectionId: connectionOne}},
			connectionOne, []string{connectionOne},
		},
		"dynamodb": {
			&pg_models.JobSourceOptions{DynamoDBOptions: &pg_models.DynamoDBSourceOptions{ConnectionId: connectionOne}},
			connectionOne, []string{connectionOne},
		},
		"a generation that reads nothing": {
			&pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}}, "", nil,
		},
		"a generation that reads the shape of a database": {
			&pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{FkSourceConnectionId: &foreignKeys}},
			connectionThree, []string{connectionThree},
		},
		"a generation by a model": {
			&pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{AiConnectionId: connectionTwo}},
			connectionTwo, []string{connectionTwo},
		},
		// The database is the source of the job; the model is a connection its source names too.
		"a generation by a model that reads the shape of a database": {
			&pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{
				AiConnectionId: connectionTwo, FkSourceConnectionId: &foreignKeys,
			}},
			connectionThree, []string{connectionTwo, connectionThree},
		},
	} {
		t.Run(name, func(t *testing.T) {
			read := readJobs(t.Context(), []jobRow{jobOf(t, jobOne, tc.options)})

			wantSource := map[string]string{}
			if tc.source != "" {
				wantSource[jobOne] = tc.source
			}
			wantAll := map[string]bool{}
			for _, id := range tc.all {
				wantAll[id] = true
			}
			require.Equal(t, wantSource, read.sourceOfJob)
			require.Equal(t, wantAll, read.sourceConnections)
		})
	}
}

type connectionRow = db_queries.ListConnectionsOfInstanceRow

func connectionOf(t testing.TB, id string, config *pg_models.ConnectionConfig) connectionRow {
	t.Helper()
	return connectionRow{ID: uuidOf(t, id), ConnectionConfig: stored(t, config)}
}

func postgresConnection() *pg_models.ConnectionConfig {
	return &pg_models.ConnectionConfig{PgConfig: &pg_models.PostgresConnectionConfig{
		Connection: &pg_models.PostgresConnection{Host: leak + ".example.com", Name: leak, User: leak, Pass: leak},
	}}
}

func mysqlConnection() *pg_models.ConnectionConfig {
	return &pg_models.ConnectionConfig{MysqlConfig: &pg_models.MysqlConnectionConfig{
		Connection: &pg_models.MysqlConnection{Host: leak + ".example.com", Name: leak, User: leak, Pass: leak},
	}}
}

func Test_connectionTypes_NamesEachTypeAndLeavesOutWhatCannotBeRead(t *testing.T) {
	ctx, output := logged(t)

	types := connectionTypes(ctx, []connectionRow{
		connectionOf(t, connectionOne, postgresConnection()),
		connectionOf(t, connectionTwo, mysqlConnection()),
		// A type the report has no name for.
		connectionOf(t, connectionThree, &pg_models.ConnectionConfig{
			LocalDirectoryConfig: &pg_models.LocalDirectoryConnectionConfig{Path: "/" + leak},
		}),
		{ID: uuidOf(t, connectionFour), ConnectionConfig: []byte(`{"pgConfig": "` + leak + `"}`)},
	})

	require.Equal(t, map[string]string{
		connectionOne: "postgres", connectionTwo: "mysql", connectionThree: "other",
	}, types)
	require.Contains(t, output.String(), connectionFour)
	require.Contains(t, output.String(), `"level":"WARN"`)
	require.NotContains(t, output.String(), leak)
}

func Test_countConnections_CountsByTypeAndRole(t *testing.T) {
	types := map[string]string{
		connectionOne: "postgres", connectionTwo: "postgres", connectionThree: "mysql", connectionFour: "aws-s3",
	}

	counts := countConnections(
		types,
		// One is a source and a destination: it counts under both roles.
		map[string]bool{connectionOne: true, connectionTwo: true, "gone": true},
		map[string]bool{connectionOne: true, connectionThree: true},
	)

	// Four has no role, and is not counted; a connection that is gone has no type to count under.
	require.Equal(t, []telemetry.ConnectionCount{
		{Type: "mysql", Role: "destination", Count: 1},
		{Type: "postgres", Role: "destination", Count: 1},
		{Type: "postgres", Role: "source", Count: 2},
	}, counts)
}

func Test_sourceTypes_LeavesOutAJobWhoseConnectionIsGone(t *testing.T) {
	types := map[string]string{connectionOne: "postgres"}

	require.Equal(t,
		map[string]string{jobOne: "postgres"},
		sourceTypes(map[string]string{jobOne: connectionOne, jobTwo: connectionTwo}, types),
	)
}

func Test_columnTypes_SumsByFamily(t *testing.T) {
	families := columnTypes([]db_queries.CountSourceColumnTypesOfInstanceRow{
		{DataType: "integer", Columns: 3},
		{DataType: "int4", Columns: 2},
		{DataType: "character varying(255)", Columns: 7},
		// A type of the customer, by the name the catalog gives it or by its own, and no type.
		{DataType: "USER-DEFINED", Columns: 1},
		{DataType: leak + "_status", Columns: 4},
		{DataType: "", Columns: 2},
	})

	require.Equal(t, []telemetry.ColumnTypeCount{
		{Family: "integer", Columns: 5},
		{Family: "other", Columns: 7},
		{Family: "text", Columns: 7},
	}, families)
}

func Test_featureUses_HasOneEntryPerFeatureOfTheLicense(t *testing.T) {
	uses := featureUses(map[license.Feature]bool{license.FeatureJobHooks: true, license.FeatureSso: true})

	require.Len(t, uses, len(license.AllFeatures()))
	for i, feature := range license.AllFeatures() {
		require.Equal(t, string(feature), uses[i].Name)
		require.Equal(t, feature == license.FeatureJobHooks || feature == license.FeatureSso, uses[i].InUse, feature)
	}
	require.ElementsMatch(t, telemetry.Features(), namesOf(uses))
}

func namesOf(uses []telemetry.FeatureUse) []string {
	names := make([]string, 0, len(uses))
	for _, use := range uses {
		names = append(names, use.Name)
	}
	return names
}

func Test_countRoles_CountsEachMemberUnderTheRoleItHolds(t *testing.T) {
	admin, developer, without := rbac.NewUser("admin"), rbac.NewUser("developer"), rbac.NewUser("without")
	counts := map[string]int{}

	countRoles(counts, []rbac.User{admin, developer, without}, map[rbac.User]mgmtv1alpha1.AccountRole{
		admin:     mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
		developer: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER,
	})
	// The members of a second account add to the same counts.
	countRoles(counts, []rbac.User{admin}, map[rbac.User]mgmtv1alpha1.AccountRole{
		admin: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	})

	require.Equal(t, []telemetry.RoleCount{
		{Role: "admin", Count: 2},
		{Role: "job_developer", Count: 1},
		{Role: "none", Count: 1},
	}, roleCounts(counts))
}
