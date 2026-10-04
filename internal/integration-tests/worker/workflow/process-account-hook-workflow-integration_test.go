package integrationtest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

// Not parallel: next to Test_Workflow starting its databases under -race, the workflow
// goroutine could wait more than a second for a CPU, which the test environment of
// Temporal reports as a deadlock (TMPRL1101).
func Test_ProcessAccountHookWorkflow(t *testing.T) {
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
	clients := husonymApi.OSSAuthenticatedLicensedClients
	admin := tchusonymapi.WithUserId("123")

	tchusonymapi.SetUser(ctx, t, clients.Users(admin))
	accountId := tchusonymapi.CreateTeamAccount(ctx, t, clients.Users(admin), uuid.NewString())

	// A member who may not edit the account: the API hides the secret of a hook from it.
	viewer := tchusonymapi.WithUserId("456")
	viewerId := tchusonymapi.SetUser(ctx, t, clients.Users(viewer))
	accountUuid, err := husonymdb.ToUuid(accountId)
	require.NoError(t, err)
	viewerUuid, err := husonymdb.ToUuid(viewerId)
	require.NoError(t, err)
	require.NoError(t, husonymApi.HusonymQuerier.CreateAccountUserAssociation(ctx, husonymApi.Pgcontainer.DB,
		db_queries.CreateAccountUserAssociationParams{AccountID: accountUuid, UserID: viewerUuid},
	))
	roleResp, err := clients.Users(admin).SetUserRole(ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRoleRequest{
		AccountId: accountId, UserId: viewerId, Role: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
	}))
	tchusonymapi.RequireNoErrResp(t, roleResp, err)

	var mu sync.Mutex
	var bodies [][]byte
	var headers []http.Header
	mux := http.NewServeMux()
	mux.Handle("/webhook", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, body)
		headers = append(headers, r.Header.Clone())
		w.WriteHeader(http.StatusOK)
	}))
	srv := startHTTPServer(t, mux)

	hookResp, err := clients.AccountHooks(admin).
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

	// process runs the workflow for a succeeded event, with activities that read the hooks
	// as the given caller.
	process := func(t *testing.T, hooks mgmtv1alpha1connect.AccountHookServiceClient) error {
		t.Helper()
		testSuite := &testsuite.WorkflowTestSuite{}
		testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
		env := testSuite.NewTestWorkflowEnvironment()
		accounthooks.Register(env, hooks, webhook.NewSender())

		env.ExecuteWorkflow(
			accounthooks.ProcessAccountHook,
			&accounthooks.ProcessAccountHookRequest{
				Event: runevents.Run{
					AccountID: accountId,
					JobID:     "test-job-id",
					RunID:     "test-job-run-id",
				}.Succeeded(time.Now()),
			},
		)
		require.True(t, env.IsWorkflowCompleted())
		return env.GetWorkflowError()
	}

	t.Run("the worker reads the secret and the receiver gets a webhook it can verify", func(t *testing.T) {
		worker := clients.AccountHooks(tchusonymapi.WithUserId(apikey.NewV1WorkerKey()))

		require.NoError(t, process(t, worker))

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, bodies, 1)
		mac := hmac.New(sha256.New, []byte("test-secret"))
		mac.Write(bodies[0])
		require.Equal(t, hex.EncodeToString(mac.Sum(nil)), headers[0].Get("X-Husonym-Signature"))
		require.Equal(t, "sha256", headers[0].Get("X-Husonym-Signature-Type"))
		require.NotEmpty(t, headers[0].Get("Webhook-Id"))
		require.Empty(t, headers[0].Get("Authorization"))
	})

	t.Run("a caller that reads a mask in place of the secret sends nothing", func(t *testing.T) {
		mu.Lock()
		before := len(bodies)
		mu.Unlock()

		err := process(t, clients.AccountHooks(viewer))

		require.ErrorContains(t, err, "the API returned a masked secret")
		require.ErrorContains(t, err, "type: WebhookSecretMasked, retryable: false")
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, bodies, before)
	})
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}
