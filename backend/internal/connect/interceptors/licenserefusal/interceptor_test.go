package licenserefusal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	refusal "github.com/fishtre-compagnie/husonym/backend/internal/licenserefusal"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

const streamProcedure = "/test.v1.TestService/Stream"

type counted struct {
	accountId string
	gates     []license.Gate
}

// recorder is a counter that remembers what it was asked to count.
type recorder struct {
	mu    sync.Mutex
	calls []counted
	fail  error
}

func (r *recorder) CountRefusal(_ context.Context, accountId string, gates []license.Gate, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, counted{accountId: accountId, gates: gates})
	return r.fail
}

func (r *recorder) count() []counted {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]counted(nil), r.calls...)
}

func refusalOf(accountId string, gates ...license.Gate) *license.Refusal {
	return license.NewRefusal(accountId, husonymerrors.NewForbidden("not allowed"), gates...)
}

// callUnary serves a unary handler that answers with err behind the interceptor, and calls it.
func callUnary(t *testing.T, counter refusal.Counter, logs *bytes.Buffer, err error) error {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(context.Context, *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: "123"}), nil
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), NewInterceptor(counter)),
	))
	srv := startHTTPServer(t, mux)
	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, callErr := client.GetUser(context.Background(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	return callErr
}

// callStream does the same for a server-streaming handler.
func callStream(t *testing.T, counter refusal.Counter, logs *bytes.Buffer, err error) error {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	mux := http.NewServeMux()
	mux.Handle(streamProcedure, connect.NewServerStreamHandler(
		streamProcedure,
		func(context.Context, *connect.Request[mgmtv1alpha1.GetUserRequest], *connect.ServerStream[mgmtv1alpha1.GetUserResponse]) error {
			return err
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), NewInterceptor(counter)),
	))
	srv := startHTTPServer(t, mux)
	client := connect.NewClient[mgmtv1alpha1.GetUserRequest, mgmtv1alpha1.GetUserResponse](srv.Client(), srv.URL+streamProcedure)
	stream, callErr := client.CallServerStream(context.Background(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if callErr != nil {
		return callErr
	}
	for stream.Receive() {
	}
	return stream.Err()
}

type call func(*testing.T, refusal.Counter, *bytes.Buffer, error) error

func Test_Interceptor(t *testing.T) {
	for name, do := range map[string]call{"unary": callUnary, "streaming": callStream} {
		t.Run(name, func(t *testing.T) {
			t.Run("a wrapped refusal is counted once with its account and gates, and the error is unchanged", func(t *testing.T) {
				counter := &recorder{}
				refusal := refusalOf("an-account", license.FeatureGate(license.FeatureRbac), license.GateJobCap)

				err := do(t, counter, &bytes.Buffer{}, fmt.Errorf("checking: %w", refusal))

				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				require.Equal(t, []counted{{
					accountId: "an-account",
					gates:     []license.Gate{license.FeatureGate(license.FeatureRbac), license.GateJobCap},
				}}, counter.count())
			})

			t.Run("another error is not counted", func(t *testing.T) {
				counter := &recorder{}

				err := do(t, counter, &bytes.Buffer{}, connect.NewError(connect.CodePermissionDenied, errors.New("no")))

				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				require.Empty(t, counter.count())
			})

			t.Run("no error is not counted", func(t *testing.T) {
				counter := &recorder{}

				require.NoError(t, do(t, counter, &bytes.Buffer{}, nil))
				require.Empty(t, counter.count())
			})

			t.Run("a counter that fails changes nothing and is logged", func(t *testing.T) {
				counter := &recorder{fail: errors.New("the usage database is down")}
				var logs bytes.Buffer

				err := do(t, counter, &logs, refusalOf("an-account", license.GateNotInForce))

				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				require.Len(t, counter.count(), 1)
				require.Contains(t, logs.String(), "unable to count a license refusal")
			})

			t.Run("a refusal without an account or without a gate is not counted", func(t *testing.T) {
				counter := &recorder{}

				err := do(t, counter, &bytes.Buffer{}, refusalOf("", license.GateNotInForce))
				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				err = do(t, counter, &bytes.Buffer{}, refusalOf("an-account"))
				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

				require.Empty(t, counter.count())
			})
		})
	}
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}
