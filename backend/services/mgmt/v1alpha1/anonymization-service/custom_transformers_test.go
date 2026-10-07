package v1alpha_anonymizationservice

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func transformJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func generateJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateJavascriptConfig{
		GenerateJavascriptConfig: &mgmtv1alpha1.GenerateJavascript{Code: code},
	}}
}

func userDefinedReference(id string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: id},
	}}
}

// nestedInPiiText is a PII text that hands what it finds to the given transformer.
func nestedInPiiText(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.TransformerConfig {
	return piiTextConfig(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
		Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: config}},
	}})
}

// withoutCustomTransformers is a license in force that includes everything else.
func withoutCustomTransformers() *testutil.FakeEELicense {
	var others []license.Feature
	for _, feature := range license.AllFeatures() {
		if feature != license.FeatureCustomTransformers {
			others = append(others, feature)
		}
	}
	return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(others...))
}

// The anonymization API executes the transformers of the request. A JavaScript transform, a
// JavaScript generate and a user-defined transformer are custom_transformers wherever the
// request carries them: in a mapping, as a default transformer, or among the anonymizers of a
// PII text. Without the feature none of them runs: no script is executed and no user-defined
// transformer is resolved (the transformers client is nil, and Presidio fails the test on a call).
func Test_Anonymize_CustomTransformersNeedTheirFeature(t *testing.T) {
	kinds := map[string]*mgmtv1alpha1.TransformerConfig{
		"a JavaScript transform":     transformJavascript(`return "x";`),
		"a JavaScript generate":      generateJavascript(`return "x";`),
		"a user-defined transformer": userDefinedReference(uuid.NewString()),
	}
	places := map[string]func(*mgmtv1alpha1.TransformerConfig) ([]*mgmtv1alpha1.TransformerMapping, *mgmtv1alpha1.DefaultTransformersConfig){
		"in a mapping": func(c *mgmtv1alpha1.TransformerConfig) ([]*mgmtv1alpha1.TransformerMapping, *mgmtv1alpha1.DefaultTransformersConfig) {
			return mapping(c), nil
		},
		"nested in the PII text of a mapping": func(c *mgmtv1alpha1.TransformerConfig) ([]*mgmtv1alpha1.TransformerMapping, *mgmtv1alpha1.DefaultTransformersConfig) {
			return mapping(nestedInPiiText(c)), nil
		},
		"as a default transformer": func(c *mgmtv1alpha1.TransformerConfig) ([]*mgmtv1alpha1.TransformerMapping, *mgmtv1alpha1.DefaultTransformersConfig) {
			return nil, &mgmtv1alpha1.DefaultTransformersConfig{S: c}
		},
		"nested in the PII text of a default transformer": func(c *mgmtv1alpha1.TransformerConfig) ([]*mgmtv1alpha1.TransformerMapping, *mgmtv1alpha1.DefaultTransformersConfig) {
			return nil, &mgmtv1alpha1.DefaultTransformersConfig{N: nestedInPiiText(c)}
		},
	}

	for kind, config := range kinds {
		for place, put := range places {
			mappings, defaults := put(config)

			t.Run("AnonymizeSingle refuses "+kind+" "+place, func(t *testing.T) {
				s := service(t, withoutCustomTransformers(), presidiotest.New(t), nil)

				resp, err := s.AnonymizeSingle(asTheWorker(), connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
					AccountId:           uuid.NewString(),
					InputData:           `{"note":"appeler Zoé demain"}`,
					TransformerMappings: mappings,
					DefaultTransformers: defaults,
				}))

				require.Nil(t, resp)
				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
				require.ErrorContains(t, err, "this license does not include custom_transformers")
			})

			t.Run("AnonymizeMany refuses "+kind+" "+place, func(t *testing.T) {
				s := service(t, withoutCustomTransformers(), presidiotest.New(t), nil)

				resp, err := s.AnonymizeMany(context.Background(), connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
					AccountId:           uuid.NewString(),
					InputData:           []string{`{"note":"appeler Zoé demain"}`},
					TransformerMappings: mappings,
					DefaultTransformers: defaults,
				}))

				require.Nil(t, resp)
				// The code AnonymizeMany answers a license that lacks pii_text with.
				require.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err), "%v", err)
				require.ErrorContains(t, err, "this license does not include custom_transformers")
			})
		}
	}

	t.Run("a transformer that is not custom is served without the feature", func(t *testing.T) {
		s := service(t, withoutCustomTransformers(), presidiotest.Finding(t, "PERSON", "Zoé"), nil)

		out, err := anonymized(asTheWorker(), s, uuid.NewString(), piiTextConfig(&mgmtv1alpha1.TransformPiiText{}), nil)

		require.NoError(t, err)
		require.Equal(t, "appeler <PERSON> demain", out)
	})
}

func Test_Anonymize_CustomTransformersRunWithTheirFeature(t *testing.T) {
	shout := transformJavascript(`return value.toUpperCase();`)

	t.Run("AnonymizeSingle", func(t *testing.T) {
		s := service(t, licensed(), presidiotest.New(t), nil)

		out, err := anonymized(context.Background(), s, uuid.NewString(), shout, nil)

		require.NoError(t, err)
		require.Equal(t, "APPELER ZOÉ DEMAIN", out)
	})

	t.Run("AnonymizeMany", func(t *testing.T) {
		s := service(t, licensed(), presidiotest.New(t), nil)
		querier := db_queries.NewMockQuerier(t)
		querier.EXPECT().GetAccount(mock.Anything, mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiAccount{AccountType: int16(husonymdb.AccountType_Team)}, nil).Once()
		s.db = husonymdb.New(husonymdb.NewMockDBTX(t), querier)

		resp, err := s.AnonymizeMany(context.Background(), connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
			AccountId:           uuid.NewString(),
			InputData:           []string{`{"note":"appeler Zoé demain"}`},
			TransformerMappings: mapping(shout),
		}))

		require.NoError(t, err)
		require.Empty(t, resp.Msg.GetErrors())
		require.JSONEq(t, `{"note":"APPELER ZOÉ DEMAIN"}`, resp.Msg.GetOutputData()[0])
	})
}

// A default transformer that is a PII text needs pii_text like a mapped one: the three kinds of
// default each have their own branch.
func Test_AnonymizeSingle_ADefaultPiiTextNeedsItsFeature(t *testing.T) {
	piiText := piiTextConfig(&mgmtv1alpha1.TransformPiiText{})
	withoutPiiText := func() *testutil.FakeEELicense {
		return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureCustomTransformers))
	}

	for name, defaults := range map[string]*mgmtv1alpha1.DefaultTransformersConfig{
		"for booleans": {Boolean: piiText},
		"for numbers":  {N: piiText},
		"for strings":  {S: piiText},
	} {
		t.Run(name, func(t *testing.T) {
			// No answer set on Presidio: a call to it fails the test.
			s := service(t, withoutPiiText(), presidiotest.New(t), nil)

			resp, err := s.AnonymizeSingle(asTheWorker(), connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
				AccountId:           uuid.NewString(),
				InputData:           `{"note":"appeler Zoé demain"}`,
				DefaultTransformers: defaults,
			}))

			require.Nil(t, resp)
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "this license does not include pii_text")
		})
	}
}
