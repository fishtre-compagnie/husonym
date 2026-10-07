package bookend_logging_interceptor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func Test_Interceptor_WrapUnary_Without_Error(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	interceptor := NewInterceptor()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: "123"}), nil
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), interceptor),
	))
	srv := startHTTPServer(t, mux)

	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}),
	)
	assert.Nil(t, err)
}

func Test_Interceptor_WrapUnary_With_Generic_Error(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	interceptor := NewInterceptor()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return nil, errors.New("test")
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), interceptor),
	))
	srv := startHTTPServer(t, mux)

	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}),
	)
	assert.Error(t, err)
}

func Test_Interceptor_WrapUnary_With_Connect_Error(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	interceptor := NewInterceptor()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("test"))
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), interceptor),
	))
	srv := startHTTPServer(t, mux)

	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}),
	)
	assert.Error(t, err)
}

func Test_Bookend_LogsTheCodeOfAWrappedConnectError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	interceptor := NewInterceptor()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return nil, license.NewRefusal("acc", husonymerrors.NewForbidden("x"), license.FeatureGate(license.FeatureRbac))
		},
		connect.WithInterceptors(logger_interceptor.NewInterceptor(logger), interceptor),
	))
	srv := startHTTPServer(t, mux)

	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(context.Background(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	var codes []string
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var entry map[string]any
		assert.NoError(t, json.Unmarshal(line, &entry))
		if code, ok := entry["connect.code"].(string); ok {
			codes = append(codes, code)
		}
	}
	assert.NotEmpty(t, codes)
	for _, code := range codes {
		assert.Equal(t, "permission_denied", code)
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
