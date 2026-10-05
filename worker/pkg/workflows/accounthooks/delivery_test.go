package accounthooks

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// deliveries runs the whole workflow, with its real activities and its real retry policy,
// against a receiver, and returns the Webhook-Id and Webhook-Timestamp of each request.
func deliveries(t *testing.T, workflowID, hookID string, answer int) (ids, timestamps []string) {
	t.Helper()
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, r.Header.Get("Webhook-Id"))
		timestamps = append(timestamps, r.Header.Get("Webhook-Timestamp"))
		w.WriteHeader(answer)
	}))
	t.Cleanup(target.Close)
	hook := webhookHook(hookID, target.URL)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.SetTestTimeout(30 * time.Second)
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: workflowID})
	Register(env, &fakeHooks{
		active: []*mgmtv1alpha1.AccountHook{hook},
		hooks:  map[string]*mgmtv1alpha1.AccountHook{hookID: hook},
	}, webhook.NewSender())

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	if answer == http.StatusOK {
		require.NoError(t, env.GetWorkflowError())
	} else {
		require.Error(t, env.GetWorkflowError())
	}
	mu.Lock()
	defer mu.Unlock()
	return ids, timestamps
}

// A receiver that answers 503 gets the five attempts of the delivery, and tells that they
// are one delivery by their id.
func Test_ProcessAccountHook_EveryAttemptOfADeliveryCarriesItsId(t *testing.T) {
	ids, timestamps := deliveries(t, "run-1-hook-job-run-succeeded-1", "hook-1", http.StatusServiceUnavailable)

	require.Len(t, ids, 5)
	require.Len(t, timestamps, 5)
	require.Equal(t, deliveryID("run-1-hook-job-run-succeeded-1", "hook-1"), ids[0])
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
}

// The id of a delivery comes from the id of the workflow that processes the event, not
// from one execution of it: the workflow executed again names its delivery the same.
func Test_ProcessAccountHook_ADeliveryKeepsItsIdWhenTheWorkflowIsExecutedAgain(t *testing.T) {
	first, _ := deliveries(t, "run-1-hook-job-run-succeeded-1", "hook-1", http.StatusOK)
	again, _ := deliveries(t, "run-1-hook-job-run-succeeded-1", "hook-1", http.StatusOK)
	otherEvent, _ := deliveries(t, "run-1-hook-job-run-failed-2", "hook-1", http.StatusOK)
	otherHook, _ := deliveries(t, "run-1-hook-job-run-succeeded-1", "hook-2", http.StatusOK)

	require.Len(t, first, 1)
	require.Len(t, first[0], 32)
	require.Equal(t, first, again)
	require.NotEqual(t, first, otherEvent)
	require.NotEqual(t, first, otherHook)
}

// recordedLog keeps the lines a workflow logs at the error level.
type recordedLog struct {
	mu     sync.Mutex
	errors []string
}

func (l *recordedLog) Debug(string, ...any) {}
func (l *recordedLog) Info(string, ...any)  {}
func (l *recordedLog) Warn(string, ...any)  {}
func (l *recordedLog) Error(msg string, keyvals ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, fmt.Sprint(append([]any{msg}, keyvals...)...))
}

func (l *recordedLog) linesOf(msg string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var lines []string
	for _, line := range l.errors {
		if len(line) >= len(msg) && line[:len(msg)] == msg {
			lines = append(lines, line)
		}
	}
	return lines
}

// A webhook that was not delivered is otherwise only seen in the history of the workflow:
// the workflow logs one error line for each hook that failed.
func Test_ProcessAccountHook_LogsEachHookThatFailed(t *testing.T) {
	logs := &recordedLog{}
	var ts testsuite.WorkflowTestSuite
	ts.SetLogger(logs)
	env := ts.NewTestWorkflowEnvironment()
	activities := NewActivities(nil, nil)
	env.RegisterActivity(activities.GetAccountHooksByEvent)
	env.RegisterActivity(activities.ExecuteAccountHook)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2", "hook-3"}}, nil)
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, &ExecuteAccountHookRequest{HookId: "hook-2", Event: testEvent()}).
		Return(&ExecuteAccountHookResponse{}, nil)
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(nil, temporal.NewNonRetryableApplicationError("the receiver said no", "WebhookRejected", nil))

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.Error(t, env.GetWorkflowError())
	lines := logs.linesOf("account hook failed")
	require.Len(t, lines, 2)
	require.Contains(t, lines[0], "account-1")
	require.Contains(t, lines[0], "hook-1")
	require.Contains(t, lines[0], "the receiver said no")
	require.Contains(t, lines[1], "hook-3")
}

// Every reason that is permanent has an error type of its own, and is not retried.
func Test_DeliveryError_NamesEveryPermanentReason(t *testing.T) {
	types := map[webhook.Reason]string{
		webhook.ReasonEvent:       "WebhookInvalidEvent",
		webhook.ReasonInvalidURL:  "WebhookInvalidURL",
		webhook.ReasonDestination: "WebhookDestinationRefused",
		webhook.ReasonRedirected:  "WebhookRedirected",
		webhook.ReasonRejected:    "WebhookRejected",
	}
	for reason, errorType := range types {
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, deliveryError(&webhook.Error{Reason: reason}), &appErr)
		require.Equal(t, errorType, appErr.Type())
		require.True(t, appErr.NonRetryable())
	}
	for _, reason := range []webhook.Reason{webhook.ReasonUnavailable, webhook.ReasonTransport} {
		var appErr *temporal.ApplicationError
		require.False(t, errors.As(deliveryError(&webhook.Error{Reason: reason}), &appErr))
	}
}
