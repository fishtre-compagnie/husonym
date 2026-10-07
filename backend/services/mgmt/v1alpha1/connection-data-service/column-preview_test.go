package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	jsonanonymizer "github.com/fishtre-compagnie/husonym/internal/json-anonymizer"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_previewValues(t *testing.T) {
	t.Run("a constant shows as fewer distinct values after than before", func(t *testing.T) {
		// The signal the preview exists to give: three logins become one, which a unique index
		// would refuse on the second row. The run would only have said so after minutes.
		resp := previewValues(
			[]any{"alice", "bruno", "chloe"},
			func(any) (any, error) { return "demo", nil },
		)
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "demo", resp.GetValues()[0].GetOutput().GetValue())
	})

	t.Run("a failure is reported against its value and does not stop the others", func(t *testing.T) {
		resp := previewValues(
			[]any{"ok", "boom", "ok too"},
			func(v any) (any, error) {
				if v == "boom" {
					return nil, errors.New("cannot transform")
				}
				return v, nil
			},
		)
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "cannot transform", resp.GetValues()[1].GetError())
		require.Nil(t, resp.GetValues()[1].GetOutput())
		require.Equal(t, "ok too", resp.GetValues()[2].GetOutput().GetValue())
		// The failed value produced nothing, so it counts on one side only.
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(2), resp.GetDistinctOutputs())
	})

	t.Run("nulls are shown and left out of the counts", func(t *testing.T) {
		resp := previewValues(
			[]any{nil, "x"},
			func(v any) (any, error) { return v, nil },
		)
		require.True(t, resp.GetValues()[0].GetInput().GetIsNull())
		require.True(t, resp.GetValues()[0].GetOutput().GetIsNull())
		require.Equal(t, uint32(1), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
	})
}

func Test_transformValue(t *testing.T) {
	// Through the real anonymizer, so the wrapping — {"value": ...} and the .value expression —
	// is what is under test, not a stand-in for it.
	anonymizer, err := jsonanonymizer.NewAnonymizer(
		context.Background(),
		jsonanonymizer.WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression: ".value",
			Transformer: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
					TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{
						Code: `return value.toUpperCase();`,
					},
				},
			},
		}}),
	)
	require.NoError(t, err)

	out, err := transformValue(anonymizer, "alice")
	require.NoError(t, err)
	require.Equal(t, "ALICE", out)
}

func engineOf(t *testing.T, analyzer presidio.Analyzer) *piitext.Engine {
	t.Helper()
	engine, err := piitext.NewEngine(analyzer, "")
	require.NoError(t, err)
	return engine
}

func Test_previewAnonymized_PiiTextNeedsAValidLicense(t *testing.T) {
	piiText := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
			TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{},
		},
	}
	raws := []any{"Hello, John Doe!"}
	logger := testutil.GetTestLogger(t)

	t.Run("under a lapsed license the transformer is not enabled and Presidio is not called", func(t *testing.T) {
		// No answer set on Presidio: a call to it fails the test.
		presidioFake := presidiotest.New(t)
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidioFake),
			License: testutil.NewFakeEELicense(),
		}}

		resp, err := s.previewAnonymized(context.Background(), "an-account", raws, piiText, nil, logger)

		// What a deployment without Presidio answers: the transformer cannot be built.
		require.Nil(t, resp)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
	})

	t.Run("under a valid license that lacks pii_text the transformer is not enabled and Presidio is not called", func(t *testing.T) {
		presidioFake := presidiotest.New(t)
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidioFake),
			License: testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureCustomTransformers)),
		}}

		resp, err := s.previewAnonymized(context.Background(), "an-account", raws, piiText, nil, logger)

		require.Nil(t, resp)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
	})

	t.Run("under a valid license the transformer runs", func(t *testing.T) {
		anonymized := "Hello, <PERSON>!"
		presidioFake := presidiotest.Finding(t, "PERSON", "John Doe")
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidioFake),
			License: testutil.NewFakeEELicense(testutil.WithIsValid()),
		}}

		resp, err := s.previewAnonymized(context.Background(), "an-account", raws, piiText, nil, logger)

		require.NoError(t, err)
		require.Len(t, resp.GetValues(), 1)
		require.Empty(t, resp.GetValues()[0].GetError())
		require.Equal(t, anonymized, resp.GetValues()[0].GetOutput().GetValue())
		require.Equal(t, presidiotest.Calls{Analyze: 1}, presidioFake.Calls())
	})

	t.Run("the license is read on every preview", func(t *testing.T) {
		eelicense := testutil.NewFakeEELicense(testutil.WithIsValid())
		presidioFake := presidiotest.Finding(t, "PERSON", "John Doe")
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidioFake),
			License: eelicense,
		}}

		_, err := s.previewAnonymized(context.Background(), "an-account", raws, piiText, nil, logger)
		require.NoError(t, err)

		eelicense.SetValid(false)
		_, err = s.previewAnonymized(context.Background(), "an-account", raws, piiText, nil, logger)
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
		// Presidio was called for the first preview only.
		require.Equal(t, presidiotest.Calls{Analyze: 1}, presidioFake.Calls())
	})

	t.Run("a user-defined transformer that stores it needs the license too", func(t *testing.T) {
		resolver := transformer_executor.NewMockUserDefinedTransformerResolver(t)
		resolver.On("GetUserDefinedTransformer", mock.Anything, "stored").Return(piiText, nil)
		userDefined := &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
				UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "stored"},
			},
		}
		eelicense := testutil.NewFakeEELicense(testutil.WithIsValid())
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidiotest.Finding(t, "PERSON", "John Doe")),
			License: eelicense,
		}}

		resp, err := s.previewAnonymized(context.Background(), "an-account", raws, userDefined, resolver, logger)
		require.NoError(t, err)
		require.Equal(t, "Hello, <PERSON>!", resp.GetValues()[0].GetOutput().GetValue())

		eelicense.SetValid(false)
		_, err = s.previewAnonymized(context.Background(), "an-account", raws, userDefined, resolver, logger)
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
	})

	t.Run("two accounts are shown two hashes for the same text, and one account the same", func(t *testing.T) {
		algo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
		hashing := &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
				TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
					Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo}},
				}},
			},
		}
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidiotest.Finding(t, "PERSON", "John Doe")),
			License: testutil.NewFakeEELicense(testutil.WithIsValid()),
		}}
		shown := func(accountId string) string {
			resp, err := s.previewAnonymized(context.Background(), accountId, raws, hashing, nil, logger)
			require.NoError(t, err)
			return resp.GetValues()[0].GetOutput().GetValue()
		}
		require.Equal(t, shown("account-a"), shown("account-a"))
		require.NotEqual(t, shown("account-a"), shown("account-b"))
	})

	t.Run("another transformer does not look at the license", func(t *testing.T) {
		presidioFake := presidiotest.New(t)
		s := &Service{cfg: &Config{}, transformers: Transformers{
			PiiText: engineOf(t, presidioFake),
			License: testutil.NewFakeEELicense(),
		}}
		passthrough := &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
				PassthroughConfig: &mgmtv1alpha1.Passthrough{},
			},
		}

		resp, err := s.previewAnonymized(context.Background(), "an-account", raws, passthrough, nil, logger)

		require.NoError(t, err)
		require.Len(t, resp.GetValues(), 1)
		require.Equal(t, "Hello, John Doe!", resp.GetValues()[0].GetOutput().GetValue())
	})
}

// The preview runs the transformer it is given. Without the feature of that transformer the call
// is refused, naming the feature, before a user-defined transformer is resolved and before a row
// is read: the transformers client is nil and the builder expects no call, so either would fail
// the test.
func Test_PreviewColumnTransformer_NeedsTheFeatureOfItsTransformer(t *testing.T) {
	javascript := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return "x";`},
	}}
	generate := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateJavascriptConfig{
		GenerateJavascriptConfig: &mgmtv1alpha1.GenerateJavascript{Code: `return "x";`},
	}}
	userDefined := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "stored"},
	}}
	piiText := func(nested *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.TransformerConfig {
		config := &mgmtv1alpha1.TransformPiiText{}
		if nested != nil {
			config.DefaultAnonymizer = &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
				Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: nested},
			}}
		}
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: config},
		}
	}
	inForceWith := func(features ...license.Feature) *testutil.FakeEELicense {
		return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(features...))
	}

	preview := func(
		t *testing.T, eelicense license.EEInterface, engine *piitext.Engine, transformer *mgmtv1alpha1.TransformerConfig,
	) error {
		t.Helper()
		connections := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
		connections.EXPECT().GetConnection(mock.Anything, mock.Anything).Return(
			connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: &mgmtv1alpha1.Connection{Id: "c1"}}), nil,
		)
		s := &Service{
			cfg:                   &Config{},
			connectionService:     connections,
			connectiondatabuilder: connectiondata.NewMockConnectionDataBuilder(t),
			transformers:          Transformers{PiiText: engine, License: eelicense},
		}
		resp, err := s.PreviewColumnTransformer(t.Context(), connect.NewRequest(
			&mgmtv1alpha1.PreviewColumnTransformerRequest{
				ConnectionId: "c1", Schema: "public", Table: "clients", Column: "name", Transformer: transformer,
			},
		))
		require.Nil(t, resp)
		return err
	}

	for name, transformer := range map[string]*mgmtv1alpha1.TransformerConfig{
		"a JavaScript transform":                 javascript,
		"a JavaScript generate":                  generate,
		"a user-defined transformer":             userDefined,
		"a PII text that hands to JavaScript":    piiText(javascript),
		"a PII text that hands to a user's rule": piiText(userDefined),
	} {
		t.Run(name+" needs custom_transformers", func(t *testing.T) {
			err := preview(t, inForceWith(license.FeaturePiiText), engineOf(t, presidiotest.New(t)), transformer)

			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "this license does not include custom_transformers")
		})
	}

	t.Run("a PII text needs pii_text, and the refusal names it", func(t *testing.T) {
		err := preview(t, inForceWith(license.FeatureCustomTransformers), engineOf(t, presidiotest.New(t)), piiText(nil))

		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
		require.ErrorContains(t, err, "this license does not include pii_text")
	})

	t.Run("under a license that is not in force the refusal says so, not that a feature is missing", func(t *testing.T) {
		for _, transformer := range []*mgmtv1alpha1.TransformerConfig{javascript, piiText(nil)} {
			err := preview(t, testutil.NewFakeEELicense(), engineOf(t, presidiotest.New(t)), transformer)

			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "account does not have an active license")
			require.NotContains(t, err.Error(), "does not include")
		}
	})
}

// A deployment without the engine of PII text cannot run it whatever the license says: that
// cause keeps its own message, which the license does not replace.
func Test_previewAnonymized_PiiTextWithoutAnEngineSaysItIsNotEnabled(t *testing.T) {
	piiText := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{}},
	}
	s := &Service{cfg: &Config{}, transformers: Transformers{License: testutil.NewFakeEELicense(testutil.WithIsValid())}}

	require.NoError(t, s.refusePiiTextPreview(piiText), "the license is not the cause")
	resp, err := s.previewAnonymized(t.Context(), "an-account", []any{"Hello"}, piiText, nil, testutil.GetTestLogger(t))

	require.Nil(t, resp)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.ErrorContains(t, err, "TransformPiiText is not enabled")
}

// An age written as a JSON number — "age":28 — and not as a string — "age":"28".
var unquotedNumber = regexp.MustCompile(`"age":[0-9]+`)

func Test_previewJavascript(t *testing.T) {
	sampled := &sampledTable{
		accountId: "22222222-2222-2222-2222-222222222222",
		rows: []map[string]any{
			{"age": int64(28), "name": "Alice"},
			{"age": int64(31), "name": "Bruno"},
		},
	}
	config := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
			TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return value + 1;`},
		},
	}

	t.Run("one trial for the whole sample, numbers as numbers", func(t *testing.T) {
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().
			TryJavascriptRules(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest]) bool {
					// Every row in the same trial: one consistency key, so a deterministic rule
					// gives the same value the same output across the preview, as in a run. And
					// each row whole, with its numbers unquoted.
					rows := req.Msg.GetRows()
					return len(rows) == 2 &&
						unquotedNumber.MatchString(rows[0]) &&
						strings.Contains(rows[1], `"name":"Bruno"`) &&
						req.Msg.GetAccountId() == sampled.accountId
				},
			)).
			Return(connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{Rows: []string{
				`{"age":29,"name":"Alice"}`,
				`{"age":32,"name":"Bruno"}`,
			}}), nil).
			Once()

		s := &Service{transformers: Transformers{Client: client}}
		resp := s.previewJavascript(context.Background(), sampled, "age", []any{int64(28), int64(31)}, config)

		require.Len(t, resp.GetValues(), 2)
		// 28 + 1 = 29, as the run computes it — not "281".
		require.Equal(t, "29", resp.GetValues()[0].GetOutput().GetValue())
		require.Equal(t, "32", resp.GetValues()[1].GetOutput().GetValue())
	})

	t.Run("a failure stops the trial, and the others say so instead of guessing", func(t *testing.T) {
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().
			TryJavascriptRules(mock.Anything, mock.Anything).
			Return(connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{
				Failure: &mgmtv1alpha1.JavascriptRuleFailure{Row: 0, Column: "age", Message: "TypeError: boom"},
			}), nil).
			Once()

		s := &Service{transformers: Transformers{Client: client}}
		resp := s.previewJavascript(context.Background(), sampled, "age", []any{int64(28), int64(31)}, config)

		require.Equal(t, "TypeError: boom", resp.GetValues()[0].GetError())
		require.Contains(t, resp.GetValues()[1].GetError(), "not tried")
		require.Nil(t, resp.GetValues()[1].GetOutput())
	})

	t.Run("never more rows than one trial takes", func(t *testing.T) {
		many := &sampledTable{accountId: sampled.accountId}
		raws := make([]any, 0, 30)
		for i := range 30 {
			many.rows = append(many.rows, map[string]any{"age": int64(i)})
			raws = append(raws, int64(i))
		}
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().
			TryJavascriptRules(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest]) bool {
					return len(req.Msg.GetRows()) == maxJavascriptTrialRows
				},
			)).
			RunAndReturn(func(
				_ context.Context,
				req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest],
			) (*connect.Response[mgmtv1alpha1.TryJavascriptRulesResponse], error) {
				return connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{Rows: req.Msg.GetRows()}), nil
			}).
			Once()

		s := &Service{transformers: Transformers{Client: client}}
		resp := s.previewJavascript(context.Background(), many, "age", raws, config)
		require.Len(t, resp.GetValues(), maxJavascriptTrialRows)
	})
}
