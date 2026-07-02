package authlogging_interceptor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	auth_jwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_Interceptor_WrapUnary_JwtContextData_ValidUser(t *testing.T) {
	logger := testutil.GetTestLogger(t)

	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	genuuid, _ := husonymdb.ToUuid(uuid.NewString())
	mockQuerier.On("GetUserByProviderSub", mock.Anything, mock.Anything, "auth-user-id").
		Return(db_queries.HusonymApiUser{ID: genuuid}, nil)

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: "123"}), nil
		},
		connect.WithInterceptors(
			logger_interceptor.NewInterceptor(logger),
			&mockAuthInterceptor{data: &auth_jwt.TokenContextData{AuthUserId: "auth-user-id"}},
			NewInterceptor(husonymdb.New(mockDbtx, mockQuerier)),
		),
	))

	srv := startHTTPServer(t, mux)
	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}),
	)
	require.NoError(t, err)
}

func Test_Interceptor_WrapUnary_JwtContextData_NoUser_NoFail(t *testing.T) {
	logger := testutil.GetTestLogger(t)

	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.UserAccountServiceGetUserProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.UserAccountServiceGetUserProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetUserRequest]) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: "123"}), nil
		},
		connect.WithInterceptors(
			logger_interceptor.NewInterceptor(logger),
			NewInterceptor(husonymdb.New(mockDbtx, mockQuerier)),
		),
	))

	srv := startHTTPServer(t, mux)
	client := mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
	_, err := client.GetUser(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}),
	)
	require.NoError(t, err)
}

type mockAuthInterceptor struct {
	data *auth_jwt.TokenContextData
}

func (i *mockAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		return next(context.WithValue(ctx, auth_jwt.TokenContextKey{}, i.data), request)
	}
}

func (i *mockAuthInterceptor) WrapStreamingClient(
	next connect.StreamingClientFunc,
) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (i *mockAuthInterceptor) WrapStreamingHandler(
	next connect.StreamingHandlerFunc,
) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, conn)
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

func Test_getAuthValues_NoTokenCtx(t *testing.T) {
	vals := getAuthValues(context.Background(), &husonymdb.HusonymDb{})
	require.Empty(t, vals)
}

func Test_getAuthValues_Valid_Jwt(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	uuidstr := uuid.NewString()
	genuuid, _ := husonymdb.ToUuid(uuidstr)
	mockQuerier.On("GetUserByProviderSub", mock.Anything, mock.Anything, "auth-user-id").
		Return(db_queries.HusonymApiUser{ID: genuuid}, nil)

	ctx := context.WithValue(
		context.Background(),
		auth_jwt.TokenContextKey{},
		&auth_jwt.TokenContextData{
			AuthUserId: "auth-user-id",
		},
	)

	vals := getAuthValues(ctx, husonymdb.New(mockDbtx, mockQuerier))
	require.Equal(
		t,
		[]any{"authUserId", "auth-user-id", "userId", uuidstr},
		vals,
	)
}

func Test_getAuthValues_Valid_Jwt_No_User(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	mockQuerier.On("GetUserByProviderSub", mock.Anything, mock.Anything, "auth-user-id").
		Return(db_queries.HusonymApiUser{}, errors.New("test err"))

	ctx := context.WithValue(
		context.Background(),
		auth_jwt.TokenContextKey{},
		&auth_jwt.TokenContextData{
			AuthUserId: "auth-user-id",
		},
	)

	vals := getAuthValues(ctx, husonymdb.New(mockDbtx, mockQuerier))
	require.Equal(
		t,
		[]any{"authUserId", "auth-user-id"},
		vals,
	)
}

func Test_getAuthValues_Valid_ApiKey(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	apikeyid := uuid.NewString()
	accountid := uuid.NewString()
	userid := uuid.NewString()

	apikeyuuid, _ := husonymdb.ToUuid(apikeyid)
	accountiduuid, _ := husonymdb.ToUuid(accountid)
	useriduuid, _ := husonymdb.ToUuid(userid)

	ctx := context.WithValue(
		context.Background(),
		auth_apikey.TokenContextKey{},
		&auth_apikey.TokenContextData{
			ApiKeyType: apikey.AccountApiKey,
			ApiKey: &db_queries.HusonymApiAccountApiKey{
				ID:        apikeyuuid,
				AccountID: accountiduuid,
				UserID:    useriduuid,
			},
		},
	)

	vals := getAuthValues(ctx, husonymdb.New(mockDbtx, mockQuerier))
	require.Equal(
		t,
		[]any{
			"apiKeyType",
			apikey.AccountApiKey,
			"apiKeyId",
			apikeyid,
			"accountId",
			accountid,
			"userId",
			userid,
		},
		vals,
	)
}

func Test_getAuthValues_Valid_ApiKey_No_Apikey(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)

	ctx := context.WithValue(
		context.Background(),
		auth_apikey.TokenContextKey{},
		&auth_apikey.TokenContextData{
			ApiKeyType: apikey.AccountApiKey,
		},
	)

	vals := getAuthValues(ctx, husonymdb.New(mockDbtx, mockQuerier))
	require.Equal(
		t,
		[]any{"apiKeyType", apikey.AccountApiKey},
		vals,
	)
}
