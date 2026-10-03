package hooks_test

import (
	"bytes"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// A stored configuration that cannot be decoded is answered with the id of its hook and
// nothing of what is stored, to the caller and in the log: what a decoder says of a value it
// refuses may quote the value.
func TestAStoredConfigurationThatCannotBeDecoded(t *testing.T) {
	const stored = "a-stored-secret-not-to-be-quoted"

	t.Run("account hook", func(t *testing.T) {
		w := newWorld(t)
		w.role.grants = viewerGrants
		id := w.store.addAccountHook(w.own.accountID, "odd",
			`{"webhook":{"url":"https://example.com/hook","secret":{"value":"`+stored+`"}}}`, true, 0)
		var logged bytes.Buffer
		ctx := logger_interceptor.SetLoggerContext(t.Context(),
			slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
		message := "the stored config of account hook " + str(id) + " cannot be read"

		_, err := w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(id)}))
		requireAnswer(t, err, connect.CodeInternal, message)
		_, err = w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: str(w.own.accountID)}))
		requireAnswer(t, err, connect.CodeInternal, message)

		w.role.grants = adminGrants
		_, err = w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(id), Enabled: true}))
		requireAnswer(t, err, connect.CodeInternal, message)

		require.Contains(t, logged.String(), str(id))
		require.NotContains(t, logged.String(), stored)

		_, err = w.account.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(id)}))
		require.NoError(t, err, "a hook that cannot be read can still be removed")
	})

	t.Run("job hook", func(t *testing.T) {
		w := newWorld(t)
		id := w.store.addJobHook(w.own.jobID, "odd", `{"sql":{"query":{"text":"`+stored+`"}}}`, "preSync", true, 0)
		var logged bytes.Buffer
		ctx := logger_interceptor.SetLoggerContext(t.Context(),
			slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))

		_, err := w.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: str(id)}))
		requireAnswer(t, err, connect.CodeInternal, "the stored config of job hook "+str(id)+" cannot be read")
		require.Contains(t, logged.String(), str(id))
		require.NotContains(t, logged.String(), stored)
	})
}

// Only the constraint that keeps a name to one hook tells a name already taken: another
// unique violation is a failure of the write.
func TestAnotherUniqueViolationIsNotANameTaken(t *testing.T) {
	w := newWorld(t)
	w.store.createFails = &pgconn.PgError{Code: husonymdb.PqUniqueViolationCode, ConstraintName: "some_other_unique"}

	_, err := w.createJobHook(t, &mgmtv1alpha1.NewJobHook{Name: "fresh", Description: "d", Config: sqlHook(str(w.own.connection))})
	require.Error(t, err)
	require.NotEqual(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	_, err = w.createWebhook(t, "fresh", webhook("https://example.com/hook", "s3cret"), failedRun...)
	require.Error(t, err)
	require.NotEqual(t, connect.CodeAlreadyExists, connect.CodeOf(err))
}

// Who the worker is, is the deployment's rule for what only the worker calls. With
// authentication off no caller is told from the worker, and what the worker asks carries the
// secret in clear; with it on, the same caller without the worker's key reads the mask.
func TestTheWorkerReadsTheSecretWithoutAuthentication(t *testing.T) {
	for name, tc := range map[string]struct {
		workerOnly userdata.WorkerOnly
		reads      string
	}{
		"authentication off": {userdata.WorkerOnly{IsAuthEnabled: false}, theSecret},
		"authentication on":  {userdata.WorkerOnly{IsAuthEnabled: true}, theMask},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorldOf(t, tc.workerOnly)
			w.role.grants = viewerGrants

			active, err := w.account.GetActiveAccountHooksByEvent(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
				AccountId: str(w.own.accountID), Event: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			}))
			require.NoError(t, err)
			require.Equal(t, tc.reads, secretOf(active.Msg.GetHooks()[0]))

			got, err := w.account.GetAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(w.own.webhook)}))
			require.NoError(t, err)
			require.Equal(t, tc.reads, secretOf(got.Msg.GetHook()))
		})
	}
}
