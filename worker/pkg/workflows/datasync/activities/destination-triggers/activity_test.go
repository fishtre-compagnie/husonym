package destinationtriggers_activity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/connection-manager/providers/sqlprovider"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

// The triggers to put back are read from a run context, and statements are run on the
// connection it names: one of another account than the run's is refused before any statement,
// and stays recorded.
func Test_RestoreTriggers_RefusesAConnectionOfAnotherAccount(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	accountId := uuid.NewString()
	foreignConnectionId := uuid.NewString()
	recorded, err := json.Marshal([]*suspended{{
		ConnectionID: foreignConnectionId,
		Triggers:     []*Trigger{{Schema: "public", Table: "users", Name: "audit", Restore: []string{"DROP TABLE users"}}},
	}})
	require.NoError(t, err)

	var stored []byte
	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.JobServiceGetRunContextProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.JobServiceGetRunContextProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetRunContextRequest]) (*connect.Response[mgmtv1alpha1.GetRunContextResponse], error) {
			return connect.NewResponse(&mgmtv1alpha1.GetRunContextResponse{Value: recorded}), nil
		},
	))
	mux.Handle(mgmtv1alpha1connect.JobServiceSetRunContextProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.JobServiceSetRunContextProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.SetRunContextRequest]) (*connect.Response[mgmtv1alpha1.SetRunContextResponse], error) {
			stored = r.Msg.GetValue()
			return connect.NewResponse(&mgmtv1alpha1.SetRunContextResponse{}), nil
		},
	))
	mux.Handle(mgmtv1alpha1connect.ConnectionServiceGetConnectionProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.ConnectionServiceGetConnectionProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetConnectionRequest]) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error) {
			return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
				Connection: &mgmtv1alpha1.Connection{Id: r.Msg.GetId(), AccountId: uuid.NewString()},
			}), nil
		},
	))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.Start()
	t.Cleanup(srv.Close)

	activity := New(
		mgmtv1alpha1connect.NewJobServiceClient(srv.Client(), srv.URL),
		mgmtv1alpha1connect.NewConnectionServiceClient(srv.Client(), srv.URL),
		nil,
		connectionmanager.NewConnectionManager(sqlprovider.NewProvider(&sqlconnect.SqlOpenConnector{})),
	)
	env.RegisterActivity(activity)

	_, err = env.ExecuteActivity(activity.RestoreTriggers, &RestoreTriggersRequest{JobId: uuid.NewString(), AccountId: accountId})
	require.ErrorContains(t, err, "does not belong to the account of the run")
	require.JSONEq(t, string(recorded), string(stored), "what was not put back stays recorded")
}
