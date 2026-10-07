package preflight_activity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"testing"

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
)

// oneDatabase is a connection manager that hands out the same database for every connection,
// or fails to open any.
type oneDatabase struct {
	db  husonym_benthos_sql.SqlDbtx
	err error
}

func (m *oneDatabase) GetConnection(
	connectionmanager.SessionInterface, connectionmanager.ConnectionInput, *slog.Logger,
) (husonym_benthos_sql.SqlDbtx, error) {
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
			a := &Activity{connclient: holdsConnection(t, tt.connection), sqlconnmanager: &oneDatabase{db: db}}

			require.Equal(t, tt.want, a.sourceVersionMajor(ctx, session, jobReading(tt.source), logger))
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}

	// Nothing of what follows stops a run: the version is only left out.
	t.Run("a source that does not answer", func(t *testing.T) {
		db, sqlMock, err := sqlmock.New()
		require.NoError(t, err)
		sqlMock.ExpectQuery("server_version_num").WillReturnError(errors.New("connection lost"))
		a := &Activity{connclient: holdsConnection(t, postgresConnection), sqlconnmanager: &oneDatabase{db: db}}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(postgresSource), logger))
	})

	t.Run("a source that cannot be opened", func(t *testing.T) {
		a := &Activity{
			connclient:     holdsConnection(t, postgresConnection),
			sqlconnmanager: &oneDatabase{err: errors.New("no route to host")},
		}

		require.Empty(t, a.sourceVersionMajor(ctx, session, jobReading(postgresSource), logger))
	})

	t.Run("a connection the API does not give", func(t *testing.T) {
		connclient := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
		connclient.On("GetConnection", mock.Anything, mock.Anything).
			Return(nil, connect.NewError(connect.CodeUnavailable, errors.New("the API is away"))).Once()
		a := &Activity{connclient: connclient, sqlconnmanager: &oneDatabase{}}

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
