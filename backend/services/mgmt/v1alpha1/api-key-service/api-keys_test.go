package v1alpha1_apikeyservice

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	pgxmock "github.com/fishtre-compagnie/husonym/internal/mocks/github.com/jackc/pgx/v5"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Test_Service_GetAccountApiKeys(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := []db_queries.HusonymApiAccountApiKey{
		{
			ID:          newPgUuid(t),
			AccountID:   newPgUuid(t),
			KeyValue:    "foo",
			CreatedByID: newPgUuid(t),
			UpdatedByID: newPgUuid(t),
			CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
			ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
			KeyName:     "foo",
		},
		{
			ID:          newPgUuid(t),
			AccountID:   newPgUuid(t),
			KeyValue:    "foobar",
			CreatedByID: newPgUuid(t),
			UpdatedByID: newPgUuid(t),
			CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
			ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
			KeyName:     "foobar",
		},
	}
	mockQuerier.On("GetAccountApiKeys", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, true)

	resp, err := svc.GetAccountApiKeys(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeysRequest{
			AccountId: uuid.NewString(),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Msg.ApiKeys)
	assert.Len(
		t,
		resp.Msg.ApiKeys,
		len(rawData),
	)
	for idx, apiKey := range resp.Msg.ApiKeys {
		dbApikey := rawData[idx]
		assert.Equal(t, apiKey.Id, husonymdb.UUIDString(dbApikey.ID))
		assert.Nil(t, apiKey.KeyValue)
		assert.Equal(t, apiKey.Name, dbApikey.KeyName)
	}
}

func Test_Service_GetAccountApiKeys_ForbiddenAccount(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockIsUserInAccount(t, mockUserService, false)

	resp, err := svc.GetAccountApiKeys(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeysRequest{
			AccountId: uuid.NewString(),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_GetAccountApiKey_Found(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, true)

	resp, err := svc.GetAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, resp.Msg.ApiKey.Id, husonymdb.UUIDString(rawData.ID))
	assert.Nil(t, resp.Msg.ApiKey.KeyValue)
}

func Test_Service_GetAccountApiKey_NotFound(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(db_queries.HusonymApiAccountApiKey{}, pgx.ErrNoRows)

	resp, err := svc.GetAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_GetAccountApiKey_Found_ForbiddenAccount(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, false)

	resp, err := svc.GetAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_CreateAccountApiKey(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockTx := pgxmock.NewMockTx(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	enforcer := mockIsUserInAccount(t, mockUserService, true)
	enforcer.On("Job", mock.Anything, mock.Anything, rbac.JobAction_Execute).Return(true, nil)
	enforcer.On("Connection", mock.Anything, mock.Anything, rbac.ConnectionAction_View).Return(true, nil)

	mockDbtx.On("Begin", mock.Anything).Return(mockTx, nil)
	mockTx.On("Commit", mock.Anything).Return(nil)
	mockTx.On("Rollback", mock.Anything).Return(nil)
	user := db_queries.HusonymApiUser{
		ID:       newPgUuid(t),
		UserType: 1,
	}
	mockQuerier.On("CreateMachineUser", mock.Anything, mock.Anything, mock.Anything).
		Return(user, nil)
	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
		UserID:      user.ID,
	}
	mockQuerier.On("CreateAccountApiKey", mock.Anything, mock.Anything,
		mock.MatchedBy(func(arg db_queries.CreateAccountApiKeyParams) bool {
			return slices.Equal(arg.Permissions, []string{"job:execute", "connection:view"})
		})).
		Return(rawData, nil)

	resp, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId: uuid.NewString(),
			Name:      "foo",
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
			Permissions: []mgmtv1alpha1.Permission{
				mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE,
				mgmtv1alpha1.Permission_PERMISSION_CONNECTION_VIEW,
			},
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Msg.ApiKey.KeyValue)
	assert.NotEqual(
		t,
		resp.Msg.ApiKey.KeyValue,
		rawData.KeyValue,
		"KeyValue return should be the clear text, not the hash",
	)
}

// A key cannot hold more than its creator: a permission the creator lacks refuses the key,
// and nothing is written.
func Test_Service_CreateAccountApiKey_BeyondTheCreator(t *testing.T) {
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)
	svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)

	enforcer := mockIsUserInAccount(t, mockUserService, true)
	enforcer.On("Job", mock.Anything, mock.Anything, rbac.JobAction_View).Return(true, nil)
	enforcer.On("Job", mock.Anything, mock.Anything, rbac.JobAction_Execute).Return(false, nil)

	resp, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId: uuid.NewString(),
			Name:      "foo",
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
			Permissions: []mgmtv1alpha1.Permission{
				mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW,
				mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE,
			},
		}),
	)
	assert.Nil(t, resp)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.ErrorContains(t, err, "job:execute")
	mockQuerier.AssertNotCalled(t, "CreateAccountApiKey", mock.Anything, mock.Anything, mock.Anything)
}

func Test_Service_RegenerateAccountApiKey(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockIsUserInAccount(t, mockUserService, true)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockQuerier.On("UpdateAccountApiKeyValue", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)

	resp, err := svc.RegenerateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.RegenerateAccountApiKeyRequest{
			Id:        uuid.NewString(),
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Msg.ApiKey.KeyValue)
	assert.NotEqual(
		t,
		resp.Msg.ApiKey.KeyValue,
		rawData.KeyValue,
		"KeyValue return should be the clear text, not the hash",
	)
}

func Test_Service_RegenerateAccountApiKey_ForbiddenAccount(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockIsUserInAccount(t, mockUserService, false)
	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)

	resp, err := svc.RegenerateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.RegenerateAccountApiKeyRequest{
			Id:        uuid.NewString(),
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_RegenerateAccountApiKey_NotFound(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(db_queries.HusonymApiAccountApiKey{}, pgx.ErrNoRows)

	resp, err := svc.RegenerateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.RegenerateAccountApiKeyRequest{
			Id:        uuid.NewString(),
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_CreateAccountApiKey_ForbiddenAccount(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockIsUserInAccount(t, mockUserService, false)

	resp, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId: uuid.NewString(),
			Name:      "foo",
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_DeleteAccountApiKey_Existing(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, true)
	mockQuerier.On("RemoveAccountApiKey", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	resp, err := svc.DeleteAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.DeleteAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func Test_Service_DeleteAccountApiKey_Existing_ForbiddenAccount(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, false)

	resp, err := svc.DeleteAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.DeleteAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func Test_Service_DeleteAccountApiKey_NotFound(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(db_queries.HusonymApiAccountApiKey{}, pgx.ErrNoRows)

	resp, err := svc.DeleteAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.DeleteAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func Test_Service_DeleteAccountApiKey_Existing_DeleteRace(t *testing.T) {
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)

	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)

	rawData := db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(rawData, nil)
	mockIsUserInAccount(t, mockUserService, true)
	mockQuerier.On("RemoveAccountApiKey", mock.Anything, mock.Anything, mock.Anything).
		Return(pgx.ErrNoRows)

	resp, err := svc.DeleteAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.DeleteAccountApiKeyRequest{
			Id: uuid.NewString(),
		}),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// withoutApiKeys is the license of an instance that was sold everything but account API keys.
func withoutApiKeys() *testutil.FakeEELicense {
	return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureRbac, license.FeatureSso))
}

func anApiKeyRow(t *testing.T) db_queries.HusonymApiAccountApiKey {
	t.Helper()
	return db_queries.HusonymApiAccountApiKey{
		ID:          newPgUuid(t),
		AccountID:   newPgUuid(t),
		KeyValue:    "foo",
		CreatedByID: newPgUuid(t),
		UpdatedByID: newPgUuid(t),
		CreatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamp{Time: time.Now(), Valid: true},
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(24 * time.Hour), Valid: true},
		KeyName:     "foo",
	}
}

// Making a key is the api_keys feature. The refusal comes after the checks that were there and
// before anything is written: the mocks fail on a call nobody expected, the job check of the
// creator among them.
func Test_Service_CreateAccountApiKey_NeedsTheApiKeysFeature(t *testing.T) {
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)
	svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)
	mockUserUnder(t, mockUserService, withoutApiKeys(), true)

	resp, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId:   uuid.NewString(),
			Name:        "foo",
			ExpiresAt:   timestamppb.New(time.Now().Add(24 * time.Hour)),
			Permissions: []mgmtv1alpha1.Permission{mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW},
		}),
	)
	assert.Nil(t, resp)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.ErrorContains(t, err, "this license does not include api_keys")
}

func Test_Service_RegenerateAccountApiKey_NeedsTheApiKeysFeature(t *testing.T) {
	mockQuerier := db_queries.NewMockQuerier(t)
	mockUserService := userdata.NewMockInterface(t)
	svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)
	mockUserUnder(t, mockUserService, withoutApiKeys(), true)
	mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).
		Return(anApiKeyRow(t), nil)

	resp, err := svc.RegenerateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.RegenerateAccountApiKeyRequest{
			Id:        uuid.NewString(),
			ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.Nil(t, resp)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.ErrorContains(t, err, "this license does not include api_keys")
}

// A key that exists is looked at and taken away whatever the license includes: removing a key is
// never refused, nor is reading them.
func Test_Service_ExistingApiKeys_AreServedWithoutTheApiKeysFeature(t *testing.T) {
	row := anApiKeyRow(t)

	t.Run("deleting", func(t *testing.T) {
		mockQuerier := db_queries.NewMockQuerier(t)
		mockUserService := userdata.NewMockInterface(t)
		svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)
		mockUserUnder(t, mockUserService, withoutApiKeys(), true)
		mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).Return(row, nil)
		mockQuerier.On("RemoveAccountApiKey", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		resp, err := svc.DeleteAccountApiKey(
			context.Background(),
			connect.NewRequest(&mgmtv1alpha1.DeleteAccountApiKeyRequest{Id: uuid.NewString()}),
		)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("listing", func(t *testing.T) {
		mockQuerier := db_queries.NewMockQuerier(t)
		mockUserService := userdata.NewMockInterface(t)
		svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)
		mockUserUnder(t, mockUserService, withoutApiKeys(), true)
		mockQuerier.On("GetAccountApiKeys", mock.Anything, mock.Anything, mock.Anything).
			Return([]db_queries.HusonymApiAccountApiKey{row}, nil)

		resp, err := svc.GetAccountApiKeys(
			context.Background(),
			connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeysRequest{AccountId: uuid.NewString()}),
		)
		assert.NoError(t, err)
		assert.Len(t, resp.Msg.ApiKeys, 1)
	})

	t.Run("reading one", func(t *testing.T) {
		mockQuerier := db_queries.NewMockQuerier(t)
		mockUserService := userdata.NewMockInterface(t)
		svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), mockQuerier), mockUserService)
		mockUserUnder(t, mockUserService, withoutApiKeys(), true)
		mockQuerier.On("GetAccountApiKeyById", mock.Anything, mock.Anything, mock.Anything).Return(row, nil)

		resp, err := svc.GetAccountApiKey(
			context.Background(),
			connect.NewRequest(&mgmtv1alpha1.GetAccountApiKeyRequest{Id: uuid.NewString()}),
		)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

// A key made while the license included api_keys keeps authenticating, and keeps being served as
// the person it acts for, once the feature is closed: making keys is refused, and nothing is
// asked of the license on the way a key comes in -- the authentication takes no license at all.
func Test_ApiKey_StillAuthenticatesWithoutTheFeature(t *testing.T) {
	eelicense := testutil.NewFakeEELicense(testutil.WithIsValid())
	accountId := uuid.NewString()
	accountUuid, err := husonymdb.ToUuid(accountId)
	assert.NoError(t, err)

	// The key is made while the license includes every feature.
	mockDbtx := husonymdb.NewMockDBTX(t)
	mockQuerier := db_queries.NewMockQuerier(t)
	mockTx := pgxmock.NewMockTx(t)
	mockUserService := userdata.NewMockInterface(t)
	svc := New(&Config{}, husonymdb.New(mockDbtx, mockQuerier), mockUserService)
	mockUserUnder(t, mockUserService, eelicense, true).
		On("Job", mock.Anything, mock.Anything, rbac.JobAction_View).Return(true, nil)
	mockDbtx.On("Begin", mock.Anything).Return(mockTx, nil)
	mockTx.On("Commit", mock.Anything).Return(nil)
	mockTx.On("Rollback", mock.Anything).Return(nil)
	mockQuerier.On("CreateMachineUser", mock.Anything, mock.Anything, mock.Anything).
		Return(db_queries.HusonymApiUser{ID: newPgUuid(t), UserType: 1}, nil)
	var stored db_queries.CreateAccountApiKeyParams
	mockQuerier.On("CreateAccountApiKey", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { stored = args.Get(2).(db_queries.CreateAccountApiKeyParams) }).
		Return(anApiKeyRow(t), nil)
	created, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId:   accountId,
			Name:        "kept",
			ExpiresAt:   timestamppb.New(time.Now().Add(24 * time.Hour)),
			Permissions: []mgmtv1alpha1.Permission{mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW},
		}),
	)
	assert.NoError(t, err)
	clearKey := created.Msg.GetApiKey().GetKeyValue()

	eelicense.SetFeatures(license.FeatureRbac)

	// Making another is refused now.
	mockUserUnder(t, mockUserService, eelicense, true)
	refused, err := svc.CreateAccountApiKey(
		context.Background(),
		connect.NewRequest(&mgmtv1alpha1.CreateAccountApiKeyRequest{
			AccountId: accountId, Name: "refused", ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)),
		}),
	)
	assert.Nil(t, refused)
	assert.ErrorContains(t, err, "this license does not include api_keys")

	// The key made earlier comes in the way it did, through a client that has no license to ask.
	authenticator := auth_apikey.New(mockQuerier, mockDbtx, nil, nil)
	mockQuerier.On("GetAccountApiKeyByKeyValue", mock.Anything, mock.Anything, stored.KeyValue).
		Return(db_queries.HusonymApiAccountApiKey{
			ID:          newPgUuid(t),
			AccountID:   accountUuid,
			KeyValue:    stored.KeyValue,
			ExpiresAt:   stored.ExpiresAt,
			Permissions: stored.Permissions,
		}, nil)
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("mgmt.v1alpha1.JobService.GetJob")
	assert.NoError(t, err)
	ctx, err := authenticator.InjectTokenCtx(context.Background(), http.Header{
		"Authorization": []string{"Bearer " + clearKey},
	}, connect.Spec{Schema: descriptor, Procedure: "/mgmt.v1alpha1.JobService/GetJob"})
	assert.NoError(t, err)

	// And the user it makes is served as it was: the license is closed, and the key still acts.
	user, err := userdata.NewClient(userdatatest.Members{UserId: uuid.NewString()}, nil, eelicense).GetUser(ctx)
	assert.NoError(t, err)
	assert.True(t, user.IsApiKey())
	assert.NoError(t, user.EnforceJob(ctx, userdata.NewWildcardDomainEntity(accountId), rbac.JobAction_View))
}

func newPgUuid(t *testing.T) pgtype.UUID {
	t.Helper()
	newuuid := uuid.NewString()
	val, err := husonymdb.ToUuid(newuuid)
	assert.NoError(t, err)
	return val
}

// mockIsUserInAccount answers GetUser with a user of an instance whose license includes every
// feature.
func mockIsUserInAccount(
	t testing.TB,
	userServiceMock *userdata.MockInterface,
	isInAccount bool,
) *userdata.MockEntityEnforcer {
	return mockUserUnder(t, userServiceMock, testutil.NewFakeEELicense(testutil.WithIsValid()), isInAccount)
}

// mockUserUnder answers GetUser with a user of an instance running under the license.
func mockUserUnder(
	t testing.TB,
	userServiceMock *userdata.MockInterface,
	eelicense license.EEInterface,
	isInAccount bool,
) *userdata.MockEntityEnforcer {
	mockEntityEnforcer := userdata.NewMockEntityEnforcer(t)
	if isInAccount {
		mockEntityEnforcer.On("EnforceAccount", mock.Anything, mock.Anything, mock.Anything).
			Once().
			Return(nil)
	} else {
		mockEntityEnforcer.On("EnforceAccount", mock.Anything, mock.Anything, mock.Anything).
			Once().
			Return(errors.New("test: not in account"))
	}
	userServiceMock.On("GetUser", mock.Anything).Once().Return(
		userdatatest.NewUser(t, eelicense, mockEntityEnforcer), nil,
	)
	return mockEntityEnforcer
}
