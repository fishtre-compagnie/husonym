package integrationtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	accounthook_events "github.com/fishtre-compagnie/husonym/internal/ee/events"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	accounthook_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/workflow"
	accounthook_workflow_register "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/workflow/register"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

func Test_ProcessAccountHookWorkflow(t *testing.T) {
	t.Parallel()
	ok := testutil.ShouldRunWorkerIntegrationTest()
	if !ok {
		return
	}
	ctx := context.Background()

	husonymApi, err := tchusonymapi.NewHusonymApiTestClient(
		ctx,
		t,
		tchusonymapi.WithMigrationsDirectory(husonymDbMigrationsPath),
	)
	if err != nil {
		t.Fatal(err)
	}

	tchusonymapi.SetUser(
		ctx,
		t,
		husonymApi.OSSAuthenticatedLicensedClients.Users(tchusonymapi.WithUserId("123")),
	)
	accountId := tchusonymapi.CreateTeamAccount(
		ctx,
		t,
		husonymApi.OSSAuthenticatedLicensedClients.Users(tchusonymapi.WithUserId("123")),
		uuid.NewString(),
	)

	mux := http.NewServeMux()
	webhookCount := 0
	mux.Handle("/webhook", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCount++
		w.WriteHeader(http.StatusOK)
	}))
	srv := startHTTPServer(t, mux)

	hookResp, err := husonymApi.OSSAuthenticatedLicensedClients.AccountHooks(tchusonymapi.WithUserId("123")).
		CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
			AccountId: accountId,
			Hook: &mgmtv1alpha1.NewAccountHook{
				Name:        "test-hook",
				Description: "test-description",
				Enabled:     true,
				Events: []mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
				},
				Config: &mgmtv1alpha1.AccountHookConfig{
					Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
						Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{
							Url:    srv.URL + "/webhook",
							Secret: "test-secret",
						},
					},
				},
			},
		}))
	require.NoError(t, err)
	require.NotNil(t, hookResp)

	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestWorkflowEnvironment()

	accounthook_workflow_register.Register(
		env,
		husonymApi.OSSAuthenticatedLicensedClients.AccountHooks(tchusonymapi.WithUserId("123")),
	)

	env.ExecuteWorkflow(
		accounthook_workflow.ProcessAccountHook,
		&accounthook_workflow.ProcessAccountHookRequest{
			Event: accounthook_events.NewEvent_JobRunSucceeded(
				accountId,
				"test-job-id",
				"test-job-run-id",
			),
		},
	)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, webhookCount, 1)
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}
