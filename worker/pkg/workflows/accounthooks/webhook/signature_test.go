package webhook

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The signature of the body is a contract with the receivers.
func Test_BodySignature_IsTheHexHmacOfTheBody(t *testing.T) {
	require.Equal(
		t,
		"1e4ce99f85f395e4e1a0a4e0415cb0e1afbe678b18b437dc2a9571134683af4e",
		bodySignature(goldenSecret, []byte(goldenSucceededBody)),
	)
}

func Test_BodySignature_AcceptsAnEmptySecret(t *testing.T) {
	// HMAC-SHA256 of the empty message under the empty key.
	require.Equal(
		t,
		"b613679a0814d9ec772f95d778c35fc5ff1697c493715653c6c712144292c5ad",
		bodySignature("", nil),
	)
}

// The timestamped signature covers "<id>.<timestamp>.<body>": the value below was computed
// apart from this code, from that formula.
func Test_TimestampedSignature_CoversIdTimestampAndBody(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	body := []byte(goldenSucceededBody)

	signature := timestampedSignature(goldenSecret, id, 1700000000, body)
	require.Equal(t, "v1,BWpGkz8rJJJy3xbQE6Hk1cCifdq/4bzvHaqkuzQ1+6E=", signature)

	require.NotEqual(t, signature, timestampedSignature(goldenSecret, "another-id", 1700000000, body))
	require.NotEqual(t, signature, timestampedSignature(goldenSecret, id, 1700000001, body))
	require.NotEqual(t, signature, timestampedSignature(goldenSecret, id, 1700000000, []byte(goldenFailedBody)))
	require.NotEqual(t, signature, timestampedSignature("another-secret", id, 1700000000, body))
}
