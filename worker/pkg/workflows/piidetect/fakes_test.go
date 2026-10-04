package piidetect

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
)

// fakeJobs is the job service of the API as the activities see it: one job and the run
// contexts.
type fakeJobs struct {
	mu       sync.Mutex
	job      *mgmtv1alpha1.Job
	contexts map[string][]byte
	keys     []*mgmtv1alpha1.RunContextKey // of the contexts that were set, in order

	getJobErr error
	getErr    error
	setErr    error
}

func contextKey(key *mgmtv1alpha1.RunContextKey) string {
	return key.GetAccountId() + "|" + key.GetJobRunId() + "|" + key.GetExternalId()
}

func (f *fakeJobs) GetJob(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetJobRequest],
) (*connect.Response[mgmtv1alpha1.GetJobResponse], error) {
	if f.getJobErr != nil {
		return nil, f.getJobErr
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{Job: f.job}), nil
}

func (f *fakeJobs) GetRunContext(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetRunContextRequest],
) (*connect.Response[mgmtv1alpha1.GetRunContextResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	value, ok := f.contexts[contextKey(req.Msg.GetId())]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no run context under this key"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetRunContextResponse{Value: value}), nil
}

func (f *fakeJobs) SetRunContext(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.SetRunContextRequest],
) (*connect.Response[mgmtv1alpha1.SetRunContextResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return nil, f.setErr
	}
	if f.contexts == nil {
		f.contexts = map[string][]byte{}
	}
	f.contexts[contextKey(req.Msg.GetId())] = req.Msg.GetValue()
	f.keys = append(f.keys, req.Msg.GetId())
	return connect.NewResponse(&mgmtv1alpha1.SetRunContextResponse{}), nil
}

// fakeConnections is the connection service of the API: connections by their id.
type fakeConnections map[string]*mgmtv1alpha1.Connection

func (f fakeConnections) GetConnection(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetConnectionRequest],
) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error) {
	connection, ok := f[req.Msg.GetId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such connection"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: connection}), nil
}

var (
	postgresConnection = &mgmtv1alpha1.Connection{
		Id: "connection-1",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}
	mongoConnection = &mgmtv1alpha1.Connection{
		Id: "connection-mongo",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_MongoConfig{MongoConfig: &mgmtv1alpha1.MongoConnectionConfig{}},
		},
	}
	connections = fakeConnections{"connection-1": postgresConnection, "connection-mongo": mongoConnection}
)

// source returns the reader of the data of the connections, and its builder.
func source(t *testing.T) (*connectiondata.MockConnectionDataBuilder, *connectiondata.MockConnectionDataService) {
	t.Helper()
	builder := connectiondata.NewMockConnectionDataBuilder(t)
	data := connectiondata.NewMockConnectionDataService(t)
	builder.EXPECT().NewDataConnection(mock.Anything, mock.Anything).Return(data, nil).Maybe()
	return builder, data
}

// sends makes the sampling of the source send rows, then end on err.
func sends(data *connectiondata.MockConnectionDataService, rows []map[string]any, err error) {
	data.EXPECT().SampleData(mock.Anything, mock.Anything, "public", "users", uint(200)).
		RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _ string, _ uint) error {
			for _, row := range rows {
				var encoded bytes.Buffer
				if encodeErr := gob.NewEncoder(&encoded).Encode(row); encodeErr != nil {
					return encodeErr
				}
				if sendErr := stream.Send(&mgmtv1alpha1.GetConnectionDataStreamResponse{RowBytes: encoded.Bytes()}); sendErr != nil {
					return sendErr
				}
			}
			return err
		})
}

func column(schema, table, name, dataType string) *mgmtv1alpha1.DatabaseColumn {
	return &mgmtv1alpha1.DatabaseColumn{Schema: schema, Table: table, Column: name, DataType: dataType, IsNullable: "YES"}
}

// logLines keeps every line the activities log, with its fields.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) add(level, msg string, keyvals []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, level+" "+msg+" "+fmt.Sprint(keyvals...))
}

func (l *logLines) Debug(msg string, keyvals ...any) { l.add("DEBUG", msg, keyvals) }
func (l *logLines) Info(msg string, keyvals ...any)  { l.add("INFO", msg, keyvals) }
func (l *logLines) Warn(msg string, keyvals ...any)  { l.add("WARN", msg, keyvals) }
func (l *logLines) Error(msg string, keyvals ...any) { l.add("ERROR", msg, keyvals) }

func (l *logLines) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// activityRun runs activities in a test environment and keeps what they log and the
// details of their heartbeats.
type activityRun struct {
	env        *testsuite.TestActivityEnvironment
	logs       *logLines
	mu         sync.Mutex
	heartbeats []string
}

func newActivityRun(t *testing.T, activities *Activities) *activityRun {
	t.Helper()
	run := &activityRun{logs: &logLines{}}
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(run.logs)
	run.env = ts.NewTestActivityEnvironment()
	Register(activityRegistry{run.env}, nil, activities, Config{})
	run.env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
		var raw any
		_ = details.Get(&raw)
		run.mu.Lock()
		defer run.mu.Unlock()
		run.heartbeats = append(run.heartbeats, fmt.Sprint(raw))
	})
	return run
}

// activityRegistry registers the activities of the package in an environment that runs
// activities only.
type activityRegistry struct {
	env *testsuite.TestActivityEnvironment
}

func (r activityRegistry) RegisterWorkflow(any)   {}
func (r activityRegistry) RegisterActivity(a any) { r.env.RegisterActivity(a) }

// execute runs an activity and decodes its result. It returns the serialized result too.
func execute[T any](t *testing.T, run *activityRun, fn, request any) (*T, string, error) {
	t.Helper()
	value, err := run.env.ExecuteActivity(fn, request)
	if err != nil {
		return nil, "", err
	}
	var response T
	require.NoError(t, value.Get(&response))
	payload, err := converter.GetDefaultDataConverter().ToPayload(&response)
	require.NoError(t, err)
	return &response, string(payload.GetData()), nil
}
