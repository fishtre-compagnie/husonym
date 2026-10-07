package v1alpha_anonymizationservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/license"
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
		&refusalLog{},
	)
}

// refusalLog is a counter that remembers the gates it was asked to count, with their account.
type refusalLog struct{ counted []string }

func (l *refusalLog) CountRefusal(_ context.Context, accountId string, gates []license.Gate, _ time.Time) error {
	for _, gate := range gates {
		l.counted = append(l.counted, accountId+" "+string(gate))
	}
	return nil
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

// A license in force that lacks pii_text refuses the PII text on every path, and says which
// feature. The refusal is an error: the value is never handed back as it came, so a run that
// reaches PII text through a script fails rather than writing text that was not anonymized.
func Test_AnonymizeSingle_PiiTextNeedsItsOwnFeature(t *testing.T) {
	account := uuid.NewString()
	withoutPiiText := func() *testutil.FakeEELicense {
		return testutil.NewFakeEELicense(
			testutil.WithIsValid(),
			testutil.WithFeatures(license.FeatureCustomTransformers, license.FeaturePiiDetection),
		)
	}
	script := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{
			Code: `return husonym.transformPiiText(value, {});`,
		},
	}}

	for name, config := range map[string]*mgmtv1alpha1.TransformerConfig{
		"a mapping":              piiTextConfig(&mgmtv1alpha1.TransformPiiText{}),
		"a script that calls it": script,
	} {
		t.Run(name, func(t *testing.T) {
			// No answer set on Presidio: a call to it fails the test.
			s := service(t, withoutPiiText(), presidiotest.New(t), nil)

			out, err := anonymized(asTheWorker(), s, account, config, nil)

			// The script fails in the engine that runs it, which does not name a feature: what
			// counts is that the call fails and returns nothing.
			require.Error(t, err)
			require.Empty(t, out, "the input is never passed through")
		})
	}

	t.Run("a transformer that is not PII text is served without the feature", func(t *testing.T) {
		s := service(t, withoutPiiText(), presidiotest.New(t), nil)
		passthrough := &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
		}

		out, err := anonymized(asTheWorker(), s, account, passthrough, nil)

		require.NoError(t, err)
		require.Equal(t, "appeler Zoé demain", out)
	})
}

// A mapping that is a PII text is refused with the code the refusal always had, and the message
// names the feature.
func Test_AnonymizeSingle_PiiTextRefusalCode(t *testing.T) {
	s := service(t, testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures()), presidiotest.New(t), nil)

	_, err := anonymized(asTheWorker(), s, uuid.NewString(), piiTextConfig(&mgmtv1alpha1.TransformPiiText{}), nil)

	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, "this license does not include pii_text")
}

// AnonymizeMany is refused whole to a license without pii_text, with the code it always had, and
// the message names the feature.
func Test_AnonymizeMany_NeedsThePiiTextFeature(t *testing.T) {
	for name, tc := range map[string]struct {
		eelicense *testutil.FakeEELicense
		refusal   string
		gate      license.Gate
	}{
		"a license that lacks pii_text": {
			eelicense: testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureMcp)),
			refusal:   "this license does not include pii_text",
			gate:      license.FeatureGate(license.FeaturePiiText),
		},
		// No feature is included then: the refusal says that no license is in force, not that
		// this one is missing from it.
		"a license that is not in force": {
			eelicense: testutil.NewFakeEELicense(),
			refusal:   "account does not have an active license",
			gate:      license.GateNotInForce,
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := service(t, tc.eelicense, presidiotest.New(t), nil)
			accountId := uuid.NewString()

			resp, err := s.AnonymizeMany(context.Background(), connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
				AccountId: accountId,
			}))

			require.Nil(t, resp)
			require.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, tc.refusal)
			require.Equal(t, 1, strings.Count(err.Error(), "license"), "one cause is told: %v", err)
			require.Equal(t, []string{accountId + " " + string(tc.gate)}, s.refusals.(*refusalLog).counted)
		})
	}
}

// Under a license that is not in force, AnonymizeSingle answers what every gated call answers
// then, whichever feature the request would have needed.
func Test_AnonymizeSingle_WithoutALicenseInForceSaysSo(t *testing.T) {
	script := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return "x";`},
	}}
	for name, config := range map[string]*mgmtv1alpha1.TransformerConfig{
		"a PII text":           piiTextConfig(&mgmtv1alpha1.TransformPiiText{}),
		"a custom transformer": script,
	} {
		t.Run(name, func(t *testing.T) {
			s := service(t, testutil.NewFakeEELicense(), presidiotest.New(t), nil)

			_, err := anonymized(asTheWorker(), s, uuid.NewString(), config, nil)

			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "account does not have an active license")
			require.NotContains(t, err.Error(), "does not include")
		})
	}
}

// A counter that cannot count changes nothing to what AnonymizeMany answers.
func Test_AnonymizeMany_ACounterThatFailsChangesNothing(t *testing.T) {
	s := service(t, testutil.NewFakeEELicense(), presidiotest.New(t), nil)
	counter := &failingCounter{}
	s.refusals = counter

	resp, err := s.AnonymizeMany(context.Background(), connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
		AccountId: uuid.NewString(),
	}))

	require.Nil(t, resp)
	require.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, "account does not have an active license")
	require.Equal(t, 1, counter.asked)
}

type failingCounter struct{ asked int }

func (c *failingCounter) CountRefusal(context.Context, string, []license.Gate, time.Time) error {
	c.asked++
	return errors.New("the usage database is down")
}

// strangers answers for a user account service whose caller is a member of no account.
type strangers struct{ accounts }

func (strangers) IsUserInAccount(
	context.Context, *connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: false}), nil
}

// A refusal is counted against an account the caller may reach: a caller that is not a member of
// the account gets the answer of the access check, and nothing is counted.
func Test_AnonymizeMany_DoesNotCountAnAccountTheCallerCannotReach(t *testing.T) {
	s := service(t, testutil.NewFakeEELicense(), presidiotest.New(t), nil)
	users := strangers{accounts{userId: uuid.NewString()}}
	s.userdataclient = userdata.NewClient(users, nil, testutil.NewFakeEELicense())

	resp, err := s.AnonymizeMany(context.Background(), connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
		AccountId: uuid.NewString(),
	}))

	require.Nil(t, resp)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.Empty(t, s.refusals.(*refusalLog).counted)
}
