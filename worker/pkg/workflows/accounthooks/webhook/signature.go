package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
)

// bodySignature is the HMAC-SHA256 of the body under the secret, in lowercase hexadecimal.
func bodySignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// timestampedSignature is the signature of the Standard Webhooks specification: the
// HMAC-SHA256 of "<id>.<timestamp>.<body>" under the secret, in base64, behind its version.
// A receiver that checks the timestamp refuses a request replayed later.
func timestampedSignature(secret, id string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id + "." + strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
