package integrationtests_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// transformersGround is a personal account of the expiring mode, whose license a test restricts.
type transformersGround struct {
	accountId    string
	transformers mgmtv1alpha1connect.TransformersServiceClient
	anonymize    mgmtv1alpha1connect.AnonymizationServiceClient
}

func (s *IntegrationTestSuite) newTransformersGround(name string) *transformersGround {
	userOpt := integrationtests_test.WithUserId(name)
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	s.T().Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
	s.setUser(s.ctx, users)
	return &transformersGround{
		accountId:    s.createPersonalAccount(s.ctx, users),
		transformers: s.OSSAuthenticatedExpiringClients.Transformers(userOpt),
		anonymize:    s.OSSAuthenticatedExpiringClients.Anonymize(userOpt),
	}
}

func javascriptConfig(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func piiTextTransformerConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
		TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{},
	}}
}

func (g *transformersGround) create(
	s *IntegrationTestSuite,
	name string,
	source mgmtv1alpha1.TransformerSource,
	config *mgmtv1alpha1.TransformerConfig,
) (*connect.Response[mgmtv1alpha1.CreateUserDefinedTransformerResponse], error) {
	return g.transformers.CreateUserDefinedTransformer(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateUserDefinedTransformerRequest{
		AccountId:         g.accountId,
		Name:              name,
		Source:            source,
		TransformerConfig: config,
	}))
}

func (g *transformersGround) update(
	s *IntegrationTestSuite,
	transformer *mgmtv1alpha1.UserDefinedTransformer,
	config *mgmtv1alpha1.TransformerConfig,
) (*connect.Response[mgmtv1alpha1.UpdateUserDefinedTransformerResponse], error) {
	return g.transformers.UpdateUserDefinedTransformer(s.ctx, connect.NewRequest(&mgmtv1alpha1.UpdateUserDefinedTransformerRequest{
		TransformerId:     transformer.GetId(),
		Name:              transformer.GetName(),
		TransformerConfig: config,
	}))
}

func (g *transformersGround) try(s *IntegrationTestSuite) (*connect.Response[mgmtv1alpha1.TryJavascriptRulesResponse], error) {
	return g.transformers.TryJavascriptRules(s.ctx, connect.NewRequest(&mgmtv1alpha1.TryJavascriptRulesRequest{
		AccountId: g.accountId,
		Rules:     []*mgmtv1alpha1.JavascriptRule{{Column: "name", Transformer: javascriptConfig(`return "x";`)}},
		Rows:      []string{`{"name": "Ada"}`},
	}))
}

func (g *transformersGround) storedCount(s *IntegrationTestSuite) int {
	resp, err := g.transformers.GetUserDefinedTransformers(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserDefinedTransformersRequest{
		AccountId: g.accountId,
	}))
	requireNoErrResp(s.T(), resp, err)
	return len(resp.Msg.GetTransformers())
}

// Creating, changing and trying a user-defined transformer or JavaScript rule is custom_transformers.
// Removing one, reading them and validating code are not: a license without the feature leaves an
// account able to look at and take away what it stored.
func (s *IntegrationTestSuite) Test_CustomTransformers_AreGatedByTheirFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newTransformersGround("custom-transformers-gate")
	javascript := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT

	// What is stored while the license includes everything.
	kept := s.createJavascriptTransformer(g.transformers, g.accountId, "kept")
	removed := s.createJavascriptTransformer(g.transformers, g.accountId, "removed")

	s.closeFeature(license.FeatureCustomTransformers)

	t.Run("creating is refused and writes nothing", func(t *testing.T) {
		_, err := g.create(s, "refused", javascript, javascriptConfig(`return "refused";`))
		requireFeatureRefusal(t, err, license.FeatureCustomTransformers)
		require.Equal(t, 2, g.storedCount(s))
	})

	t.Run("changing is refused and leaves the transformer as it was", func(t *testing.T) {
		_, err := g.update(s, kept, javascriptConfig(`return "changed";`))
		requireFeatureRefusal(t, err, license.FeatureCustomTransformers)

		stored, err := g.transformers.GetUserDefinedTransformerById(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserDefinedTransformerByIdRequest{
			TransformerId: kept.GetId(),
		}))
		requireNoErrResp(t, stored, err)
		require.Equal(t, `return "kept";`, stored.Msg.GetTransformer().GetConfig().GetTransformJavascriptConfig().GetCode())
	})

	t.Run("trying rules is refused", func(t *testing.T) {
		resp, err := g.try(s)
		require.Nil(t, resp)
		requireFeatureRefusal(t, err, license.FeatureCustomTransformers)
	})

	t.Run("the reads are served", func(t *testing.T) {
		require.Equal(t, 2, g.storedCount(s))
		byId, err := g.transformers.GetUserDefinedTransformerById(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserDefinedTransformerByIdRequest{
			TransformerId: kept.GetId(),
		}))
		requireNoErrResp(t, byId, err)
		available, err := g.transformers.IsTransformerNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsTransformerNameAvailableRequest{
			AccountId: g.accountId, TransformerName: "kept",
		}))
		requireNoErrResp(t, available, err)
	})

	t.Run("the validations are served", func(t *testing.T) {
		js, err := g.transformers.ValidateUserJavascriptCode(ctx, connect.NewRequest(&mgmtv1alpha1.ValidateUserJavascriptCodeRequest{
			Code: `return value;`,
		}))
		requireNoErrResp(t, js, err)
		require.True(t, js.Msg.GetValid())
		regex, err := g.transformers.ValidateUserRegexCode(ctx, connect.NewRequest(&mgmtv1alpha1.ValidateUserRegexCodeRequest{
			UserProvidedRegex: `^a+$`,
		}))
		requireNoErrResp(t, regex, err)
		require.True(t, regex.Msg.GetValid())
	})

	t.Run("removing is served", func(t *testing.T) {
		resp, err := g.transformers.DeleteUserDefinedTransformer(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteUserDefinedTransformerRequest{
			TransformerId: removed.GetId(),
		}))
		requireNoErrResp(t, resp, err)
		require.Equal(t, 1, g.storedCount(s))
	})

	t.Run("every call is served by a license that includes only that feature", func(t *testing.T) {
		s.Mocks.ExpiringLicense.SetFeatures(license.FeatureCustomTransformers)

		created, err := g.create(s, "created", javascript, javascriptConfig(`return "created";`))
		requireNoErrResp(t, created, err)
		updated, err := g.update(s, kept, javascriptConfig(`return "changed";`))
		requireNoErrResp(t, updated, err)
		tried, err := g.try(s)
		requireNoErrResp(t, tried, err)
		require.Len(t, tried.Msg.GetRows(), 1)
	})
}

// A key that names no feature list includes every feature: the calls of custom transformers are
// served as they were.
func (s *IntegrationTestSuite) Test_CustomTransformers_UnderAKeyWithoutAFeatureList() {
	t := s.T()
	g := s.newTransformersGround("custom-transformers-no-list")
	s.Mocks.ExpiringLicense.ClearFeatures()

	created, err := g.create(s, "created", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT, javascriptConfig(`return "created";`))
	requireNoErrResp(t, created, err)
	updated, err := g.update(s, created.Msg.GetTransformer(), javascriptConfig(`return "changed";`))
	requireNoErrResp(t, updated, err)
	tried, err := g.try(s)
	requireNoErrResp(t, tried, err)
}

// The PII text is pii_text: the catalog does not offer the transformer without it, a user-defined
// transformer cannot store it, and anonymizing text is refused, each with a message naming the
// feature. Anything else of the catalog is untouched.
func (s *IntegrationTestSuite) Test_PiiText_IsGatedByItsFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newTransformersGround("pii-text-gate")
	piiTextSource := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT
	javascript := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT

	offered := func() []mgmtv1alpha1.TransformerSource {
		resp, err := g.transformers.GetSystemTransformers(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemTransformersRequest{}))
		requireNoErrResp(t, resp, err)
		var sources []mgmtv1alpha1.TransformerSource
		for _, transformer := range resp.Msg.GetTransformers() {
			sources = append(sources, transformer.GetSource())
		}
		return sources
	}
	bySource := func() error {
		_, err := g.transformers.GetSystemTransformerBySource(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemTransformerBySourceRequest{
			Source: piiTextSource,
		}))
		return err
	}
	anonymizeSingle := func() (*connect.Response[mgmtv1alpha1.AnonymizeSingleResponse], error) {
		return g.anonymize.AnonymizeSingle(ctx, connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
			AccountId:           g.accountId,
			InputData:           `{"note": "appeler Zoé demain"}`,
			TransformerMappings: []*mgmtv1alpha1.TransformerMapping{{Expression: ".note", Transformer: piiTextTransformerConfig()}},
		}))
	}

	// A license that includes every feature but this one.
	stored := s.createJavascriptTransformer(g.transformers, g.accountId, "stored")
	s.closeFeature(license.FeaturePiiText)

	t.Run("the catalog does not offer it", func(t *testing.T) {
		sources := offered()
		require.NotEmpty(t, sources)
		require.False(t, slices.Contains(sources, piiTextSource))
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(bySource()))
	})

	t.Run("a user-defined transformer cannot store it", func(t *testing.T) {
		_, err := g.create(s, "pii", piiTextSource, piiTextTransformerConfig())
		requireFeatureRefusal(t, err, license.FeaturePiiText)
		_, err = g.update(s, stored, piiTextTransformerConfig())
		requireFeatureRefusal(t, err, license.FeaturePiiText)
		require.Equal(t, 1, g.storedCount(s), "nothing was written")

		// What is not a PII text is stored as before.
		created, err := g.create(s, "js", javascript, javascriptConfig(`return "js";`))
		requireNoErrResp(t, created, err)
	})

	t.Run("anonymizing text is refused, one text and many", func(t *testing.T) {
		resp, err := anonymizeSingle()
		require.Nil(t, resp)
		requireFeatureRefusal(t, err, license.FeaturePiiText)

		many, err := g.anonymize.AnonymizeMany(ctx, connect.NewRequest(&mgmtv1alpha1.AnonymizeManyRequest{
			AccountId: g.accountId,
			InputData: []string{`{"note": "appeler Zoé demain"}`},
		}))
		require.Nil(t, many)
		require.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err), "%v", err)
		require.ErrorContains(t, err, "this license does not include pii_text")
	})

	t.Run("the license that includes it offers it and stores it", func(t *testing.T) {
		s.Mocks.ExpiringLicense.ClearFeatures()

		require.True(t, slices.Contains(offered(), piiTextSource))
		require.NoError(t, bySource())
		created, err := g.create(s, "pii", piiTextSource, piiTextTransformerConfig())
		requireNoErrResp(t, created, err)
		updated, err := g.update(s, created.Msg.GetTransformer(), piiTextTransformerConfig())
		requireNoErrResp(t, updated, err)
	})
}
