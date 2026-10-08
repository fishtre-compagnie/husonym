package issuing

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

const testKid = "k-test"

func newKeyPair(t *testing.T) (ed25519.PrivateKey, license.Keyring) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return priv, license.Keyring{testKid: pub}
}

func newTestSigner(t *testing.T) (*Signer, license.Keyring) {
	t.Helper()
	priv, ring := newKeyPair(t)
	signer, err := NewSigner(priv, ring)
	require.NoError(t, err)
	return signer, ring
}

func pemOf(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func writeFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "signing.key")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	return path
}

// pemBody is the lines of a PEM file between its markers: what an error must never carry.
func pemBody(content []byte) []string {
	var body []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(content)), "\n") {
		if line != "" && !strings.HasPrefix(line, "-----") {
			body = append(body, line)
		}
	}
	return body
}

func validDraft(now time.Time) *Draft {
	sources := 5
	grace := 7
	return &Draft{
		LicenseID:          "0123456789abcdef",
		CustomerExternalID: "acme",
		CustomerName:       "Acme Co.",
		Plan:               "a plan",
		Features:           []string{string(license.FeatureSso), string(license.FeatureRbac)},
		MaxSources:         &sources,
		ExpiresAt:          endOfDay(now.AddDate(1, 0, 0)),
		GraceDays:          &grace,
		Telemetry:          string(license.TelemetryOfflineReport),
		Note:               "a note",
		Succeeds:           "fedcba9876543210",
	}
}

func testNow() time.Time {
	return time.Now().UTC()
}

func Test_LoadSigner_ReadsAKeyOfTheRing(t *testing.T) {
	priv, ring := newKeyPair(t)

	signer, err := LoadSigner(writeFile(t, pemOf(t, priv)), ring)

	require.NoError(t, err)
	require.Equal(t, testKid, signer.Kid())
}

func Test_LoadSigner_RefusesAKeyOutsideTheRing(t *testing.T) {
	_, ring := newKeyPair(t)
	stranger, _ := newKeyPair(t)
	content := pemOf(t, stranger)

	signer, err := LoadSigner(writeFile(t, content), ring)

	require.ErrorIs(t, err, ErrKeyNotInRing)
	require.Nil(t, signer)
	requireNoFileContent(t, err, content)
}

func Test_LoadSigner_RefusesWhatIsNotAKeyFile(t *testing.T) {
	_, ring := newKeyPair(t)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	valid, _ := newKeyPair(t)
	truncated := pemOf(t, valid)
	truncated = append(truncated[:60:60], []byte("\n-----END PRIVATE KEY-----\n")...)

	cases := map[string]struct {
		content []byte
		want    error
	}{
		"empty file":       {content: nil, want: ErrKeyFileNotAKey},
		"not PEM":          {content: []byte("MARKER-not-a-pem-file-MARKER"), want: ErrKeyFileNotAKey},
		"truncated key":    {content: truncated, want: ErrKeyFileNotAKey},
		"not ed25519":      {content: pemOf(t, ecdsaKey), want: ErrKeyFileNotAKey},
		"over the maximum": {content: []byte(strings.Repeat("MARKER", maxKeyFileSize)), want: ErrKeyFileTooLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			signer, err := LoadSigner(writeFile(t, tc.content), ring)

			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.want.Error(), err.Error(), "the message is fixed")
			require.Nil(t, signer)
			requireNoFileContent(t, err, tc.content)
		})
	}
}

func Test_LoadSigner_AFileOfExactlyTheMaximumIsRead(t *testing.T) {
	priv, ring := newKeyPair(t)
	content := pemOf(t, priv)
	content = append(content, []byte(strings.Repeat("\n", maxKeyFileSize-len(content)))...)

	_, err := LoadSigner(writeFile(t, content), ring)

	require.NoError(t, err)
}

func Test_LoadSigner_RefusesAFileItCannotRead(t *testing.T) {
	_, ring := newKeyPair(t)

	_, err := LoadSigner("", ring)
	require.ErrorIs(t, err, ErrNoKeyFile)

	_, err = LoadSigner(filepath.Join(t.TempDir(), "absent.key"), ring)
	require.ErrorIs(t, err, ErrKeyFileMissing)
	require.Equal(t, ErrKeyFileMissing.Error(), err.Error())

	_, err = LoadSigner(t.TempDir(), ring)
	require.ErrorIs(t, err, ErrKeyFileUnreadable)
	require.Equal(t, ErrKeyFileUnreadable.Error(), err.Error())
}

func requireNoFileContent(t *testing.T, err error, content []byte) {
	t.Helper()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "MARKER")
	for _, line := range pemBody(content) {
		require.NotContains(t, err.Error(), line)
	}
}

func Test_NewSigner_RefusesAKeyOutsideTheRing(t *testing.T) {
	_, ring := newKeyPair(t)
	stranger, _ := newKeyPair(t)

	_, err := NewSigner(stranger, ring)
	require.ErrorIs(t, err, ErrKeyNotInRing)

	_, err = NewSigner(nil, ring)
	require.Error(t, err)

	_, err = NewSigner(stranger[:10], ring)
	require.Error(t, err)
}

func Test_Signer_NeverPrintsItsKey(t *testing.T) {
	priv, ring := newKeyPair(t)
	signer, err := NewSigner(priv, ring)
	require.NoError(t, err)

	printed := fmt.Sprintf("%v %+v %#v", signer, signer, signer) +
		fmt.Sprintf(" %v %+v %#v", *signer, *signer, *signer) +
		fmt.Sprintf(" %s %q %d %x %X", signer, signer, signer, signer, *signer) +
		fmt.Sprint(signer, *signer) + signer.String()

	require.Equal(t, strings.Repeat("signer", strings.Count(printed, "signer")),
		strings.NewReplacer(" ", "", `"`, "").Replace(printed), "nothing but the fixed word is printed")
	for _, secret := range [][]byte{priv, priv.Seed()} {
		require.NotContains(t, printed, hex.EncodeToString(secret))
		require.NotContains(t, strings.ToLower(printed), hex.EncodeToString(secret))
		require.NotContains(t, printed, base64.StdEncoding.EncodeToString(secret))
		require.NotContains(t, printed, fmt.Sprintf("%d", secret)[1:20])
		require.NotContains(t, printed, fmt.Sprintf("%#v", secret)[10:40])
	}
}

func Test_Signer_Issue_GivesAKeyTheProductReads(t *testing.T) {
	signer, ring := newTestSigner(t)
	now := time.Now().UTC()
	draft := validDraft(now)

	issued, key, err := signer.Issue(draft, now)

	require.NoError(t, err)
	read, err := license.ParseWith(issued.Encoded, ring)
	require.NoError(t, err)
	require.Equal(t, read, key, "the key returned is the one the product would read")

	require.Equal(t, testKid, issued.Kid)
	require.Equal(t, draft.LicenseID, key.Id)
	require.Equal(t, draft.CustomerName, key.IssuedTo)
	require.Equal(t, draft.CustomerExternalID, key.CustomerId)
	require.True(t, key.IssuedAt.Equal(now.Truncate(time.Second)))
	require.True(t, key.ExpiresAt.Equal(draft.ExpiresAt))
	require.Equal(t, 7, *key.GraceDays)
	require.Equal(t, &license.Limits{MaxSources: draft.MaxSources}, key.Limits)
	require.Equal(t, draft.Plan, key.Plan)
	require.Equal(t, draft.Features, key.Features)
	require.Equal(t, license.TelemetryOfflineReport, key.TelemetryMode())
	require.NotContains(t, issued.Encoded, draft.Note)
}

func Test_Signer_Issue_BareDraft(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := time.Now().UTC()
	draft := &Draft{
		LicenseID:          "0123456789abcdef",
		CustomerExternalID: "acme",
		CustomerName:       "Acme Co.",
		AllFeatures:        true,
		ExpiresAt:          endOfDay(now.AddDate(0, 0, 30)),
	}

	_, key, err := signer.Issue(draft, now)

	require.NoError(t, err)
	require.Nil(t, key.Features, "all features is no list in the key")
	require.True(t, key.AllowsEveryFeature())
	require.Nil(t, key.Limits)
	require.Nil(t, key.GraceDays)
	require.Empty(t, key.Telemetry)
	require.Equal(t, license.TelemetryOnline, key.TelemetryMode())
	require.Empty(t, key.Plan)
}

func Test_Signer_Issue_AnEmptyListAllowsNoFeature(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := time.Now().UTC()
	for name, features := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			draft := validDraft(now)
			draft.Features = features

			_, key, err := signer.Issue(draft, now)

			require.NoError(t, err)
			require.NotNil(t, key.Features)
			require.Empty(t, key.Features)
			require.False(t, key.AllowsEveryFeature())
			require.False(t, key.HasFeature(license.FeatureSso))
		})
	}
}

func Test_Signer_Issue_Refuses(t *testing.T) {
	signer, _ := newTestSigner(t)
	now := time.Now().UTC()
	negative := -1

	cases := map[string]func(d *Draft){
		"an unknown feature":           func(d *Draft) { d.Features = []string{"sso", "not_a_feature"} },
		"the wildcard among features":  func(d *Draft) { d.Features = []string{"sso", license.FeatureWildcard} },
		"the wildcard alone":           func(d *Draft) { d.Features = []string{license.FeatureWildcard} },
		"all features and a list":      func(d *Draft) { d.AllFeatures = true },
		"an unknown telemetry mode":    func(d *Draft) { d.Telemetry = "sometimes" },
		"an expiry in the past":        func(d *Draft) { d.ExpiresAt = now.AddDate(0, 0, -1) },
		"no expiry":                    func(d *Draft) { d.ExpiresAt = time.Time{} },
		"a negative grace":             func(d *Draft) { d.GraceDays = &negative },
		"a negative cap":               func(d *Draft) { d.MaxSources = &negative },
		"no customer":                  func(d *Draft) { d.CustomerExternalID = "" },
		"no customer name":             func(d *Draft) { d.CustomerName = "" },
		"no license id":                func(d *Draft) { d.LicenseID = "" },
		"a license id of another form": func(d *Draft) { d.LicenseID = "0123456789ABCDEF" },
		"a name the key cannot hold":   func(d *Draft) { d.CustomerName = "Acme \xff" },
		"a plan the key cannot hold":   func(d *Draft) { d.Plan = "\xff" },
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			draft := validDraft(now)
			alter(draft)

			issued, key, err := signer.Issue(draft, now)

			require.Error(t, err)
			require.Nil(t, issued)
			require.Nil(t, key)
		})
	}

	_, _, err := signer.Issue(nil, now)
	require.Error(t, err)
	_, _, err = signer.Issue(validDraft(now), time.Time{})
	require.Error(t, err)
}

func Test_disagreement(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	request := func() *license.IssueRequest { return validDraft(now).request(now) }
	keyOf := func(req *license.IssueRequest) *license.Key {
		return &license.Key{
			Version: "v1", Id: req.Id, IssuedTo: req.IssuedTo, CustomerId: req.CustomerId,
			IssuedAt: req.IssuedAt, ExpiresAt: req.ExpiresAt, GraceDays: req.GraceDays, Limits: req.Limits,
			Plan: req.Plan, Features: req.Features, Telemetry: req.Telemetry,
		}
	}
	require.Empty(t, disagreement(keyOf(request()), request()))

	other := 99
	cases := map[string]func(k *license.Key){
		"id":          func(k *license.Key) { k.Id = "fedcba9876543210" },
		"issued_to":   func(k *license.Key) { k.IssuedTo = "Another" },
		"customer_id": func(k *license.Key) { k.CustomerId = "another" },
		"issued_at":   func(k *license.Key) { k.IssuedAt = k.IssuedAt.Add(time.Second) },
		"expires_at":  func(k *license.Key) { k.ExpiresAt = k.ExpiresAt.Add(time.Second) },
		"grace_days":  func(k *license.Key) { k.GraceDays = nil },
		"limits":      func(k *license.Key) { k.Limits = &license.Limits{MaxSources: &other} },
		"plan":        func(k *license.Key) { k.Plan = "" },
		"features":    func(k *license.Key) { k.Features = nil },
		"telemetry":   func(k *license.Key) { k.Telemetry = "" },
	}
	for field, alter := range cases {
		t.Run(field, func(t *testing.T) {
			key := keyOf(request())
			alter(key)
			require.Equal(t, field, disagreement(key, request()))
		})
	}

	t.Run("a limit the draft does not set", func(t *testing.T) {
		key := keyOf(request())
		key.Limits = &license.Limits{MaxSources: key.Limits.MaxSources, MaxJobs: &other}
		require.Equal(t, "limits", disagreement(key, request()))
	})
	t.Run("a list where the draft has none", func(t *testing.T) {
		req := request()
		req.Features = nil
		key := keyOf(req)
		key.Features = []string{}
		require.Equal(t, "features", disagreement(key, req))
	})
}
