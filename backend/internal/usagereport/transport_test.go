package usagereport

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// The document of the tests is not the JSON an encoder would write again: its spaces, the order
// of its keys and its last line must reach the receiver as they are.
var storedReport = &usagestore.StoredReport{
	Day:            time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	Document:       []byte("{\"b\": 1,   \"a\": [ 2 ,3 ],\n \"text\": \"\\u00e9\"}\n"),
	Seal:           "v1:c2VhbA==",
	KeyFingerprint: "sha256:0123abcd",
}

// received is one request as the receiver saw it.
type received struct {
	method, path string
	header       http.Header
	body         []byte
}

// receiver is a destination that answers every request with one status, and keeps what it got.
type receiver struct {
	*httptest.Server
	mu       sync.Mutex
	requests []received
}

func newReceiver(t *testing.T, status int, header http.Header) *receiver {
	t.Helper()
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{req.Method, req.URL.Path, req.Header.Clone(), body})
		r.mu.Unlock()
		for name, values := range header {
			w.Header()[name] = values
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) got() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.requests...)
}

func transportTo(t *testing.T, address string) Transport {
	t.Helper()
	transport, err := NewHTTPTransport(address, "v0.2.0")
	require.NoError(t, err)
	return transport
}

func Test_Post_SendsTheStoredDocumentByteForByteWithItsHeaders(t *testing.T) {
	destination := newReceiver(t, http.StatusNoContent, nil)

	require.NoError(t, transportTo(t, destination.URL+"/v1/usage-reports").Post(t.Context(), storedReport))

	requests := destination.got()
	require.Len(t, requests, 1)
	got := requests[0]
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/v1/usage-reports", got.path)
	require.True(t, bytes.Equal(storedReport.Document, got.body), "the body is not the stored document: %q", got.body)
	require.Equal(t, "application/json", got.header.Get("Content-Type"))
	require.Equal(t, "v1:c2VhbA==", got.header.Get("Husonym-Seal"))
	require.Equal(t, "sha256:0123abcd", got.header.Get("Husonym-Key-Fingerprint"))
	require.Equal(t, "husonym/v0.2.0", got.header.Get("User-Agent"))
}

func Test_Post_TheUserAgentCarriesTheGuardedVersion(t *testing.T) {
	destination := newReceiver(t, http.StatusOK, nil)
	transport, err := NewHTTPTransport(destination.URL, "a build of someone")
	require.NoError(t, err)

	require.NoError(t, transport.Post(t.Context(), storedReport))
	require.Equal(t, "husonym/other", destination.got()[0].header.Get("User-Agent"))
}

func Test_Post_AnAnswerThatIsNot2xxIsAnErrorThatDoesNotQuoteIt(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("what the receiver answered"))
	}))
	t.Cleanup(destination.Close)

	err := transportTo(t, destination.URL).Post(t.Context(), storedReport)
	require.ErrorContains(t, err, "500")
	require.NotContains(t, err.Error(), "what the receiver answered")
}

func Test_Post_ARedirectIsAnErrorAndIsNotFollowed(t *testing.T) {
	elsewhere := newReceiver(t, http.StatusNoContent, nil)
	destination := newReceiver(t, http.StatusFound, http.Header{"Location": {elsewhere.URL}})

	err := transportTo(t, destination.URL).Post(t.Context(), storedReport)
	require.ErrorContains(t, err, "302")
	require.Len(t, destination.got(), 1)
	require.Empty(t, elsewhere.got())
}

func Test_Post_ADestinationThatDoesNotAnswerIsAnErrorWithinTheBound(t *testing.T) {
	release := make(chan struct{})
	destination := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		select {
		case <-release:
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(destination.Close)
	t.Cleanup(func() { close(release) })

	target, err := checkReportURL(destination.URL)
	require.NoError(t, err)
	transport := newHTTPTransport(target, "v0.2.0", 50*time.Millisecond)

	started := time.Now()
	require.Error(t, transport.Post(t.Context(), storedReport))
	require.Less(t, time.Since(started), 5*time.Second)
}

func Test_Post_AnAnswerIsReadOnlyUpToItsBound(t *testing.T) {
	var written atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		chunk := bytes.Repeat([]byte{'x'}, 32<<10)
		// Far more than the bound: the handler ends when the client hangs up.
		for range 4096 {
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil || req.Context().Err() != nil {
				return
			}
		}
	}))
	t.Cleanup(destination.Close)

	require.NoError(t, transportTo(t, destination.URL).Post(t.Context(), storedReport))
	destination.Close()
	require.Less(t, written.Load(), int64(64<<20), "the whole answer was read")
}

func Test_Post_AnErrorNeverQuotesTheCredentialsOfTheAddress(t *testing.T) {
	destination := newReceiver(t, http.StatusNoContent, nil)
	address := strings.Replace(destination.URL, "http://", "http://someone:hunter2@", 1)
	destination.Close()

	err := transportTo(t, address).Post(t.Context(), storedReport)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2")
	require.NotContains(t, err.Error(), "someone")
	require.Contains(t, err.Error(), strings.TrimPrefix(destination.URL, "http://"))
}

func Test_NewHTTPTransport_RefusesAnAddressItCannotSendTo(t *testing.T) {
	for name, address := range map[string]string{
		"no address":      "",
		"not a URL":       "://nowhere",
		"another scheme":  "ftp://reports.example.com/in",
		"a file":          "file:///etc/passwd",
		"no host":         "https:///v1/usage-reports",
		"a host and port": "reports.example.com:443",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewHTTPTransport(address, "v0.2.0")
			require.Error(t, err)
		})
	}
}

func Test_NewHTTPTransport_AnErrorNeverQuotesTheCredentialsOfTheAddress(t *testing.T) {
	_, err := NewHTTPTransport("ftp://someone:hunter2@reports.example.com/in", "v0.2.0")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2")
}

func Test_NewHTTPTransport_TakesTheDefaultAddress(t *testing.T) {
	_, err := NewHTTPTransport(DefaultReportURL, "v0.2.0")
	require.NoError(t, err)
}

func setReportURL(t *testing.T, value string) {
	t.Helper()
	viper.Set(reportURLVariable, value)
	t.Cleanup(func() { viper.Set(reportURLVariable, nil) })
}

func Test_ReportURLFromEnvironment_IsTheDefaultOneWithoutTheVariable(t *testing.T) {
	require.Equal(t, DefaultReportURL, ReportURLFromEnvironment(slog.New(slog.DiscardHandler)))
}

func Test_ReportURLFromEnvironment_TakesAnAddressThatCanBeSentTo(t *testing.T) {
	setReportURL(t, " http://127.0.0.1:9999/in ")
	require.Equal(t, "http://127.0.0.1:9999/in", ReportURLFromEnvironment(slog.New(slog.DiscardHandler)))
}

func Test_ReportURLFromEnvironment_KeepsTheDefaultOneAndSaysSoWhenTheVariableIsNotValid(t *testing.T) {
	var logs syncBuffer
	setReportURL(t, "ftp://someone:hunter2@reports.example.com/in")

	require.Equal(t, DefaultReportURL, ReportURLFromEnvironment(slog.New(slog.NewTextHandler(&logs, nil))))
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), reportURLVariable)
	require.NotContains(t, logs.String(), "hunter2")
}
