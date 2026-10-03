package v1alpha_anonymizationservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// accounts answers for the user account service: every caller is a user, a member of every
// account, and every account may anonymize.
type accounts struct {
	mgmtv1alpha1connect.UserAccountServiceClient
	userId string
}

func (a accounts) GetUser(
	context.Context, *connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: a.userId}), nil
}

func (a accounts) IsUserInAccount(
	context.Context, *connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: true}), nil
}

func (a accounts) IsAccountStatusValid(
	context.Context, *connect.Request[mgmtv1alpha1.IsAccountStatusValidRequest],
) (*connect.Response[mgmtv1alpha1.IsAccountStatusValidResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsAccountStatusValidResponse{IsValid: true}), nil
}

// service is an anonymization service with authentication on, whose analyzer finds "Zoé" as a
// person, under the given license. The transformers service is the given one.
func service(
	t *testing.T,
	eelicense *testutil.FakeEELicense,
	analyzer presidio.Analyzer,
	transformers mgmtv1alpha1connect.TransformersServiceClient,
) *Service {
	t.Helper()
	engine, err := piitext.NewEngine(analyzer, "")
	require.NoError(t, err)
	users := accounts{userId: uuid.NewString()}
	return New(
		&Config{IsAuthEnabled: true, WorkerOnly: userdata.WorkerOnly{IsAuthEnabled: true}},
		nil,
		userdata.NewClient(users, nil, eelicense),
		users,
		transformers,
		engine,
		nil,
		eelicense,
	)
}

func licensed() *testutil.FakeEELicense {
	return testutil.NewFakeEELicense(testutil.WithIsValid())
}

// asTheWorker is the context of a call that carries the key of the worker.
func asTheWorker() context.Context {
	return auth_apikey.SetTokenData(context.Background(), &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey})
}

func mapping(config *mgmtv1alpha1.TransformerConfig) []*mgmtv1alpha1.TransformerMapping {
	return []*mgmtv1alpha1.TransformerMapping{{Expression: ".note", Transformer: config}}
}

func piiTextConfig(config *mgmtv1alpha1.TransformPiiText) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: config},
	}
}

func hashing() *mgmtv1alpha1.TransformerConfig {
	algo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
	return piiTextConfig(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
		Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo}},
	}})
}

// anonymized is the note AnonymizeSingle answers for an account, with or without the header of
// a hash key.
func anonymized(
	ctx context.Context,
	s *Service,
	accountId string,
	config *mgmtv1alpha1.TransformerConfig,
	key *piitext.HashKey,
) (string, error) {
	req := connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
		AccountId:           accountId,
		InputData:           `{"note":"appeler Zoé demain"}`,
		TransformerMappings: mapping(config),
	})
	if key != nil {
		req.Header().Set(piitext.HashKeyHeader, key.Encode())
	}
	resp, err := s.AnonymizeSingle(ctx, req)
	if err != nil {
		return "", err
	}
	var out struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(resp.Msg.GetOutputData()), &out); err != nil {
		return "", err
	}
	return out.Note, nil
}

func Test_AnonymizeSingle_PiiTextHashKey(t *testing.T) {
	s := service(t, licensed(), presidiotest.Finding(t, "PERSON", "Zoé"), nil)
	account, otherAccount := uuid.NewString(), uuid.NewString()
	user := context.Background()
	key := &piitext.HashKey{1, 2, 3}

	hash := func(t *testing.T, ctx context.Context, accountId string, key *piitext.HashKey) string {
		t.Helper()
		out, err := anonymized(ctx, s, accountId, hashing(), key)
		require.NoError(t, err)
		require.Regexp(t, `^appeler [0-9a-f]{64} demain$`, out)
		return out
	}

	t.Run("the worker gets the hashes of the key it hands, whatever the account", func(t *testing.T) {
		require.Equal(t, hash(t, asTheWorker(), account, key), hash(t, asTheWorker(), account, key))
		require.Equal(t, hash(t, asTheWorker(), account, key), hash(t, asTheWorker(), otherAccount, key))
		require.NotEqual(t, hash(t, asTheWorker(), account, key), hash(t, asTheWorker(), account, &piitext.HashKey{9}))
	})

	t.Run("the header of a caller that is not the worker is not read", func(t *testing.T) {
		require.Equal(t, hash(t, user, account, nil), hash(t, user, account, key))
		require.NotEqual(t, hash(t, asTheWorker(), account, key), hash(t, user, account, key))
	})

	t.Run("outside a run an account keeps its hashes for as long as the process lives", func(t *testing.T) {
		require.Equal(t, hash(t, user, account, nil), hash(t, user, account, nil))
		require.Equal(t, hash(t, user, account, nil), hash(t, asTheWorker(), account, nil))
	})

	t.Run("outside a run two accounts get two hashes for the same text", func(t *testing.T) {
		require.NotEqual(t, hash(t, user, account, nil), hash(t, user, otherAccount, nil))
	})
}

// The key a run hands is a secret of its scope: nothing the service logs carries it, at any
// level, whether the request succeeds or fails.
func Test_AnonymizeSingle_TheHashKeyIsNotLogged(t *testing.T) {
	key := &piitext.HashKey{4, 5, 6}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := logger_interceptor.SetLoggerContext(asTheWorker(), logger)

	s := service(t, licensed(), presidiotest.Finding(t, "PERSON", "Zoé"), nil)
	_, err := anonymized(ctx, s, uuid.NewString(), hashing(), key)
	require.NoError(t, err)

	down := presidiotest.New(t)
	down.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return nil, errors.Join(presidio.ErrNoAnswer, errors.New("connection refused"))
	})
	_, err = anonymized(ctx, service(t, licensed(), down, nil), uuid.NewString(), hashing(), key)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.NotContains(t, err.Error(), key.Encode())

	require.NotEmpty(t, logs.String(), "the failure is logged")
	require.NotContains(t, logs.String(), key.Encode())
}

// Executing the PII text transformer takes a valid license, however the request reaches it:
// named in a mapping, stored in a user-defined transformer, or called by a script.
func Test_AnonymizeSingle_PiiTextNeedsALicenseOnEveryPath(t *testing.T) {
	account := uuid.NewString()
	script := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{
			Code: `return husonym.transformPiiText(value, {});`,
		},
	}}
	userDefined := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "stored"},
	}}
	// stored is the transformers service of an account that stored a PII text transformer.
	stored := func(t *testing.T) mgmtv1alpha1connect.TransformersServiceClient {
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().GetUserDefinedTransformerById(mock.Anything, mock.Anything).
			Return(connect.NewResponse(&mgmtv1alpha1.GetUserDefinedTransformerByIdResponse{
				Transformer: &mgmtv1alpha1.UserDefinedTransformer{
					Id:        "stored",
					AccountId: account,
					Config:    piiTextConfig(&mgmtv1alpha1.TransformPiiText{}),
				},
			}), nil).Maybe()
		return client
	}

	for name, config := range map[string]*mgmtv1alpha1.TransformerConfig{
		"a mapping":                  piiTextConfig(&mgmtv1alpha1.TransformPiiText{}),
		"a user-defined transformer": userDefined,
		"a script that calls it":     script,
	} {
		t.Run(name+" runs under a valid license", func(t *testing.T) {
			s := service(t, licensed(), presidiotest.Finding(t, "PERSON", "Zoé"), stored(t))
			out, err := anonymized(context.Background(), s, account, config, nil)
			require.NoError(t, err)
			require.Equal(t, "appeler <PERSON> demain", out)
		})

		t.Run(name+" is refused without a valid license, and the analyzer is not called", func(t *testing.T) {
			// No answer set on Presidio: a call to it fails the test.
			s := service(t, testutil.NewFakeEELicense(), presidiotest.New(t), stored(t))
			out, err := anonymized(context.Background(), s, account, config, nil)
			require.Error(t, err)
			require.Empty(t, out)
		})
	}
}
