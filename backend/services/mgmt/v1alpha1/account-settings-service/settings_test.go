package v1alpha1_accountsettingservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/encrypt/protosecret"
	sym_encrypt "github.com/fishtre-compagnie/husonym/internal/encrypt/sym"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const anAccountId = "d5ef8fc7-4b2e-4f1f-8c9c-2a2a2a2a2a2a"

type fixture struct {
	svc       *Service
	querier   *db_queries.MockQuerier
	users     *userdata.MockInterface
	encryptor sym_encrypt.Interface
}

func newFixture(t *testing.T, cfg *Config) *fixture {
	t.Helper()
	encryptor, err := sym_encrypt.NewEncryptor("a-test-password")
	require.NoError(t, err)

	querier := db_queries.NewMockQuerier(t)
	users := userdata.NewMockInterface(t)
	return &fixture{
		svc: New(
			cfg,
			husonymdb.New(husonymdb.NewMockDBTX(t), querier),
			users,
			encryptor,
		),
		querier:   querier,
		users:     users,
		encryptor: encryptor,
	}
}

// allowUser lets the caller through, or turns it away at the account.
func (f *fixture) allowUser(t *testing.T, allowed bool) {
	t.Helper()
	enforcer := userdata.NewMockEntityEnforcer(t)
	var result error
	if !allowed {
		result = errors.New("test: not in account")
	}
	enforcer.On("EnforceAccount", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(result)
	f.users.On("GetUser", mock.Anything).Once().Return(&userdata.User{
		EntityEnforcer: enforcer,
	}, nil)
}

// storedRow is what the column holds for a setting: its secrets encrypted one by one.
func (f *fixture) storedRow(t *testing.T, key string) db_queries.HusonymApiAccountSetting {
	t.Helper()
	encrypted, err := protosecret.Encrypt(f.encryptor, consistencyConfig(key))
	require.NoError(t, err)
	config, err := json.Marshal(encrypted)
	require.NoError(t, err)

	accountUuid, err := husonymdb.ToUuid(anAccountId)
	require.NoError(t, err)
	return db_queries.HusonymApiAccountSetting{
		ID:          newPgUuid(t),
		AccountID:   accountUuid,
		Config:      config,
		SettingType: pgtype.Text{String: settingTypeAnonymizationConsistency, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func consistencyConfig(key string) *mgmtv1alpha1.AccountSettingConfig {
	return &mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_AnonymizationConsistency{
			AnonymizationConsistency: &mgmtv1alpha1.AnonymizationConsistency{DerivationKey: key},
		},
	}
}

func newPgUuid(t *testing.T) pgtype.UUID {
	t.Helper()
	value, err := husonymdb.ToUuid(uuid.NewString())
	require.NoError(t, err)
	return value
}

func TestGetAccountSettingsHandsBackTheFingerprintAndNotTheSecret(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)
	f.querier.On("GetAccountSettings", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return([]db_queries.HusonymApiAccountSetting{f.storedRow(t, "the-key")}, nil)

	resp, err := f.svc.GetAccountSettings(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: anAccountId},
	))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSettings(), 1)

	setting := resp.Msg.GetSettings()[0]
	require.Empty(
		t,
		setting.GetConfig().GetAnonymizationConsistency().GetDerivationKey(),
		"the secret is absent from what a user reads",
	)
	require.Equal(
		t,
		map[string]string{
			"anonymization_consistency.derivation_key": protosecret.Fingerprint("the-key"),
		},
		setting.GetSecretFingerprints(),
	)
	require.Equal(t, anAccountId, setting.GetAccountId())
	require.Empty(t, setting.GetCreatedByUserId(), "a generated setting has no user behind it")
}

func TestGetAccountSettingsTurnsAwayWhoIsNotInTheAccount(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, false)

	_, err := f.svc.GetAccountSettings(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountSettingsRequest{AccountId: anAccountId},
	))
	require.Error(t, err)
}

func TestSetAccountSettingWritesTheSecretEncryptedAndAnswersWithoutIt(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)

	var written []byte
	f.querier.On("UpsertAccountSetting", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Run(func(args mock.Arguments) {
			params, ok := args.Get(2).(db_queries.UpsertAccountSettingParams)
			require.True(t, ok)
			written = params.Config
		}).
		Return(f.storedRow(t, "a-given-key"), nil)

	resp, err := f.svc.SetAccountSetting(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.SetAccountSettingRequest{
			AccountId: anAccountId,
			Config:    consistencyConfig("a-given-key"),
		},
	))
	require.NoError(t, err)

	require.NotContains(
		t,
		string(written),
		"a-given-key",
		"what lands in the column never holds the secret in clear",
	)
	stored := &mgmtv1alpha1.AccountSettingConfig{}
	require.NoError(t, json.Unmarshal(written, stored))
	decrypted, err := protosecret.Decrypt(f.encryptor, stored)
	require.NoError(t, err)
	require.Equal(
		t,
		"a-given-key",
		decrypted.GetAnonymizationConsistency().GetDerivationKey(),
		"and what lands there reads back as the key that was given",
	)

	require.Empty(
		t,
		resp.Msg.GetSetting().GetConfig().GetAnonymizationConsistency().GetDerivationKey(),
	)
	require.Equal(
		t,
		protosecret.Fingerprint("a-given-key"),
		resp.Msg.GetSetting().GetSecretFingerprints()["anonymization_consistency.derivation_key"],
	)
}

func TestSetAccountSettingTurnsAwayWhoMayNotEditTheAccount(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, false)

	_, err := f.svc.SetAccountSetting(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.SetAccountSettingRequest{
			AccountId: anAccountId,
			Config:    consistencyConfig("a-given-key"),
		},
	))
	require.Error(t, err)
}

func TestGetAccountConsistencyKeyReadsTheOneTheAccountHolds(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)
	f.querier.On("GetAccountSettingByType", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(f.storedRow(t, "the-key"), nil)

	resp, err := f.svc.GetAccountConsistencyKey(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: anAccountId},
	))
	require.NoError(t, err)
	require.Equal(t, "the-key", resp.Msg.GetKey())
}

func TestGetAccountConsistencyKeyGeneratesNothingUnlessAsked(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)
	f.querier.On("GetAccountSettingByType", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(db_queries.HusonymApiAccountSetting{}, pgx.ErrNoRows)

	resp, err := f.svc.GetAccountConsistencyKey(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: anAccountId},
	))
	require.NoError(t, err)
	require.Nil(
		t,
		resp.Msg.Key,
		"a deployment whose variable still carries the key must see nothing written here",
	)
}

func TestGetAccountConsistencyKeyDrawsOneWhenAskedAndTheAccountHasNone(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)
	f.querier.On("GetAccountSettingByType", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(db_queries.HusonymApiAccountSetting{}, pgx.ErrNoRows)

	var written []byte
	f.querier.On("CreateAccountSettingIfAbsent", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Run(func(args mock.Arguments) {
			params, ok := args.Get(2).(db_queries.CreateAccountSettingIfAbsentParams)
			require.True(t, ok)
			written = params.Config
			require.False(t, params.CreatedByUserID.Valid, "nobody chose this key")
		}).
		Return(db_queries.HusonymApiAccountSetting{}, nil)

	resp, err := f.svc.GetAccountConsistencyKey(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
			AccountId:        anAccountId,
			GenerateIfAbsent: true,
		},
	))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Msg.GetKey())
	require.NotContains(t, string(written), resp.Msg.GetKey())

	stored := &mgmtv1alpha1.AccountSettingConfig{}
	require.NoError(t, json.Unmarshal(written, stored))
	decrypted, err := protosecret.Decrypt(f.encryptor, stored)
	require.NoError(t, err)
	require.Equal(
		t,
		resp.Msg.GetKey(),
		decrypted.GetAnonymizationConsistency().GetDerivationKey(),
		"the key handed to the run is the one written down",
	)
}

func TestTwoAccountsDoNotGetTheSameGeneratedKey(t *testing.T) {
	keys := map[string]struct{}{}
	for range 5 {
		key, err := newDerivationKey()
		require.NoError(t, err)
		require.NotEmpty(t, key)
		keys[key] = struct{}{}
	}
	require.Len(t, keys, 5)
}

// TestGetAccountConsistencyKeyKeepsTheKeyThatLandedFirst: two runs of the same account
// starting together each draw a key. Only one row lands, and both runs then derive from
// it — outputs of a run must come from a single key.
func TestGetAccountConsistencyKeyKeepsTheKeyThatLandedFirst(t *testing.T) {
	f := newFixture(t, &Config{})
	f.allowUser(t, true)
	f.querier.On("GetAccountSettingByType", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(db_queries.HusonymApiAccountSetting{}, pgx.ErrNoRows)
	f.querier.On("CreateAccountSettingIfAbsent", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(db_queries.HusonymApiAccountSetting{}, pgx.ErrNoRows)
	f.querier.On("GetAccountSettingByType", mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(f.storedRow(t, "the-key-that-won"), nil)

	resp, err := f.svc.GetAccountConsistencyKey(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
			AccountId:        anAccountId,
			GenerateIfAbsent: true,
		},
	))
	require.NoError(t, err)
	require.Equal(t, "the-key-that-won", resp.Msg.GetKey())
}

// TestGetAccountConsistencyKeyIsForTheRunAlone: in Husonym Cloud the key in clear only
// leaves towards a run, the lock ReconcileJobMappings applies.
func TestGetAccountConsistencyKeyIsForTheRunAlone(t *testing.T) {
	f := newFixture(t, &Config{IsHusonymCloud: true})
	f.allowUser(t, true)

	_, err := f.svc.GetAccountConsistencyKey(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: anAccountId},
	))
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// A provider this deployment must not be made to contact is refused when it is saved, not
// only when it is tried: saving without trying must not get around the bound. The mock
// querier holds the proof that nothing is written -- an unexpected call fails the test.
func TestSetAccountSettingRefusesAnIssuerOutOfReach(t *testing.T) {
	for _, issuer := range []string{
		"http://idp.example.com/realms/acme",
		"https://127.0.0.1/realms/acme",
		"https://169.254.169.254/latest",
		"https://10.0.3.12/realms/acme",
	} {
		t.Run(issuer, func(t *testing.T) {
			f := newFixture(t, &Config{})
			f.allowUser(t, true)
			f.querier.On("CountOtherAccountsDeclaringIssuer", mock.Anything, mock.Anything, mock.Anything).
				Once().Return(int64(0), nil)

			_, err := f.svc.SetAccountSetting(context.Background(), connect.NewRequest(
				&mgmtv1alpha1.SetAccountSettingRequest{
					AccountId: anAccountId,
					Config: &mgmtv1alpha1.AccountSettingConfig{
						Config: &mgmtv1alpha1.AccountSettingConfig_OidcProvider{
							OidcProvider: &mgmtv1alpha1.OidcProvider{Issuer: issuer, ClientId: "husonym"},
						},
					},
				},
			))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), err)
		})
	}
}
