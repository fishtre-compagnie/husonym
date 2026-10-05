package accounthooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// fakeHooks is the API as the activities read it.
type fakeHooks struct {
	active    []*mgmtv1alpha1.AccountHook
	activeErr error
	hooks     map[string]*mgmtv1alpha1.AccountHook
	hookErr   error
	hangs     bool // every call waits for the end of its context

	mu      sync.Mutex
	lookups []*mgmtv1alpha1.GetActiveAccountHooksByEventRequest
}

func (f *fakeHooks) GetActiveAccountHooksByEvent(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetActiveAccountHooksByEventRequest],
) (*connect.Response[mgmtv1alpha1.GetActiveAccountHooksByEventResponse], error) {
	if f.hangs {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	f.lookups = append(f.lookups, req.Msg)
	f.mu.Unlock()
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	return connect.NewResponse(&mgmtv1alpha1.GetActiveAccountHooksByEventResponse{Hooks: f.active}), nil
}

func (f *fakeHooks) GetAccountHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountHookRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountHookResponse], error) {
	if f.hangs {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.hookErr != nil {
		return nil, f.hookErr
	}
	hook, ok := f.hooks[req.Msg.GetId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such hook"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetAccountHookResponse{Hook: hook}), nil
}

// receiver counts the webhooks it gets and keeps the last one.
type receiver struct {
	*httptest.Server
	mu     sync.Mutex
	calls  int
	header http.Header
	body   []byte
}

func newReceiver(t *testing.T, status int) *receiver {
	t.Helper()
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.calls++
		r.header = req.Header.Clone()
		r.body = body
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) called() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func webhookHook(id, url string) *mgmtv1alpha1.AccountHook {
	return &mgmtv1alpha1.AccountHook{
		Id:        id,
		AccountId: testRun.AccountID,
		Enabled:   true,
		Events:    []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED},
		Config: &mgmtv1alpha1.AccountHookConfig{
			Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
				Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{Url: url, Secret: "test-secret"},
			},
		},
	}
}

func executeHook(t *testing.T, activities *Activities, req *ExecuteAccountHookRequest) error {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.ExecuteAccountHook)
	_, err := env.ExecuteActivity(activities.ExecuteAccountHook, req)
	return err
}

// requireNonRetryable checks that an activity failed for good, with the given error type.
func requireNonRetryable(t *testing.T, err error, errorType string) *temporal.ApplicationError {
	t.Helper()
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, errorType, appErr.Type())
	require.True(t, appErr.NonRetryable())
	return appErr
}

func requireRetryable(t *testing.T, err error) {
	t.Helper()
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.False(t, appErr.NonRetryable())
}

func Test_GetAccountHooksByEvent_ReturnsTheIdsInTheOrderOfTheAPI(t *testing.T) {
	hooks := &fakeHooks{active: []*mgmtv1alpha1.AccountHook{{Id: "hook-b"}, {Id: "hook-a"}}}
	activities := NewActivities(hooks, webhook.NewSender())
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.GetAccountHooksByEvent)

	value, err := env.ExecuteActivity(activities.GetAccountHooksByEvent, &GetAccountHooksByEventRequest{
		AccountId: "account-1",
		EventName: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
	})
	require.NoError(t, err)
	var response *GetAccountHooksByEventResponse
	require.NoError(t, value.Get(&response))

	require.Equal(t, []string{"hook-b", "hook-a"}, response.HookIds)
	require.Len(t, hooks.lookups, 1)
	require.Equal(t, "account-1", hooks.lookups[0].GetAccountId())
	require.Equal(t, mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED, hooks.lookups[0].GetEvent())
}

func Test_GetAccountHooksByEvent_NoHookIsAnEmptyList(t *testing.T) {
	activities := NewActivities(&fakeHooks{}, webhook.NewSender())
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.GetAccountHooksByEvent)

	value, err := env.ExecuteActivity(activities.GetAccountHooksByEvent, &GetAccountHooksByEventRequest{AccountId: "account-1"})
	require.NoError(t, err)
	var response *GetAccountHooksByEventResponse
	require.NoError(t, value.Get(&response))

	// An empty list, not an absent one: it is written as [] in the history.
	require.Equal(t, []string{}, response.HookIds)
}

func Test_GetAccountHooksByEvent_FailsWhenTheAPIDoes(t *testing.T) {
	hooks := &fakeHooks{activeErr: connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))}
	activities := NewActivities(hooks, webhook.NewSender())
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.GetAccountHooksByEvent)

	_, err := env.ExecuteActivity(activities.GetAccountHooksByEvent, &GetAccountHooksByEventRequest{AccountId: "account-1"})

	require.ErrorContains(t, err, "the hooks of the event cannot be listed: unavailable: connection refused")
	requireRetryable(t, err)
}

func Test_Activities_ReturnWithinTheirDeadlineWhenTheAPIHangs(t *testing.T) {
	activities := NewActivities(&fakeHooks{hangs: true}, webhook.NewSender())
	activities.lookupTimeout = 50 * time.Millisecond
	activities.readTimeout = 50 * time.Millisecond
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.GetAccountHooksByEvent)
	env.RegisterActivity(activities.ExecuteAccountHook)

	started := time.Now()
	_, lookupErr := env.ExecuteActivity(activities.GetAccountHooksByEvent, &GetAccountHooksByEventRequest{AccountId: "account-1"})
	_, executeErr := env.ExecuteActivity(activities.ExecuteAccountHook, &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

	require.Less(t, time.Since(started), 5*time.Second)
	require.ErrorContains(t, lookupErr, context.DeadlineExceeded.Error())
	requireRetryable(t, lookupErr)
	require.ErrorContains(t, executeErr, context.DeadlineExceeded.Error())
	requireRetryable(t, executeErr)
}

// An activity scheduled with a heartbeat timeout of a minute sends no heartbeat: each one
// ends well within that minute by itself.
func Test_NewActivities_BoundsEachActivity(t *testing.T) {
	activities := NewActivities(&fakeHooks{}, webhook.NewSender())
	require.Equal(t, 30*time.Second, activities.lookupTimeout)
	require.Equal(t, 20*time.Second, activities.readTimeout)
}

func Test_ExecuteAccountHook_DeliversTheEvent(t *testing.T) {
	target := newReceiver(t, http.StatusOK)
	hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{"hook-1": webhookHook("hook-1", target.URL)}}

	err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

	require.NoError(t, err)
	require.Equal(t, 1, target.called())
	require.JSONEq(
		t,
		`{"event_name":"ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED","event_data":{"name":3,"accountId":"account-1","timestamp":"2026-10-03T07:51:53Z","jobRunSucceeded":{"jobId":"job-1","jobRunId":"run-1"}}}`,
		string(target.body),
	)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(target.body)
	require.Equal(t, hex.EncodeToString(mac.Sum(nil)), target.header.Get("X-Husonym-Signature"))
	require.Equal(t, "sha256", target.header.Get("X-Husonym-Signature-Type"))
	require.Empty(t, target.header.Get("Authorization"))
}

func Test_ExecuteAccountHook_AWildcardHookListensToEveryEvent(t *testing.T) {
	target := newReceiver(t, http.StatusOK)
	hook := webhookHook("hook-1", target.URL)
	hook.Events = []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED}
	hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{"hook-1": hook}}

	err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

	require.NoError(t, err)
	require.Equal(t, 1, target.called())
}

func Test_ExecuteAccountHook_HonorsTheCertificateOptionOfTheHook(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	hook := webhookHook("hook-1", target.URL)
	hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{"hook-1": hook}}
	activities := NewActivities(hooks, webhook.NewSender())
	req := &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()}

	requireRetryable(t, executeHook(t, activities, req))
	require.Zero(t, calls.Load())

	hook.GetConfig().GetWebhook().DisableSslVerification = true
	require.NoError(t, executeHook(t, activities, req))
	require.EqualValues(t, 1, calls.Load())
}

// A hook that is not to be called is skipped: the activity succeeds and nothing is sent.
func Test_ExecuteAccountHook_SkipsAHookThatIsNotToBeCalled(t *testing.T) {
	tests := []struct {
		name   string
		change func(hook *mgmtv1alpha1.AccountHook)
		gone   bool
	}{
		{name: "deleted since the lookup", gone: true},
		{name: "disabled", change: func(hook *mgmtv1alpha1.AccountHook) { hook.Enabled = false }},
		{name: "listening to other events", change: func(hook *mgmtv1alpha1.AccountHook) {
			hook.Events = []mgmtv1alpha1.AccountHookEvent{
				mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED,
				mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			}
		}},
		{name: "listening to no event", change: func(hook *mgmtv1alpha1.AccountHook) { hook.Events = nil }},
		{name: "of the slack kind", change: func(hook *mgmtv1alpha1.AccountHook) {
			hook.Config = &mgmtv1alpha1.AccountHookConfig{
				Config: &mgmtv1alpha1.AccountHookConfig_Slack{Slack: &mgmtv1alpha1.AccountHookConfig_SlackHook{ChannelId: "C1"}},
			}
		}},
		{name: "without a kind", change: func(hook *mgmtv1alpha1.AccountHook) { hook.Config = &mgmtv1alpha1.AccountHookConfig{} }},
		{name: "without a configuration", change: func(hook *mgmtv1alpha1.AccountHook) { hook.Config = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := newReceiver(t, http.StatusOK)
			hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{}}
			if !tt.gone {
				hook := webhookHook("hook-1", target.URL)
				tt.change(hook)
				hooks.hooks["hook-1"] = hook
			}

			err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

			require.NoError(t, err)
			require.Zero(t, target.called())
		})
	}
}

// A hook that cannot be called whatever the number of attempts fails the activity at once.
func Test_ExecuteAccountHook_RefusesForGood(t *testing.T) {
	tests := []struct {
		name      string
		change    func(hook *mgmtv1alpha1.AccountHook)
		errorType string
		message   string
	}{
		{
			name:      "a hook of another account",
			change:    func(hook *mgmtv1alpha1.AccountHook) { hook.AccountId = "account-2" },
			errorType: "HookOfAnotherAccount",
			message:   "the hook does not belong to the account of the event",
		},
		{
			name: "a webhook without configuration",
			change: func(hook *mgmtv1alpha1.AccountHook) {
				hook.Config = &mgmtv1alpha1.AccountHookConfig{Config: &mgmtv1alpha1.AccountHookConfig_Webhook{}}
			},
			errorType: "WebhookConfigMissing",
			message:   "the hook is a webhook that has no configuration",
		},
		{
			name:      "a masked secret",
			change:    func(hook *mgmtv1alpha1.AccountHook) { hook.GetConfig().GetWebhook().Secret = "********" },
			errorType: "WebhookSecretMasked",
			message: "the API returned the masked value in place of the secret: either the worker is not identified " +
				"by its API key, or the stored secret is that very value and must be set again",
		},
		{
			name:      "a URL that is not http",
			change:    func(hook *mgmtv1alpha1.AccountHook) { hook.GetConfig().GetWebhook().Url = "ftp://example.com/hook" },
			errorType: "WebhookInvalidURL",
			message:   "invalid url",
		},
		{
			name: "a link-local destination",
			change: func(hook *mgmtv1alpha1.AccountHook) {
				hook.GetConfig().GetWebhook().Url = "http://169.254.169.254/latest/meta-data/"
			},
			errorType: "WebhookDestinationRefused",
			message:   "destination refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := newReceiver(t, http.StatusOK)
			hook := webhookHook("hook-1", target.URL)
			tt.change(hook)
			hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{"hook-1": hook}}

			err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

			requireNonRetryable(t, err, tt.errorType)
			require.ErrorContains(t, err, tt.message)
			require.Zero(t, target.called())
		})
	}
}

func Test_ExecuteAccountHook_RefusesARequestWithoutEvent(t *testing.T) {
	activities := NewActivities(&fakeHooks{}, webhook.NewSender())

	err := executeHook(t, activities, &ExecuteAccountHookRequest{HookId: "hook-1"})

	appErr := requireNonRetryable(t, err, "MissingEvent")
	require.Equal(t, "event is required", appErr.Message())
}

func Test_ExecuteAccountHook_RetriesWhenTheHookCannotBeRead(t *testing.T) {
	hooks := &fakeHooks{hookErr: connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))}

	err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

	require.ErrorContains(t, err, "the hook cannot be read: unavailable: connection refused")
	requireRetryable(t, err)
}

func Test_ExecuteAccountHook_RetriesOnlyWhatCanHeal(t *testing.T) {
	tests := []struct {
		status    int
		errorType string // empty when the failure is retried
	}{
		{http.StatusInternalServerError, ""},
		{http.StatusServiceUnavailable, ""},
		{http.StatusTooManyRequests, ""},
		{http.StatusRequestTimeout, ""},
		{http.StatusBadRequest, "WebhookRejected"},
		{http.StatusNotFound, "WebhookRejected"},
		{http.StatusFound, "WebhookRedirected"},
		{http.StatusPermanentRedirect, "WebhookRedirected"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			target := newReceiver(t, tt.status)
			hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{"hook-1": webhookHook("hook-1", target.URL+"/hook?token=abc")}}

			err := executeHook(t, NewActivities(hooks, webhook.NewSender()), &ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()})

			require.ErrorContains(t, err, "the webhook was not delivered")
			require.ErrorContains(t, err, strconv.Itoa(tt.status))
			require.NotContains(t, err.Error(), "token")
			require.Equal(t, 1, target.called())
			if tt.errorType == "" {
				requireRetryable(t, err)
				return
			}
			requireNonRetryable(t, err, tt.errorType)
		})
	}
}

func Test_ExecuteAccountHook_NamesADeliveryTheSameOnEveryAttempt(t *testing.T) {
	target := newReceiver(t, http.StatusOK)
	hooks := &fakeHooks{hooks: map[string]*mgmtv1alpha1.AccountHook{
		"hook-1": webhookHook("hook-1", target.URL),
		"hook-2": webhookHook("hook-2", target.URL),
	}}
	activities := NewActivities(hooks, webhook.NewSender())
	deliveryIDOf := func(hookID string) string {
		require.NoError(t, executeHook(t, activities, &ExecuteAccountHookRequest{HookId: hookID, Event: testEvent()}))
		return target.header.Get("Webhook-Id")
	}

	first := deliveryIDOf("hook-1")
	require.Len(t, first, 32)
	require.Equal(t, first, deliveryIDOf("hook-1"))
	require.NotEqual(t, first, deliveryIDOf("hook-2"))
}

// The id of a delivery is the one of an event, hence of the workflow that processes it,
// for a hook. The value below was computed apart from this code.
func Test_DeliveryID(t *testing.T) {
	require.Equal(t, "595ae46a07955a0689e0643745ba1ddb", deliveryID("wf-1", "hook-1"))
	require.NotEqual(t, deliveryID("wf-1", "hook-1"), deliveryID("wf-2", "hook-1"))
	require.NotEqual(t, deliveryID("wf-1", "hook-1"), deliveryID("wf-1", "hook-2"))
	require.NotEqual(t, deliveryID("a", "bc"), deliveryID("ab", "c"))
}
