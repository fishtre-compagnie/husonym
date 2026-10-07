package integrationtests_test

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ssoGround is a team account of the expiring mode, whose license a test restricts.
type ssoGround struct {
	accountId string
	settings  mgmtv1alpha1connect.AccountSettingServiceClient
}

func (s *IntegrationTestSuite) newSsoGround(name string) *ssoGround {
	userOpt := integrationtests_test.WithUserId(name)
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	s.T().Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
	s.setUser(s.ctx, users)
	return &ssoGround{
		accountId: s.createTeamAccount(s.ctx, users, name+"-"+uuid.NewString()),
		settings:  s.OSSAuthenticatedExpiringClients.AccountSettings(userOpt),
	}
}

func (g *ssoGround) set(s *IntegrationTestSuite, config *mgmtv1alpha1.AccountSettingConfig) error {
	_, err := g.settings.SetAccountSetting(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
		AccountId: g.accountId,
		Config:    config,
	}))
	return err
}

func (g *ssoGround) stored(s *IntegrationTestSuite) []*mgmtv1alpha1.AccountSetting {
	resp, err := g.settings.GetAccountSettings(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountSettingsRequest{
		AccountId: g.accountId,
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetSettings()
}

// Declaring an identity provider, and trying one, is sso. A license without the feature refuses
// both and writes nothing; reading what the account holds is served.
func (s *IntegrationTestSuite) Test_OidcProviderSetting_IsGatedBySso() {
	t := s.T()
	g := s.newSsoGround("sso-gate")
	issuer := publicIssuer()

	s.closeFeature(license.FeatureSso)

	t.Run("declaring a provider is refused and writes nothing", func(t *testing.T) {
		err := g.set(s, oidcSetting(issuer, "a-client"))
		requireFeatureRefusal(t, err, license.FeatureSso)
		require.Empty(t, g.stored(s))
	})

	t.Run("trying a provider is refused", func(t *testing.T) {
		resp, err := g.settings.TestAccountSetting(s.ctx, connect.NewRequest(&mgmtv1alpha1.TestAccountSettingRequest{
			AccountId: g.accountId,
			Config:    oidcSetting("not-an-https-url", ""),
		}))
		require.Nil(t, resp)
		requireFeatureRefusal(t, err, license.FeatureSso)
	})

	t.Run("reading the settings is served", func(t *testing.T) {
		require.Empty(t, g.stored(s))
	})

	t.Run("the provider is declared again once the feature is back", func(t *testing.T) {
		s.Mocks.ExpiringLicense.ClearFeatures()
		require.NoError(t, g.set(s, oidcSetting(issuer, "a-client")))
		require.Len(t, g.stored(s), 1)
	})
}

// The consistency key is written by the same RPC as the provider, and is not sso: it is tried and
// written under a license that does not include the feature.
func (s *IntegrationTestSuite) Test_ConsistencyKey_IsNotGatedBySso() {
	t := s.T()
	g := s.newSsoGround("sso-consistency")

	s.closeFeature(license.FeatureSso)

	require.NoError(t, g.set(s, consistencySetting("a-given-key")))
	stored := g.stored(s)
	require.Len(t, stored, 1)
	require.NotNil(t, stored[0].GetConfig().GetAnonymizationConsistency())

	tried, err := g.settings.TestAccountSetting(s.ctx, connect.NewRequest(&mgmtv1alpha1.TestAccountSettingRequest{
		AccountId: g.accountId,
		Config:    consistencySetting("another-key"),
	}))
	requireNoErrResp(t, tried, err)
	require.True(t, tried.Msg.GetOk())
}

// A provider declared while the license included sso is still stored and still read once the
// feature is closed: the sign-in of its members is not a call the gate sits on. The discovery
// that starts a sign-in is covered by the unit test of the auth service, which is not part of
// the API the integration suite serves.
func (s *IntegrationTestSuite) Test_OidcLogin_StillWorksWithoutTheFeature() {
	t := s.T()
	g := s.newSsoGround("sso-login")
	issuer := publicIssuer()
	require.NoError(t, g.set(s, oidcSetting(issuer, "a-client")))

	s.closeFeature(license.FeatureSso)

	stored := g.stored(s)
	require.Len(t, stored, 1)
	require.Equal(t, issuer, stored[0].GetConfig().GetOidcProvider().GetIssuer())
	require.Equal(t, "a-client", stored[0].GetConfig().GetOidcProvider().GetClientId())

	// A write of the other kind sits beside the provider and leaves it as it was.
	require.NoError(t, g.set(s, consistencySetting("a-given-key")))
	require.Len(t, g.stored(s), 2)
}
