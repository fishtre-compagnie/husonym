package utils

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_LenientBool(t *testing.T) {
	type payload struct {
		Verified LenientBool `json:"email_verified"`
		Rest     string      `json:"rest"`
	}

	cases := []struct {
		name  string
		claim string
		want  bool
	}{
		{"a boolean true", `true`, true},
		{"a boolean false", `false`, false},
		{"the string spelling of true", `"true"`, true},
		{"the string spelling of false", `"false"`, false},
		{"a string nobody anticipated", `"yes"`, false},
		{"a number", `1`, false},
		{"null", `null`, false},
		{"an object", `{"value": true}`, false},
		{"an array", `[true]`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var decoded payload
			body := `{"email_verified": ` + tc.claim + `, "rest": "kept"}`

			// What matters is not only the value: the rest of the payload must survive.
			// A shape this type refused would fail the whole sign-in.
			require.NoError(t, json.Unmarshal([]byte(body), &decoded))
			require.Equal(t, tc.want, bool(decoded.Verified))
			require.Equal(t, "kept", decoded.Rest)
		})
	}

	t.Run("an absent claim is false", func(t *testing.T) {
		var decoded payload
		require.NoError(t, json.Unmarshal([]byte(`{"rest": "kept"}`), &decoded))
		require.False(t, bool(decoded.Verified))
	})
}
