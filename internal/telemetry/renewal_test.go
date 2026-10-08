package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const renewalLicenseID = "0123456789abcdef"

func validRenewalBody() string {
	return `{"schema_version":1,"license_id":"0123456789abcdef","instance_id":"inst-1","requested_at":"2026-10-08T10:00:00Z"}`
}

func Test_RenewalRequest_RoundTrips(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 30, 15, 987, time.FixedZone("x", 2*3600))
	request := NewRenewalRequest(renewalLicenseID, "inst-1", at)
	require.Equal(t, "2026-10-08T10:30:15Z", request.RequestedAt)

	body, err := request.Marshal()
	require.NoError(t, err)
	require.Equal(t,
		`{"schema_version":1,"license_id":"0123456789abcdef","instance_id":"inst-1","requested_at":"2026-10-08T10:30:15Z"}`,
		string(body))

	parsed, err := ParseRenewalRequest(body)
	require.NoError(t, err)
	require.Equal(t, request, parsed)
	require.True(t, parsed.At().Equal(at.Truncate(time.Second)))
	again, err := parsed.Marshal()
	require.NoError(t, err)
	require.Equal(t, body, again)
}

func Test_NewRenewalRequest_LicenseIdPassesThroughTheList(t *testing.T) {
	require.Equal(t, "other", NewRenewalRequest("contract-42", "inst-1", time.Now()).LicenseID)
}

func Test_RenewalRequest_AtIsZeroWhenUnreadable(t *testing.T) {
	require.True(t, (&RenewalRequest{RequestedAt: "soon"}).At().IsZero())
}

func Test_ParseRenewalRequest_Refuses(t *testing.T) {
	with := func(old, replacement string) string { return strings.Replace(validRenewalBody(), old, replacement, 1) }
	cases := map[string]string{
		"empty":                "",
		"not json":             "nope",
		"an array":             "[]",
		"unknown field":        with(`"instance_id"`, `"extra":1,"instance_id"`),
		"trailing data":        validRenewalBody() + ` {}`,
		"trailing garbage":     validRenewalBody() + `x`,
		"version 0":            with(`"schema_version":1`, `"schema_version":0`),
		"version 2":            with(`"schema_version":1`, `"schema_version":2`),
		"version missing":      with(`"schema_version":1,`, ``),
		"license id missing":   with(`"license_id":"0123456789abcdef",`, ``),
		"license id empty":     with(`"0123456789abcdef"`, `""`),
		"license id null":      with(`"0123456789abcdef"`, `null`),
		"license id a number":  with(`"0123456789abcdef"`, `7`),
		"instance id missing":  with(`"instance_id":"inst-1",`, ``),
		"instance id empty":    with(`"inst-1"`, `""`),
		"instance id too long": with(`"inst-1"`, `"`+strings.Repeat("a", 129)+`"`),
		"control character":    with(`"inst-1"`, `"in\nst"`),
		"invalid utf-8":        with(`"inst-1"`, "\"in\xffst\""),
		"instant missing":      with(`,"requested_at":"2026-10-08T10:00:00Z"`, ``),
		"instant not a time":   with(`2026-10-08T10:00:00Z`, `yesterday`),
		"instant with offset":  with(`2026-10-08T10:00:00Z`, `2026-10-08T12:00:00+02:00`),
		"instant fractional":   with(`2026-10-08T10:00:00Z`, `2026-10-08T10:00:00.5Z`),
		"instant date only":    with(`2026-10-08T10:00:00Z`, `2026-10-08`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			request, err := ParseRenewalRequest([]byte(body))
			require.Error(t, err)
			require.Nil(t, request)
		})
	}
}

func Test_ParseRenewalRequest_AcceptsTheBounds(t *testing.T) {
	body := strings.Replace(validRenewalBody(), `"inst-1"`, `"`+strings.Repeat("é", 128)+`"`, 1)
	_, err := ParseRenewalRequest([]byte(body))
	require.NoError(t, err)
}

func Test_ParseRenewalRequest_ErrorsNeverQuoteTheBody(t *testing.T) {
	for _, body := range []string{
		`{"schema_version":1,"license_id":"secret-id","instance_id":"secret-instance","requested_at":"secret-time"}`,
		`{"secret-field":"secret-value"}`,
		validRenewalBody() + `secret-tail`,
		`secret-garbage`,
	} {
		_, err := ParseRenewalRequest([]byte(body))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

func Test_RenewalAnswer_RoundTrips(t *testing.T) {
	body, err := (&RenewalAnswer{SchemaVersion: RenewalSchemaVersion, License: "abc=="}).Marshal()
	require.NoError(t, err)
	require.Equal(t, `{"schema_version":1,"license":"abc=="}`, string(body))
	parsed, err := ParseRenewalAnswer(body)
	require.NoError(t, err)
	require.Equal(t, &RenewalAnswer{SchemaVersion: 1, License: "abc=="}, parsed)
}

func Test_ParseRenewalAnswer_TrimsTheLicense(t *testing.T) {
	parsed, err := ParseRenewalAnswer([]byte(`{"schema_version":1,"license":"  abc==\n"}`))
	require.NoError(t, err)
	require.Equal(t, "abc==", parsed.License)
}

func Test_ParseRenewalAnswer_Refuses(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"not json":        "nope",
		"unknown field":   `{"schema_version":1,"license":"a","extra":1}`,
		"trailing data":   `{"schema_version":1,"license":"a"} {}`,
		"version 0":       `{"schema_version":0,"license":"a"}`,
		"version 2":       `{"schema_version":2,"license":"a"}`,
		"version missing": `{"license":"a"}`,
		"license missing": `{"schema_version":1}`,
		"license empty":   `{"schema_version":1,"license":""}`,
		"license blank":   `{"schema_version":1,"license":" \n\t"}`,
		"license null":    `{"schema_version":1,"license":null}`,
		"license number":  `{"schema_version":1,"license":1}`,
		"license too big": `{"schema_version":1,"license":"` + strings.Repeat("a", 16<<10+1) + `"}`,
		"invalid utf-8":   "{\"schema_version\":1,\"license\":\"a\xff\"}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			answer, err := ParseRenewalAnswer([]byte(body))
			require.Error(t, err)
			require.Nil(t, answer)
		})
	}
}

func Test_ParseRenewalAnswer_AcceptsTheLargestLicense(t *testing.T) {
	_, err := ParseRenewalAnswer([]byte(`{"schema_version":1,"license":"` + strings.Repeat("a", 16<<10) + `"}`))
	require.NoError(t, err)
}

func Test_ParseRenewalAnswer_ErrorsNeverQuoteTheBody(t *testing.T) {
	_, err := ParseRenewalAnswer([]byte(`{"schema_version":1,"license":"secret","secret":1}`))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
}

func Test_RenewalRequest_IsSealedLikeAReport(t *testing.T) {
	key := mintKey(t, `{"version":"1","id":"`+renewalLicenseID+`"}`, "")
	body, err := NewRenewalRequest(renewalLicenseID, "inst-1", time.Now()).Marshal()
	require.NoError(t, err)

	sealed, err := Seal(key, body)
	require.NoError(t, err)
	require.NoError(t, Verify(key, body, sealed))

	require.Error(t, Verify(key, append([]byte(" "), body...), sealed))
	require.Error(t, Verify(mintKey(t, `{"version":"1","id":"other"}`, ""), body, sealed))
}

func Test_RenewalConstants(t *testing.T) {
	require.Equal(t, "/v1/license-renewals", RenewalPath)
	require.Equal(t, 4096, RenewalBodyCap)
	require.Equal(t, 65536, RenewalAnswerCap)
	require.Equal(t, 5*time.Minute, RenewalFreshness)
	require.Equal(t, 1, RenewalSchemaVersion)
}
