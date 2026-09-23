package protosecret

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sym_encrypt "github.com/fishtre-compagnie/husonym/internal/encrypt/sym"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func newEncryptor(t *testing.T) sym_encrypt.Interface {
	t.Helper()
	encryptor, err := sym_encrypt.NewEncryptor("a-test-password")
	require.NoError(t, err)
	return encryptor
}

func consistency(key string) *mgmtv1alpha1.AccountSettingConfig {
	return &mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_AnonymizationConsistency{
			AnonymizationConsistency: &mgmtv1alpha1.AnonymizationConsistency{DerivationKey: key},
		},
	}
}

func TestEncryptThenDecryptGivesBackTheSecret(t *testing.T) {
	encryptor := newEncryptor(t)
	config := consistency("the-key")

	encrypted, err := Encrypt(encryptor, config)
	require.NoError(t, err)
	require.NotEqual(
		t,
		"the-key",
		encrypted.GetAnonymizationConsistency().GetDerivationKey(),
		"the secret must not travel in clear",
	)

	decrypted, err := Decrypt(encryptor, encrypted)
	require.NoError(t, err)
	require.Equal(t, "the-key", decrypted.GetAnonymizationConsistency().GetDerivationKey())
}

func TestEncryptLeavesTheMessageItWasGiven(t *testing.T) {
	encryptor := newEncryptor(t)
	config := consistency("the-key")

	_, err := Encrypt(encryptor, config)
	require.NoError(t, err)
	require.Equal(
		t,
		"the-key",
		config.GetAnonymizationConsistency().GetDerivationKey(),
		"the caller's message is copied, never written into",
	)
}

func TestRedactClearsTheSecretAndNamesItsFingerprint(t *testing.T) {
	redacted, fingerprints, err := Redact(consistency("the-key"))
	require.NoError(t, err)
	require.Empty(t, redacted.GetAnonymizationConsistency().GetDerivationKey())
	require.Equal(
		t,
		map[string]string{"anonymization_consistency.derivation_key": Fingerprint("the-key")},
		fingerprints,
	)
}

func TestRedactSaysTwoSecretsApartAndShowsNothingOfThem(t *testing.T) {
	require.Equal(t, Fingerprint("the-key"), Fingerprint("the-key"))
	require.NotEqual(t, Fingerprint("the-key"), Fingerprint("another-key"))
	require.NotContains(t, Fingerprint("the-key"), "the-key")
	require.Len(t, Fingerprint("the-key"), fingerprintBytes*2)
}

func TestRedactOfAMessageWithoutASecretReportsNone(t *testing.T) {
	_, fingerprints, err := Redact(&mgmtv1alpha1.AccountSettingConfig{})
	require.NoError(t, err)
	require.Empty(t, fingerprints)
}

func TestNilMessageIsHandedBack(t *testing.T) {
	var config *mgmtv1alpha1.AccountSettingConfig
	out, err := Encrypt(newEncryptor(t), config)
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestDecryptOfSomethingNotEncryptedFails(t *testing.T) {
	_, err := Decrypt(newEncryptor(t), consistency("not-a-ciphertext"))
	require.Error(t, err)
	require.Contains(
		t,
		err.Error(),
		"anonymization_consistency.derivation_key",
		"the failure says which field it could not read",
	)
}

// TestASecretInAListIsRefusedRatherThanLeftInClear: a message holding several settings is
// not what this package takes. Handed one anyway, it says so instead of walking past the
// secrets it carries.
func TestASecretInAListIsRefusedRatherThanLeftInClear(t *testing.T) {
	response := &mgmtv1alpha1.GetAccountSettingsResponse{
		Settings: []*mgmtv1alpha1.AccountSetting{
			{AccountId: "an-account", Config: consistency("the-key")},
		},
	}
	_, err := Encrypt(newEncryptor(t), response)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settings")

	require.Equal(
		t,
		"the-key",
		response.GetSettings()[0].GetConfig().GetAnonymizationConsistency().GetDerivationKey(),
		"the refusal leaves the caller's message untouched",
	)
}

// TestASettingOnItsOwnIsWalked: the shape the service does hand over — one setting, whose
// secret sits under a plain message field — is reached.
func TestASettingOnItsOwnIsWalked(t *testing.T) {
	setting := &mgmtv1alpha1.AccountSetting{
		AccountId: "an-account",
		Config:    consistency("the-key"),
	}
	redacted, fingerprints, err := Redact(setting)
	require.NoError(t, err)
	require.Empty(
		t,
		redacted.GetConfig().GetAnonymizationConsistency().GetDerivationKey(),
	)
	require.Equal(
		t,
		map[string]string{"config.anonymization_consistency.derivation_key": Fingerprint("the-key")},
		fingerprints,
	)
}

// TestTheAnnotationIsReadFromTheContract guards the one thing the walk depends on: the
// option travels with the generated descriptor.
func TestTheAnnotationIsReadFromTheContract(t *testing.T) {
	fields := (&mgmtv1alpha1.AnonymizationConsistency{}).ProtoReflect().Descriptor().Fields()
	field := fields.ByName("derivation_key")
	require.NotNil(t, field)
	require.True(t, proto.GetExtension(
		field.Options().(*descriptorpb.FieldOptions),
		mgmtv1alpha1.E_Secret,
	).(bool))
}
