package preflight_activity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/DATA-DOG/go-sqlmock"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

// oneDatabase is a connection manager that hands out the same database for every connection,
// or fails to open any.
type oneDatabase struct {
	db  husonym_benthos_sql.SqlDbtx
	err error
	// block makes the opening wait until it is closed.
	block chan struct{}
	// panics makes the opening panic.
	panics bool
}

func (m *oneDatabase) GetConnection(
	connectionmanager.SessionInterface, connectionmanager.ConnectionInput, *slog.Logger,
) (husonym_benthos_sql.SqlDbtx, error) {
	if m.panics {
		panic("a value that must not be logged")
	}
	if m.block != nil {
		<-m.block
	}
	return m.db, m.err
}
func (*oneDatabase) ReleaseSession(connectionmanager.SessionInterface, *slog.Logger) bool {
	return true
}
func (*oneDatabase) Shutdown(*slog.Logger) {}
func (*oneDatabase) Reaper(*slog.Logger)   {}

func jobReading(options *mgmtv1alpha1.JobSourceOptions) *mgmtv1alpha1.Job {
	return &mgmtv1alpha1.Job{Source: &mgmtv1alpha1.JobSource{Options: options}}
}

var (
	postgresSource = &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{ConnectionId: "source"},
	}}
	mysqlSource = &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
		Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{ConnectionId: "source"},
	}}
	postgresConnection = &mgmtv1alpha1.ConnectionConfig{
		Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
	}
	mysqlConnection = &mgmtv1alpha1.ConnectionConfig{
		Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{}},
	}
)

// holdsConnection makes the API hold the source connection of the test.
func holdsConnection(t *testing.T, config *mgmtv1alpha1.ConnectionConfig) *mgmtv1alpha1connect.MockConnectionServiceClient {
	t.Helper()
	connclient := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
	connclient.On("GetConnection", mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
			Connection: &mgmtv1alpha1.Connection{Id: "source", Name: "prod", ConnectionConfig: config},
		}), nil).Once()
	return connclient
}

func Test_sourceVersionMajor(t *testing.T) {
	ctx := context.Background()
	session := connectionmanager.NewUniqueSession()
	logger := testutil.GetTestLogger(t)

	for name, tt := range map[string]struct {
		source     *mgmtv1alpha1.JobSourceOptions
		connection *mgmtv1alpha1.ConnectionConfig
		query      string
		answer     any
		want       string
	}{
		"postgres": {postgresSource, postgresConnection, "server_version_num", 160004, "16"},
		"mysql":    {mysqlSource, mysqlConnection, "SELECT VERSION()", "8.0.36-log", "8.0"},
	} {
		t.Run(name, func(t *testing.T) {
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			sqlMock.ExpectQuery(regexp.QuoteMeta(tt.query)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(tt.answer))
			a := &Activity{
				connclient: holdsConnection(t, tt.connection), sqlconnmanager: &oneDatabase{db: db},
				sourceVersionTimeout: sourceVersionTimeout,
			}

			require.Equal(t, tt.want, a.sourceVersionMajor(ctx, session, jobReading(tt.source), logger))
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}

	// Nothing of what follows stops a run: the version is only left out.
	t.Run("a source that does not answer", func(t *testing.T) {
		db, sqlMock, err := sqlmock.New()
		require.NoError(t, err)
		sqlMock.ExpectQuery("server_version_num").WillReturnError(errors.New("connection lost"))
		a := &Activity{
			connclient: holdsConnection(t, postgresConnection), sqlconnmanager: &oneDatabase{db: db},
			sourceVersionTimeout: sourceVersionTimeout,
		}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(postgresSource), logger))
	})

	t.Run("a source that cannot be opened", func(t *testing.T) {
		a := &Activity{
			connclient:           holdsConnection(t, postgresConnection),
			sqlconnmanager:       &oneDatabase{err: errors.New("no route to host")},
			sourceVersionTimeout: sourceVersionTimeout,
		}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(postgresSource), logger))
	})

	t.Run("a connection the API does not give", func(t *testing.T) {
		connclient := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
		connclient.On("GetConnection", mock.Anything, mock.Anything).
			Return(nil, connect.NewError(connect.CodeUnavailable, errors.New("the API is away"))).Once()
		a := &Activity{connclient: connclient, sqlconnmanager: &oneDatabase{}, sourceVersionTimeout: sourceVersionTimeout}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(postgresSource), logger))
	})

	t.Run("a source that is no MySQL nor PostgreSQL is not asked", func(t *testing.T) {
		a := &Activity{
			connclient:     mgmtv1alpha1connect.NewMockConnectionServiceClient(t),
			sqlconnmanager: &oneDatabase{},
		}
		mongo := &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mongodb{
			Mongodb: &mgmtv1alpha1.MongoDBSourceConnectionOptions{ConnectionId: "source"},
		}}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(mongo), logger))
	})
}

// The serialized form of the response is in the histories of the runs: a version that was
// not read is left out.
func Test_RunPreflightResponse_LeavesOutAVersionNotRead(t *testing.T) {
	payload, err := json.Marshal(&RunPreflightResponse{})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(payload))
}

// A destination is checked on the columns the run writes into it. Athanor leaves a column
// mapped to GenerateDefault out of its INSERT: a destination without it is fine. Benthos
// writes DEFAULT into it: the destination must have it.
func Test_writtenTables(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "users", Column: "id", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
				PassthroughConfig: &mgmtv1alpha1.Passthrough{},
			}},
		}},
		{Schema: "public", Table: "users", Column: "created_at", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
				GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
			}},
		}},
	}}
	tables := []*connectionchecks.Table{{Schema: "public", Table: "users", Columns: []string{"id", "created_at"}}}
	a := &Activity{}

	athanor, err := a.writtenTables(context.Background(), job, tables, true)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, athanor[0].Columns)
	require.Equal(t, []string{"id", "created_at"}, tables[0].Columns, "the source is checked on the tables as given")

	benthos, err := a.writtenTables(context.Background(), job, tables, false)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "created_at"}, benthos[0].Columns)
}

// holdsJob makes the API hold a job that reads a PostgreSQL source and writes nowhere.
func holdsJob(t *testing.T) *mgmtv1alpha1connect.MockJobServiceClient {
	t.Helper()
	jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
	jobclient.On("GetJob", mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.GetJobResponse{Job: jobReading(postgresSource)}), nil).Once()
	return jobclient
}

func activityEnvironment(t *testing.T, a *Activity) *testsuite.TestActivityEnvironment {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	suite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(a)
	return env
}

func runPreflight(t *testing.T, a *Activity) *RunPreflightResponse {
	t.Helper()
	value, err := activityEnvironment(t, a).ExecuteActivity(a.RunPreflight, &RunPreflightRequest{JobId: "job"})
	require.NoError(t, err)
	response := &RunPreflightResponse{}
	require.NoError(t, value.Get(response))
	return response
}

func Test_RunPreflight_TellsTheVersionOfTheSource(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	sqlMock.ExpectQuery("server_version_num").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(160004))
	jobclient := holdsJob(t)
	jobclient.On("SetRunContext", mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.SetRunContextResponse{}), nil).Once()
	a := &Activity{
		jobclient: jobclient, connclient: holdsConnection(t, postgresConnection),
		sqlconnmanager: &oneDatabase{db: db}, sourceVersionTimeout: sourceVersionTimeout,
	}

	require.Equal(t, "16", runPreflight(t, a).SourceVersionMajor)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// The version is no part of what a run needs: a source that does not answer the question
// leaves the check as it was, and holds it for the bound of the reading only.
func Test_RunPreflight_AVersionThatDoesNotComeLeavesTheCheckPassed(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	sqlMock.ExpectQuery("server_version_num").WillDelayFor(time.Hour).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(160004))
	jobclient := holdsJob(t)
	jobclient.On("SetRunContext", mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.SetRunContextResponse{}), nil).Once()
	a := &Activity{
		jobclient: jobclient, connclient: holdsConnection(t, postgresConnection),
		sqlconnmanager: &oneDatabase{db: db}, sourceVersionTimeout: 20 * time.Millisecond,
	}

	started := time.Now()
	require.Empty(t, runPreflight(t, a).SourceVersionMajor)
	require.Less(t, time.Since(started), 30*time.Second)
}

// Opening a connection takes no context: the reading is still left at its bound.
func Test_sourceVersionMajor_ASourceThatNeverOpensIsLeftAtTheBound(t *testing.T) {
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	a := &Activity{
		connclient:           holdsConnection(t, postgresConnection),
		sqlconnmanager:       &oneDatabase{err: errors.New("closed"), block: never},
		sourceVersionTimeout: 20 * time.Millisecond,
	}

	done := make(chan string, 1)
	go func() {
		done <- a.sourceVersionMajor(context.Background(), connectionmanager.NewUniqueSession(),
			jobReading(postgresSource), testutil.GetConcurrentTestLogger(t))
	}()
	select {
	case major := <-done:
		require.Empty(t, major)
	case <-time.After(30 * time.Second):
		t.Fatal("the reading of the version held its caller")
	}
}

func Test_sourceVersionMajor_AReadingThatPanicsLeavesTheVersionOut(t *testing.T) {
	a := &Activity{
		connclient:           holdsConnection(t, postgresConnection),
		sqlconnmanager:       &oneDatabase{panics: true},
		sourceVersionTimeout: sourceVersionTimeout,
	}

	require.Empty(t, a.sourceVersionMajor(context.Background(), connectionmanager.NewUniqueSession(),
		jobReading(postgresSource), testutil.GetTestLogger(t)))
}

// The check asked through the API tells no version: it opens nothing to read one.
func Test_CheckPreflight_DoesNotReadTheVersionOfTheSource(t *testing.T) {
	a := &Activity{
		jobclient: holdsJob(t),
		// Neither is expected to be asked for anything.
		connclient:     mgmtv1alpha1connect.NewMockConnectionServiceClient(t),
		sqlconnmanager: &oneDatabase{panics: true},
	}

	value, err := activityEnvironment(t, a).ExecuteActivity(a.CheckPreflight, &CheckPreflightRequest{JobId: "job"})
	require.NoError(t, err)
	response := &CheckPreflightResponse{}
	require.NoError(t, value.Get(response))
	require.NotNil(t, response.Report)
}
