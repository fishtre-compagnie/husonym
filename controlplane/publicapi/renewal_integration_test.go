package publicapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

type renewalReply struct {
	status int
	header http.Header
	body   []byte
}

func postRenewal(t *testing.T, url string, ask cptest.SealedRenewal) renewalReply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/license-renewals",
		bytes.NewReader(ask.Document))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Husonym-Seal", ask.Seal)
	req.Header.Set("Husonym-Key-Fingerprint", ask.Fingerprint)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// The date is the one of the answer, not something it says of the request.
	resp.Header.Del("Date")
	return renewalReply{status: resp.StatusCode, header: resp.Header, body: body}
}

// Review focus: a key nobody recorded, a wrong seal for a license that is known and a license
// nothing succeeds are answered the same, to the byte: nothing tells a caller that a fingerprint
// is the one of a license.
func Test_Renewal_OverTheRealStore_TheThreeWaysToBeGivenNothingAnswerTheSame(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	renewed := issuer.Entry("lic-renewed", "cust-1", "Acme")
	successor := issuer.Entry("lic-successor", "cust-1", "Acme")
	lone := issuer.Entry("lic-lone", "cust-2", "Globex")
	unknown := issuer.Entry("lic-unknown", "cust-3", "Initech")
	cptest.AddLicense(t, store, issuer, &renewed)
	cptest.AddLicense(t, store, issuer, &successor)
	cptest.AddLicense(t, store, issuer, &lone)
	cptest.Succeed(t, pool, "lic-successor", "lic-renewed")
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	server := httptest.NewServer(publicapi.NewHandler(
		intake.New(store, clock), renewal.New(store, clock, logger), nil, logger))
	t.Cleanup(server.Close)

	withoutSuccessor := postRenewal(t, server.URL, cptest.RenewalFor(t, &lone, instanceA, now))
	fromNobody := postRenewal(t, server.URL, cptest.RenewalFor(t, &unknown, instanceA, now))
	// The license that has a successor, asked for under a seal that is not the one of the request.
	forged := cptest.RenewalFor(t, &renewed, instanceA, now)
	forged.Seal = cptest.RenewalFor(t, &unknown, instanceA, now).Seal
	wrongSeal := postRenewal(t, server.URL, forged)

	require.Equal(t, http.StatusNoContent, withoutSuccessor.status)
	require.Empty(t, withoutSuccessor.body)
	require.Equal(t, "no-store", withoutSuccessor.header.Get("Cache-Control"))
	require.Equal(t, withoutSuccessor, fromNobody)
	require.Equal(t, withoutSuccessor, wrongSeal)
	require.Equal(t, 1, count(t, pool, "seal_rejections"), "the wrong seal is counted under its license")
	require.Equal(t, 1, count(t, pool, "renewal_asks"), "only the ask whose seal verified is recorded")

	// The holder of the key is given the successor, as it is stored.
	served := postRenewal(t, server.URL, cptest.RenewalFor(t, &renewed, instanceA, now))
	require.Equal(t, http.StatusOK, served.status)
	require.Equal(t, "application/json", served.header.Get("Content-Type"))
	require.Equal(t, "no-store", served.header.Get("Cache-Control"))
	answer, err := telemetry.ParseRenewalAnswer(served.body)
	require.NoError(t, err)
	require.Equal(t, successor.Encoded, answer.License)

	// A request made too long ago is refused, whoever makes it.
	stale := postRenewal(t, server.URL, cptest.RenewalFor(t, &renewed, instanceA, now.Add(-time.Hour)))
	require.Equal(t, http.StatusBadRequest, stale.status)
	require.Empty(t, stale.body)

	// Five requests, five lines, and nothing in them of a key, a seal, a fingerprint or an instance.
	logged := logs.String()
	require.Equal(t, 5, bytes.Count([]byte(logged), []byte("\n")))
	for _, secret := range []string{
		successor.Encoded, renewed.Encoded, lone.Encoded, forged.Seal, forged.Fingerprint, instanceA,
		"lic-renewed", "lic-successor", "lic-lone",
	} {
		require.NotContains(t, logged, secret)
	}
}
