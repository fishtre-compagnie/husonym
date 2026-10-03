package hooks_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/stretchr/testify/require"
)

func secretOf(hook *mgmtv1alpha1.AccountHook) string {
	return hook.GetConfig().GetWebhook().GetSecret()
}

// The secret a webhook is signed with is read by the worker, which signs, and by whoever may
// edit the account, who set it. Anyone else reads a mask in its place.
func TestTheSecretIsReadByTheWorkerAndTheEditors(t *testing.T) {
	callers := []struct {
		name   string
		become func(ctx context.Context, w *world) context.Context
		reads  string
	}{
		{"the worker", func(ctx context.Context, _ *world) context.Context { return asWorker(ctx) }, theSecret},
		{"an editor of the account", func(ctx context.Context, _ *world) context.Context { return ctx }, theSecret},
		{"a viewer of the account", func(ctx context.Context, w *world) context.Context {
			w.role.grants = viewerGrants
			return ctx
		}, theMask},
	}
	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			w := newWorld(t)
			ctx := caller.become(t.Context(), w)

			got, err := w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(w.own.webhook)}))
			require.NoError(t, err)
			require.Equal(t, caller.reads, secretOf(got.Msg.GetHook()), "get")

			list, err := w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: str(w.own.accountID)}))
			require.NoError(t, err)
			webhooks := 0
			for _, hook := range list.Msg.GetHooks() {
				if hook.GetConfig().GetWebhook() != nil {
					webhooks++
					require.Equal(t, caller.reads, secretOf(hook), "list")
				}
			}
			require.Equal(t, 2, webhooks)

			active, err := w.account.GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
				AccountId: str(w.own.accountID), Event: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			}))
			require.NoError(t, err)
			require.Equal(t, str(w.own.webhook), active.Msg.GetHooks()[0].GetId())
			require.Equal(t, caller.reads, secretOf(active.Msg.GetHooks()[0]), "active by event")

			require.Contains(t, string(w.store.accountHooks[w.own.webhook].Config), theSecret, "what is stored is not masked")
		})
	}
}

func TestAnEditorReadsTheSecretInWhatAWriteReturns(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()

	created, err := w.account.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
		AccountId: str(w.own.accountID),
		Hook: &mgmtv1alpha1.NewAccountHook{
			Name: "fresh", Description: "a new hook", Events: failedRun, Config: webhook("https://example.com/new", "created-secret"),
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "created-secret", secretOf(created.Msg.GetHook()))

	updated, err := w.account.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
		Id: created.Msg.GetHook().GetId(), Name: "fresh", Description: "changed", Events: failedRun,
		Config: webhook("https://example.com/new", "updated-secret"),
	}))
	require.NoError(t, err)
	require.Equal(t, "updated-secret", secretOf(updated.Msg.GetHook()))

	enabled, err := w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
		Id: created.Msg.GetHook().GetId(), Enabled: true,
	}))
	require.NoError(t, err)
	require.Equal(t, "updated-secret", secretOf(enabled.Msg.GetHook()))
}

// The mask is what a caller reads, never a secret: sent back, it is refused rather than
// stored, and the stored secret is not kept in its place.
func TestAMaskedSecretIsRefused(t *testing.T) {
	const message = "the secret of the webhook is masked: send the secret itself"

	t.Run("on create", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.account.CreateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
			AccountId: str(w.own.accountID),
			Hook: &mgmtv1alpha1.NewAccountHook{
				Name: "fresh", Description: "a new hook", Events: failedRun, Config: webhook("https://example.com/new", theMask),
			},
		}))
		requireAnswer(t, err, connect.CodeInvalidArgument, message)
		require.Zero(t, w.store.writes)
	})

	t.Run("on update", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.account.UpdateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: str(w.own.webhook), Name: "renamed", Description: "changed", Events: failedRun,
			Config: webhook("https://example.com/elsewhere", theMask),
		}))
		requireAnswer(t, err, connect.CodeInvalidArgument, message)
		require.Zero(t, w.store.writes)
		require.Contains(t, string(w.store.accountHooks[w.own.webhook].Config), "https://example.com/hook")
	})
}

// No operation writes a secret to the log, whatever its outcome.
func TestTheSecretIsNeverLogged(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	for i := range operations {
		for s := range situations {
			if operations[i].cells[s].cannot != "" {
				continue
			}
			w := newWorld(t)
			target, slack := w.stand(s)
			ctx := logger_interceptor.SetLoggerContext(t.Context(), logger)
			_, _ = operations[i].call(ctx, w, target, slack)
		}
	}

	require.NotEmpty(t, logged.String(), "the operations log what they do")
	require.NotContains(t, logged.String(), theSecret)
	require.NotContains(t, logged.String(), "a-new-secret")
}
