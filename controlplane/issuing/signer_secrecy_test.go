package issuing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyRenderings gives the ways a printer could write the secret half of priv: windows of it in
// hexadecimal, in decimal, as raw bytes, and its base64. The public half is not secret and is left
// out: the ring a signer holds prints it.
func keyRenderings(priv ed25519.PrivateKey) []string {
	seed := priv.Seed()
	var renderings []string
	for _, window := range [][]byte{seed[:8], seed[12:20], seed[24:32]} {
		// As fmt writes a slice of bytes under each verb, without what surrounds it.
		for _, verb := range contentVerbs {
			rendering := fmt.Sprintf(verb, window)
			rendering = strings.TrimPrefix(rendering, "[]byte{")
			rendering = strings.Trim(rendering, `[]{}"`)
			renderings = append(renderings, rendering)
		}
		var spacedHex, decimal []string
		for _, b := range window {
			spacedHex = append(spacedHex, fmt.Sprintf("%02x", b))
			decimal = append(decimal, fmt.Sprintf("%d", b))
		}
		renderings = append(renderings,
			hex.EncodeToString(window),
			// As a hexadecimal dump writes them.
			strings.Join(spacedHex, " "),
			strings.Join(decimal, ","),
			strings.Join(decimal, ", "),
		)
	}
	return append(renderings,
		base64.StdEncoding.EncodeToString(seed)[:40],
		base64.RawURLEncoding.EncodeToString(seed)[:40],
	)
}

// holdsKey reports whether printed holds a rendering of the secret half of priv, in either case.
func holdsKey(priv ed25519.PrivateKey, printed string) bool {
	lowered := strings.ToLower(printed)
	for _, rendering := range keyRenderings(priv) {
		if strings.Contains(printed, rendering) {
			return true
		}
		if isASCII(rendering) && strings.Contains(lowered, strings.ToLower(rendering)) {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func requireNoKey(t *testing.T, priv ed25519.PrivateKey, printed string) {
	t.Helper()
	require.NotEmpty(t, printed)
	if holdsKey(priv, printed) {
		// The rendering found is not put in the failure: it is the key.
		t.Fatalf("what was printed holds bytes of the private key:\n%.300q", printed)
	}
}

// The verbs that write the content of a value; %p writes where it is.
var contentVerbs = []string{"%v", "%+v", "%#v", "%s", "%q", "%+q", "%x", "%X", "% x", "%d", "%o", "%b", "%c", "%U", "%e", "%t"}

var everyVerb = append([]string{"%p"}, contentVerbs...)

func Test_holdsKey_FindsAPrintedKey(t *testing.T) {
	priv, _ := newKeyPair(t)
	holders := map[string]any{
		"a field":            struct{ key ed25519.PrivateKey }{priv},
		"a pointer":          &struct{ key ed25519.PrivateKey }{priv},
		"a field of a field": struct{ inner struct{ key []byte } }{struct{ key []byte }{priv}},
	}
	for name, holder := range holders {
		for _, verb := range contentVerbs {
			require.True(t, holdsKey(priv, fmt.Sprintf(verb, holder)), "%s printed under %s", name, verb)
		}
	}
	var text bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("held", "value", holders["a field"])
	require.True(t, holdsKey(priv, text.String()), "through the text handler of slog")

	recorded := &failures{}
	assert.Equal(recorded, holders["a field"], struct{ key ed25519.PrivateKey }{})
	require.True(t, holdsKey(priv, recorded.printed.String()), "through the diff of testify")

	other, _ := newKeyPair(t)
	require.False(t, holdsKey(priv, fmt.Sprintf("%v %x %d", other, other, other)), "another key is not this one")
}

func Test_Signer_HeldInAStruct_NeverPrintsItsKey(t *testing.T) {
	priv, ring := newKeyPair(t)
	signer, err := NewSigner(priv, ring)
	require.NoError(t, err)

	type byPointer struct {
		name   string
		signer *Signer
	}
	type byValue struct {
		name   string
		signer Signer
	}
	type exported struct {
		Name    string
		Signer  *Signer
		ByValue Signer
	}
	held := map[string]any{
		"the signer":               signer,
		"the signer by value":      *signer,
		"an unexported pointer":    byPointer{"console", signer},
		"an unexported value":      byValue{"console", *signer},
		"a pointer to the holder":  &byValue{"console", *signer},
		"exported fields":          exported{"console", signer, *signer},
		"a slice of holders":       []byValue{{"console", *signer}},
		"a map of holders":         map[string]byPointer{"console": {"console", signer}},
		"an interface in a holder": struct{ any any }{*signer},
	}
	for name, value := range held {
		t.Run(name, func(t *testing.T) {
			for _, verb := range everyVerb {
				requireNoKey(t, priv, fmt.Sprintf(verb, value))
			}
			requireNoKey(t, priv, fmt.Sprint(value))
			requireNoKey(t, priv, fmt.Sprintln(value))

			var text, json bytes.Buffer
			slog.New(slog.NewTextHandler(&text, nil)).Info("held", "value", value, slog.Any("any", value))
			slog.New(slog.NewJSONHandler(&json, nil)).Info("held", "value", value, slog.Any("any", value))
			requireNoKey(t, priv, text.String())
			requireNoKey(t, priv, json.String())
		})
	}
}

// failures keeps what testify would print for a failed assertion: its diff walks the fields of
// both values with spew, methods disabled.
type failures struct{ printed strings.Builder }

func (f *failures) Errorf(format string, args ...any) {
	fmt.Fprintf(&f.printed, format, args...)
}

func Test_Signer_ComparedByTestify_NeverPrintsItsKey(t *testing.T) {
	priv, ring := newKeyPair(t)
	one, err := NewSigner(priv, ring)
	require.NoError(t, err)
	otherPriv, otherRing := newKeyPair(t)
	other, err := NewSigner(otherPriv, otherRing)
	require.NoError(t, err)

	require.False(t, assert.ObjectsAreEqual(one, other))

	type holder struct {
		name   string
		signer Signer
	}
	pairs := map[string][2]any{
		"two signers":          {one, other},
		"two signers by value": {*one, *other},
		"two holders":          {holder{"a", *one}, holder{"b", *other}},
		"two slices":           {[]*Signer{one}, []*Signer{other}},
	}
	for name, pair := range pairs {
		t.Run(name, func(t *testing.T) {
			recorded := &failures{}

			require.False(t, assert.Equal(recorded, pair[0], pair[1]))
			assert.EqualValues(recorded, pair[0], pair[1])
			assert.Same(recorded, &pair[0], &pair[1])
			assert.Nil(recorded, pair[0])
			assert.Empty(recorded, pair[0])

			printed := recorded.printed.String()
			require.Contains(t, printed, "Diff:", "the diff of testify, which dumps both values, was printed")
			requireNoKey(t, priv, printed)
			requireNoKey(t, otherPriv, printed)
		})
	}
}

func Test_Signer_HoldsNoKeyBytes(t *testing.T) {
	priv, ring := newKeyPair(t)
	signer, err := NewSigner(priv, ring)
	require.NoError(t, err)

	// The key given is not the one kept: clearing it does not stop the signer.
	clear(priv)
	_, _, err = signer.Issue(validDraft(testNow()), testNow())
	require.NoError(t, err)
}

func Test_Signer_ZeroValue_IssuesNothing(t *testing.T) {
	var signer Signer
	_, _, err := signer.Issue(validDraft(testNow()), testNow())
	require.Error(t, err)

	var none *Signer
	_, _, err = none.Issue(validDraft(testNow()), testNow())
	require.Error(t, err)
}

func Test_Signer_PublicKeyFingerprint(t *testing.T) {
	priv, ring := newKeyPair(t)
	signer, err := NewSigner(priv, ring)
	require.NoError(t, err)

	require.Equal(t, license.PublicKeyFingerprint(ring[testKid]), signer.PublicKeyFingerprint())
	require.Len(t, signer.PublicKeyFingerprint(), 16)
	require.NotEqual(t, hex.EncodeToString(priv.Seed())[:16], signer.PublicKeyFingerprint())
}

// countingReader counts what is read through it.
type countingReader struct {
	reader io.Reader
	read   int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.read += n
	return n, err
}

func Test_readKey_StopsAtTheBound(t *testing.T) {
	const mebibyte = 1 << 20
	source := &countingReader{reader: strings.NewReader(strings.Repeat("M", mebibyte))}

	raw, err := readKey(source)

	require.ErrorIs(t, err, ErrKeyFileTooLarge)
	require.Nil(t, raw)
	require.LessOrEqual(t, source.read, maxKeyFileSize+1, "nothing is read past the bound")
}

func Test_LoadSigner_RefusesALargeFile(t *testing.T) {
	priv, ring := newKeyPair(t)
	// A real key at the head of a file that is far too large: the size refuses it, not its content.
	content := append(pemOf(t, priv), bytes.Repeat([]byte("\n"), 1<<20)...)
	path := filepath.Join(t.TempDir(), "large.key")
	require.NoError(t, os.WriteFile(path, content, 0o600))

	signer, err := LoadSigner(path, ring)

	require.ErrorIs(t, err, ErrKeyFileTooLarge)
	require.Equal(t, ErrKeyFileTooLarge.Error(), err.Error())
	require.Nil(t, signer)

	oneOver := append(pemOf(t, priv), bytes.Repeat([]byte("\n"), maxKeyFileSize)...)[:maxKeyFileSize+1]
	_, err = LoadSigner(writeFile(t, oneOver), ring)
	require.ErrorIs(t, err, ErrKeyFileTooLarge)
}
