package retry_interceptor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/cenkalti/backoff/v7"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// failingAnonymization is an API whose AnonymizeSingle fails with a code a number of times,
// then anonymizes.
type failingAnonymization struct {
	mgmtv1alpha1connect.UnimplementedAnonymizationServiceHandler
	code     connect.Code
	failures int32
	calls    atomic.Int32
}

func (f *failingAnonymization) AnonymizeSingle(
	context.Context,
	*connect.Request[mgmtv1alpha1.AnonymizeSingleRequest],
) (*connect.Response[mgmtv1alpha1.AnonymizeSingleResponse], error) {
	if f.calls.Add(1) <= f.failures {
		return nil, connect.NewError(f.code, errors.New("presidio did not answer"))
	}
	return connect.NewResponse(&mgmtv1alpha1.AnonymizeSingleResponse{OutputData: `"anonymized"`}), nil
}

// anonymizeThrough calls AnonymizeSingle on the API the way the worker does: over HTTP, through
// a client that carries the interceptor.
func anonymizeThrough(t *testing.T, api *failingAnonymization, interceptor *Interceptor) (string, error) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewAnonymizationServiceHandler(api))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := mgmtv1alpha1connect.NewAnonymizationServiceClient(
		srv.Client(), srv.URL, connect.WithInterceptors(interceptor),
	)
	resp, err := client.AnonymizeSingle(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{InputData: `"Jane"`}),
	)
	if err != nil {
		return "", err
	}
	return resp.Msg.GetOutputData(), nil
}

// During a run the worker anonymizes a value through the API. An API that answers unavailable,
// which is what it answers when Presidio does not, is asked again: the value is anonymized once
// Presidio is back, and the run goes on.
func TestWorkerClient_RidesOutAnApiThatIsUnavailable(t *testing.T) {
	api := &failingAnonymization{code: connect.CodeUnavailable, failures: 2}

	// The interceptor the worker gives its clients. Two retries wait about a second in all.
	output, err := anonymizeThrough(t, api, DefaultRetryInterceptor(testutil.GetTestLogger(t)))

	require.NoError(t, err)
	require.Equal(t, `"anonymized"`, output)
	require.Equal(t, int32(3), api.calls.Load())
}

// An API that stays unavailable fails the value once the tries run out, with the error of the
// API.
func TestWorkerClient_GivesUpOnAnApiThatStaysUnavailable(t *testing.T) {
	api := &failingAnonymization{code: connect.CodeUnavailable, failures: 1000}
	// As many tries as the worker's interceptor makes, without its waits: with them, giving up
	// takes a minute.
	const tries = 10
	impatient := New(WithRetryOptions(func() []backoff.RetryOption {
		return []backoff.RetryOption{
			backoff.WithBackOff(backoff.NewConstantBackOff(time.Millisecond)),
			backoff.WithMaxTries(tries),
		}
	}))

	_, err := anonymizeThrough(t, api, impatient)

	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.ErrorContains(t, err, "presidio did not answer")
	require.Equal(t, int32(tries), api.calls.Load())
}

// What is not the API being unavailable is not asked again: the request Presidio refused, and
// the call whose own caller ran out of time or gave up.
func TestWorkerClient_AsksOnceWhenTheApiIsNotUnavailable(t *testing.T) {
	for _, code := range []connect.Code{
		connect.CodeInvalidArgument,
		connect.CodeDeadlineExceeded,
		connect.CodeCanceled,
		connect.CodeInternal,
	} {
		t.Run(code.String(), func(t *testing.T) {
			api := &failingAnonymization{code: code, failures: 1000}

			_, err := anonymizeThrough(t, api, DefaultRetryInterceptor(testutil.GetTestLogger(t)))

			require.Equal(t, code, connect.CodeOf(err))
			require.Equal(t, int32(1), api.calls.Load())
		})
	}
}
