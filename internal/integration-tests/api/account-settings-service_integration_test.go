package integrationtests_test

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func consistencySetting(key string) *mgmtv1alpha1.AccountSettingConfig {
	return &mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_AnonymizationConsistency{
			AnonymizationConsistency: &mgmtv1alpha1.AnonymizationConsistency{DerivationKey: key},
		},
	}
}

// Test_AccountSettingsService walks the setting through a real database: the generated
// column that derives its kind, the constraint that keeps one per kind, and the row a run
// creates for itself.
func (s *IntegrationTestSuite) Test_AccountSettingsService() {
	t := s.T()
	ctx := s.ctx

	// Authenticated, because a team account — one account per sub-test — needs a user.
	client := s.OSSAuthenticatedLicensedClients.AccountSettings(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	userclient := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	s.setUser(ctx, userclient)
	s.createPersonalAccount(ctx, userclient)

	// One account per sub-test: a setting is held per account, so two sub-tests sharing one
	// would read each other's.
	newAccount := func() string {
		return s.createTeamAccount(ctx, userclient, uuid.NewString())
	}

	t.Run("an account starts with no settings", func(t *testing.T) {
		accountId := newAccount()
		resp, err := client.GetAccountSettings(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Empty(t, resp.Msg.GetSettings())
	})

	t.Run("a setting written is read back without its secret", func(t *testing.T) {
		accountId := newAccount()

		_, err := client.SetAccountSetting(ctx, connect.NewRequest(
			&mgmtv1alpha1.SetAccountSettingRequest{
				AccountId: accountId,
				Config:    consistencySetting("a-key-taken-over"),
			},
		))
		require.NoError(t, err)

		resp, err := client.GetAccountSettings(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetSettings(), 1)

		setting := resp.Msg.GetSettings()[0]
		require.Empty(t, setting.GetConfig().GetAnonymizationConsistency().GetDerivationKey())
		require.NotEmpty(
			t,
			setting.GetSecretFingerprints()["anonymization_consistency.derivation_key"],
		)

		// The run reads it in clear, and it is the key that was given.
		key, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Equal(t, "a-key-taken-over", key.Msg.GetKey())
	})

	t.Run("writing again replaces the setting rather than adding one", func(t *testing.T) {
		accountId := newAccount()

		for _, key := range []string{"the-first-key", "the-second-key"} {
			_, err := client.SetAccountSetting(ctx, connect.NewRequest(
				&mgmtv1alpha1.SetAccountSettingRequest{
					AccountId: accountId,
					Config:    consistencySetting(key),
				},
			))
			require.NoError(t, err)
		}

		resp, err := client.GetAccountSettings(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetSettings(), 1, "one row per account and kind of setting")

		key, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Equal(t, "the-second-key", key.Msg.GetKey())
	})

	t.Run("nothing is drawn for an account that was not asked to have one", func(t *testing.T) {
		accountId := newAccount()

		resp, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Nil(t, resp.Msg.Key)

		settings, err := client.GetAccountSettings(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Empty(t, settings.Msg.GetSettings())
	})

	t.Run("a key drawn for an account is the one it keeps", func(t *testing.T) {
		accountId := newAccount()

		first, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
				AccountId:        accountId,
				GenerateIfAbsent: true,
			},
		))
		require.NoError(t, err)
		require.NotEmpty(t, first.Msg.GetKey())

		// Asking again — the next run, or the next table of this one — gives the same key,
		// or the outputs of two runs would not match.
		second, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
				AccountId:        accountId,
				GenerateIfAbsent: true,
			},
		))
		require.NoError(t, err)
		require.Equal(t, first.Msg.GetKey(), second.Msg.GetKey())

		settings, err := client.GetAccountSettings(ctx, connect.NewRequest(
			&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: accountId},
		))
		require.NoError(t, err)
		require.Len(t, settings.Msg.GetSettings(), 1)
		require.Empty(
			t,
			settings.Msg.GetSettings()[0].GetCreatedByUserId(),
			"nobody chose this key",
		)
	})

	t.Run("two accounts do not share a key", func(t *testing.T) {
		keys := map[string]struct{}{}
		for range 2 {
			accountId := newAccount()
			resp, err := client.GetAccountConsistencyKey(ctx, connect.NewRequest(
				&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
					AccountId:        accountId,
					GenerateIfAbsent: true,
				},
			))
			require.NoError(t, err)
			keys[resp.Msg.GetKey()] = struct{}{}
		}
		require.Len(t, keys, 2)
	})
}

func oidcSetting(issuer, clientId string) *mgmtv1alpha1.AccountSettingConfig {
	return &mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_OidcProvider{
			OidcProvider: &mgmtv1alpha1.OidcProvider{Issuer: issuer, ClientId: clientId},
		},
	}
}

// Identities are keyed by (issuer, subject), so two accounts trusting one issuer share the
// subject space it mints: whoever administers sign-ins there could hand themselves the
// identity of a member of the other account. That is the thing keying on the issuer
// prevents, reintroduced one level up.
func (s *IntegrationTestSuite) Test_AccountSettingsService_OidcProvider() {
	t := s.T()
	ctx := s.ctx

	client := s.OSSAuthenticatedLicensedClients.AccountSettings(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	userclient := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	s.setUser(ctx, userclient)
	s.createPersonalAccount(ctx, userclient)

	newAccount := func() string {
		return s.createTeamAccount(ctx, userclient, uuid.NewString())
	}

	t.Run("an account declares its provider, and the generated column knows the kind", func(t *testing.T) {
		accountId := newAccount()
		issuer := "https://" + uuid.NewString() + ".example.com/"

		_, err := client.SetAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
			AccountId: accountId,
			Config:    oidcSetting(issuer, "a-client"),
		}))
		require.NoError(t, err, "the migration must recognize the variant, or the constraint refuses the row")

		got, err := client.GetAccountSettings(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountSettingsRequest{
			AccountId: accountId,
		}))
		require.NoError(t, err)
		require.Len(t, got.Msg.GetSettings(), 1)
		require.Equal(t, issuer, got.Msg.GetSettings()[0].GetConfig().GetOidcProvider().GetIssuer())
	})

	t.Run("a second account cannot claim the same issuer", func(t *testing.T) {
		issuer := "https://" + uuid.NewString() + ".example.com/"

		first := newAccount()
		_, err := client.SetAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
			AccountId: first,
			Config:    oidcSetting(issuer, "a-client"),
		}))
		require.NoError(t, err)

		second := newAccount()
		_, err = client.SetAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
			AccountId: second,
			Config:    oidcSetting(issuer, "another-client"),
		}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "already declared by another account")
	})

	t.Run("an account may replace its own declaration with the same issuer", func(t *testing.T) {
		accountId := newAccount()
		issuer := "https://" + uuid.NewString() + ".example.com/"

		for _, clientId := range []string{"first-client", "second-client"} {
			_, err := client.SetAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
				AccountId: accountId,
				Config:    oidcSetting(issuer, clientId),
			}))
			require.NoError(t, err, "the guard is about other accounts, not about this one")
		}
	})

	// The probe reaches the public internet, which a test must not depend on. What is
	// checked here is that the endpoint refuses what it can judge without leaving the
	// process, and that it never writes.
	t.Run("trying a setting reports findings and writes nothing", func(t *testing.T) {
		accountId := newAccount()

		got, err := client.TestAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.TestAccountSettingRequest{
			AccountId: accountId,
			Config:    oidcSetting("not-an-https-url", ""),
		}))
		require.NoError(t, err, "a provider that cannot work is findings, not an error")
		require.False(t, got.Msg.GetOk())
		require.NotEmpty(t, got.Msg.GetChecks())

		settings, err := client.GetAccountSettings(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountSettingsRequest{
			AccountId: accountId,
		}))
		require.NoError(t, err)
		require.Empty(t, settings.Msg.GetSettings(), "trying must never write")
	})
}
