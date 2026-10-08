package usagereport

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

var sealedAsk = &RenewalAsk{
	Document:       []byte(`{"schema_version":1,"license_id":"8f2a41c09b7e63d5","instance_id":"i","requested_at":"2026-10-07T00:05:12Z"}`),
	Seal:           "v1:c2VhbA==",
	KeyFingerprint: "sha256:0123abcd",
}

// answering is a destination that answers every request with one status and one body.
func answering(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(destination.Close)
	return destination
}

func renewalTransportTo(t *testing.T, reportAddress string) RenewalTransport {
	t.Helper()
	transport, err := NewHTTPRenewalTransport(reportAddress, "v0.2.0")
	require.NoError(t, err)
	return transport
}

// renewalURL is the address renewalTarget derives, as it is written.
func renewalURL(t *testing.T, reportAddress string) string {
	t.Helper()
	target, err := renewalTarget(reportAddress)
	require.NoError(t, err)
	return target.String()
}

func Test_RenewalTarget_OfTheDefaultReportAddress_IsTheDefaultRenewalAddress(t *testing.T) {
	require.Equal(t, "https://license.husonym.com/v1/license-renewals", renewalURL(t, DefaultReportURL))
}

func Test_RenewalTarget_IsTheOriginOfTheReportAddressWithTheRenewalPath(t *testing.T) {
	for name, tc := range map[string]struct{ report, want string }{
		"a path replaced":             {"http://127.0.0.1:9999/v1/usage-reports", "http://127.0.0.1:9999/v1/license-renewals"},
		"no path":                     {"https://reports.example.com", "https://reports.example.com/v1/license-renewals"},
		"a deeper path":               {"https://reports.example.com/a/b/c/", "https://reports.example.com/v1/license-renewals"},
		"credentials kept":            {"http://someone:hunter2@127.0.0.1:9999/in", "http://someone:hunter2@127.0.0.1:9999/v1/license-renewals"},
		"a query and a fragment gone": {"https://reports.example.com/in?token=hunter3#top", "https://reports.example.com/v1/license-renewals"},
		// The whole path is replaced: a receiver behind a prefix is asked at the root of its host.
		"a path prefix is not kept": {"https://gateway.example.com/husonym/v1/usage-reports", "https://gateway.example.com/v1/license-renewals"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, renewalURL(t, tc.report))
		})
	}
}

// The headers of an ask are these and no other: nothing of the instance leaves in a header that
// nobody chose to send.
func Test_Ask_SendsTheseHeadersAndNoOther(t *testing.T) {
	names := func(header http.Header) []string {
		got := make([]string, 0, len(header))
		for name := range header {
			got = append(got, name)
		}
		return got
	}
	// The four the transport sets, and the three net/http adds to a request with a body that has a
	// connection of its own.
	set := []string{"Content-Type", "User-Agent", "Husonym-Seal", "Husonym-Key-Fingerprint"}
	added := []string{"Content-Length", "Accept-Encoding", "Connection"}

	t.Run("an address without credentials", func(t *testing.T) {
		destination := newReceiver(t, http.StatusNoContent, nil)

		_, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
		require.NoError(t, err)

		requests := destination.got()
		require.Len(t, requests, 1)
		require.ElementsMatch(t, append(set, added...), names(requests[0].header))
		require.Equal(t, []string{"gzip"}, requests[0].header.Values("Accept-Encoding"))
		require.Equal(t, []string{"close"}, requests[0].header.Values("Connection"))
		require.Equal(t, []string{strconv.Itoa(len(sealedAsk.Document))}, requests[0].header.Values("Content-Length"))
	})

	t.Run("an address with credentials", func(t *testing.T) {
		destination := newReceiver(t, http.StatusNoContent, nil)
		address := strings.Replace(destination.URL, "http://", "http://someone:hunter2@", 1)

		_, err := renewalTransportTo(t, address).Ask(t.Context(), sealedAsk)
		require.NoError(t, err)

		requests := destination.got()
		require.Len(t, requests, 1)
		require.ElementsMatch(t, append(append(set, added...), "Authorization"), names(requests[0].header))
		user, password, ok := (&http.Request{Header: requests[0].header}).BasicAuth()
		require.True(t, ok)
		require.Equal(t, "someone", user)
		require.Equal(t, "hunter2", password)
	})
}

func Test_NewHTTPRenewalTransport_RefusesAnAddressAReportCannotBeSentTo(t *testing.T) {
	for name, address := range map[string]string{
		"no address":     "",
		"not a URL":      "://nowhere",
		"another scheme": "ftp://someone:hunter2@reports.example.com/in",
		"no host":        "https:///v1/usage-reports",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewHTTPRenewalTransport(address, "v0.2.0")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "hunter2")
		})
	}
}

func Test_Ask_PostsTheSealedRequestToTheRenewalPathOfTheOrigin(t *testing.T) {
	destination := newReceiver(t, http.StatusNoContent, nil)

	license, err := renewalTransportTo(t, destination.URL+"/v1/usage-reports").Ask(t.Context(), sealedAsk)
	require.NoError(t, err)
	require.Empty(t, license)

	requests := destination.got()
	require.Len(t, requests, 1)
	got := requests[0]
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, telemetry.RenewalPath, got.path)
	require.True(t, bytes.Equal(sealedAsk.Document, got.body), "the body is not the sealed request: %q", got.body)
	require.Equal(t, "application/json", got.header.Get("Content-Type"))
	require.Equal(t, "v1:c2VhbA==", got.header.Get("Husonym-Seal"))
	require.Equal(t, "sha256:0123abcd", got.header.Get("Husonym-Key-Fingerprint"))
	require.Equal(t, "husonym/v0.2.0", got.header.Get("User-Agent"))
}

func Test_Ask_A200GivesTheLicenseOfTheAnswer(t *testing.T) {
	destination := answering(t, http.StatusOK, `{"schema_version":1,"license":" the-license \n"}`)

	license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
	require.NoError(t, err)
	require.Equal(t, "the-license", license)
}

func Test_Ask_A204IsNothingToGiveWhateverItCarries(t *testing.T) {
	destination := newReceiver(t, http.StatusNoContent, http.Header{"X-License": {"what the receiver answered"}})

	license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
	require.NoError(t, err)
	require.Empty(t, license)
}

func Test_Ask_AnyOtherStatusIsAnErrorThatDoesNotQuoteTheAnswer(t *testing.T) {
	for _, status := range []int{
		http.StatusCreated, http.StatusAccepted, http.StatusPartialContent,
		http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusInternalServerError, http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// A license in the body of an answer that is not a 200 is not one.
			destination := answering(t, status, `{"schema_version":1,"license":"what the receiver answered"}`)
			host := strings.TrimPrefix(destination.URL, "http://")

			license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
			require.EqualError(t, err, host+" answered with the status "+strconv.Itoa(status))
			require.Empty(t, license)
		})
	}
}

func Test_Ask_ARedirectIsAnErrorAndIsNotFollowed(t *testing.T) {
	elsewhere := answering(t, http.StatusOK, `{"schema_version":1,"license":"from elsewhere"}`)
	var asked atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.Header().Set("Location", elsewhere.URL+telemetry.RenewalPath)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(destination.Close)

	license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
	require.ErrorContains(t, err, "307")
	require.Empty(t, license)
	require.Equal(t, int64(1), asked.Load())
}

func Test_Ask_ADestinationThatDoesNotAnswerIsAnErrorWithinTheBound(t *testing.T) {
	release := make(chan struct{})
	destination := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		select {
		case <-release:
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(destination.Close)
	t.Cleanup(func() { close(release) })

	target, err := renewalTarget(destination.URL)
	require.NoError(t, err)
	transport := newHTTPRenewalTransport(target, "v0.2.0", 50*time.Millisecond)

	started := time.Now()
	license, err := transport.Ask(t.Context(), sealedAsk)
	require.ErrorContains(t, err, ": timed out")
	require.Empty(t, license)
	require.Less(t, time.Since(started), 5*time.Second)
}

// The head of the answer comes at once and its body never ends: the bound is on the whole.
func Test_Ask_AnAnswerThatNeverEndsIsAnErrorWithinTheBound(t *testing.T) {
	release := make(chan struct{})
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"schema_version":1,`))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(destination.Close)
	t.Cleanup(func() { close(release) })

	target, err := renewalTarget(destination.URL)
	require.NoError(t, err)

	started := time.Now()
	license, err := newHTTPRenewalTransport(target, "v0.2.0", 50*time.Millisecond).Ask(t.Context(), sealedAsk)
	require.ErrorContains(t, err, ": timed out")
	require.Empty(t, license)
	require.Less(t, time.Since(started), 5*time.Second)
}

func Test_Ask_AnAnswerThatIsNotOneIsAnErrorThatDoesNotQuoteIt(t *testing.T) {
	for name, body := range map[string]string{
		"nothing":            "",
		"not JSON":           "what the receiver answered",
		"a page":             "<html><body>what the receiver answered</body></html>",
		"another version":    `{"schema_version":2,"license":"what the receiver answered"}`,
		"an unknown field":   `{"schema_version":1,"license":"what the receiver answered","more":true}`,
		"no license":         `{"schema_version":1,"license":"  "}`,
		"data after":         `{"schema_version":1,"license":"what the receiver answered"} what the receiver answered`,
		"a license too long": `{"schema_version":1,"license":"` + strings.Repeat("a", 16<<10+1) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			destination := answering(t, http.StatusOK, body)
			host := strings.TrimPrefix(destination.URL, "http://")

			license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
			require.ErrorContains(t, err, host+" answered with the status 200 and ")
			require.NotContains(t, err.Error(), "what the receiver answered")
			require.Less(t, len(err.Error()), 200)
			require.Empty(t, license)
		})
	}
}

func Test_Ask_AnAnswerOverItsBoundIsAnErrorAndIsNotReadWhole(t *testing.T) {
	var written atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// A well-formed answer once it ends, far over the bound: the handler ends when the client
		// hangs up.
		_, _ = w.Write([]byte(`{"schema_version":1,"license":"`))
		chunk := bytes.Repeat([]byte{'a'}, 32<<10)
		for range 4096 {
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil || req.Context().Err() != nil {
				return
			}
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(destination.Close)
	host := strings.TrimPrefix(destination.URL, "http://")

	license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
	require.EqualError(t, err, host+" answered with the status 200 and more than is read of an answer")
	require.Empty(t, license)
	destination.Close()
	require.Less(t, written.Load(), int64(64<<20), "the whole answer was read")
}

// An answer of exactly the bound is read whole: it is then judged as an answer.
func Test_Ask_AnAnswerOfExactlyItsBoundIsRead(t *testing.T) {
	head, tail := `{"schema_version":1,"license":"abc"`, `}`
	body := head + strings.Repeat(" ", telemetry.RenewalAnswerCap-len(head)-len(tail)) + tail
	require.Len(t, body, telemetry.RenewalAnswerCap)
	destination := answering(t, http.StatusOK, body)

	license, err := renewalTransportTo(t, destination.URL).Ask(t.Context(), sealedAsk)
	require.NoError(t, err)
	require.Equal(t, "abc", license)
}

func Test_Ask_AnErrorNeverQuotesTheCredentialsOfTheAddress(t *testing.T) {
	destination := answering(t, http.StatusInternalServerError, "")
	parsed, err := url.Parse(destination.URL)
	require.NoError(t, err)
	address := strings.Replace(destination.URL, "http://", "http://someone:hunter2@", 1)

	_, err = renewalTransportTo(t, address).Ask(t.Context(), sealedAsk)
	require.EqualError(t, err, parsed.Host+" answered with the status 500")
	destination.Close()
	_, err = renewalTransportTo(t, address).Ask(t.Context(), sealedAsk)
	require.EqualError(t, err, "unable to send to "+parsed.Host+": connection refused")
}
