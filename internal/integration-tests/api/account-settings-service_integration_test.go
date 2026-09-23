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
