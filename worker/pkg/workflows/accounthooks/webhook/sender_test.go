package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// received is what a receiver got of one request.
type received struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// newReceiver starts a receiver that records each request and answers with respond.
func newReceiver(t *testing.T, respond http.HandlerFunc) (*httptest.Server, func() []received) {
	t.Helper()
	requests := make(chan received, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- received{method: r.Method, path: r.RequestURI, header: r.Header.Clone(), body: body}
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []received {
		var got []received
		for {
			select {
			case request := <-requests:
				got = append(got, request)
			default:
				return got
			}
		}
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func goldenDelivery(target string) Delivery {
	return Delivery{
		ID:     "0123456789abcdef0123456789abcdef",
		URL:    target,
		Secret: goldenSecret,
		Event:  goldenRun.Succeeded(goldenAt),
	}
}

func fixedClock(s *Sender, unix int64) {
	s.now = func() time.Time { return time.Unix(unix, 0) }
}

func requireError(t *testing.T, err error) *Error {
	t.Helper()
	var failure *Error
	require.ErrorAs(t, err, &failure)
	return failure
}

func Test_Send_PostsTheSignedBody(t *testing.T) {
	srv, requests := newReceiver(t, status(http.StatusOK))
	sender := NewSender()
	fixedClock(sender, 1700000000)

	require.NoError(t, sender.Send(t.Context(), goldenDelivery(srv.URL+"/hook?token=abc")))

	got := requests()
	require.Len(t, got, 1)
	request := got[0]
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/hook?token=abc", request.path)
	require.Equal(t, goldenSucceededBody, string(request.body))

	// What the HTTP client adds by itself is set apart: the rest is what the sender sets.
	set := maps.Clone(request.header)
	delete(set, "Accept-Encoding")
	delete(set, "Content-Length")
	require.Equal(t, http.Header{
		"Content-Type":             {"application/json"},
		"User-Agent":               {"husonym"},
		"X-Husonym-Signature":      {"1e4ce99f85f395e4e1a0a4e0415cb0e1afbe678b18b437dc2a9571134683af4e"},
		"X-Husonym-Signature-Type": {"sha256"},
		"Webhook-Id":               {"0123456789abcdef0123456789abcdef"},
		"Webhook-Timestamp":        {"1700000000"},
		"Webhook-Signature":        {"v1,BWpGkz8rJJJy3xbQE6Hk1cCifdq/4bzvHaqkuzQ1+6E="},
	}, set)
	require.Empty(t, request.header.Get("Authorization"))
}

func Test_Send_ANewAttemptKeepsTheIdAndRenewsTheTimestamp(t *testing.T) {
	srv, requests := newReceiver(t, status(http.StatusOK))
	sender := NewSender()

	fixedClock(sender, 1700000000)
	require.NoError(t, sender.Send(t.Context(), goldenDelivery(srv.URL)))
	fixedClock(sender, 1700000005)
	require.NoError(t, sender.Send(t.Context(), goldenDelivery(srv.URL)))

	got := requests()
	require.Len(t, got, 2)
	first, second := got[0].header, got[1].header
	require.Equal(t, first.Get("Webhook-Id"), second.Get("Webhook-Id"))
	require.Equal(t, first.Get("X-Husonym-Signature"), second.Get("X-Husonym-Signature"))
	require.Equal(t, got[0].body, got[1].body)
	require.Equal(t, "1700000005", second.Get("Webhook-Timestamp"))
	require.NotEqual(t, first.Get("Webhook-Signature"), second.Get("Webhook-Signature"))
}

func Test_Send_ClassifiesTheAnswer(t *testing.T) {
	tests := []struct {
		status    int
		reason    Reason
		permanent bool
	}{
		{http.StatusOK, "", false},
		{http.StatusCreated, "", false},
		{http.StatusNoContent, "", false},
		{299, "", false},
		{http.StatusMovedPermanently, ReasonRedirected, true},
		{http.StatusNotModified, ReasonRedirected, true},
		{http.StatusBadRequest, ReasonRejected, true},
		{http.StatusUnauthorized, ReasonRejected, true},
		{http.StatusNotFound, ReasonRejected, true},
		{http.StatusGone, ReasonRejected, true},
		{http.StatusRequestTimeout, ReasonUnavailable, false},
		{http.StatusTooEarly, ReasonUnavailable, false},
		{http.StatusTooManyRequests, ReasonUnavailable, false},
		{http.StatusInternalServerError, ReasonUnavailable, false},
		{http.StatusBadGateway, ReasonUnavailable, false},
		{http.StatusServiceUnavailable, ReasonUnavailable, false},
		{599, ReasonUnavailable, false},
		{600, ReasonRejected, true},
	}
	sender := NewSender()
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			srv, requests := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				if tt.status != http.StatusNoContent && tt.status != http.StatusNotModified {
					_, _ = io.WriteString(w, "the receiver's words")
				}
			})

			err := sender.Send(t.Context(), goldenDelivery(srv.URL))

			require.Len(t, requests(), 1)
			if tt.reason == "" {
				require.NoError(t, err)
				return
			}
			failure := requireError(t, err)
			require.Equal(t, tt.reason, failure.Reason)
			require.Equal(t, tt.status, failure.Status)
			require.Equal(t, tt.permanent, failure.Permanent())
			require.Equal(t, strings.TrimPrefix(srv.URL, "http://"), failure.Host)
			require.Contains(t, failure.Error(), strconv.Itoa(tt.status))
			if tt.reason != ReasonRedirected {
				require.Equal(t, "the receiver's words", failure.Detail)
			}
		})
	}
}

func Test_Send_DoesNotFollowARedirect(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			other, otherRequests := newReceiver(t, status(http.StatusOK))
			srv, requests := newReceiver(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+"/elsewhere?token=abc", code)
			})

			err := NewSender().Send(t.Context(), goldenDelivery(srv.URL))

			failure := requireError(t, err)
			require.Equal(t, ReasonRedirected, failure.Reason)
			require.Equal(t, code, failure.Status)
			require.True(t, failure.Permanent())
			require.Contains(t, failure.Detail, strings.TrimPrefix(other.URL, "http://"))
			require.NotContains(t, failure.Error(), "token")
			require.Len(t, requests(), 1)
			require.Empty(t, otherRequests())
		})
	}
}

func Test_Send_KeepsAnExcerptOfALargeAnswer(t *testing.T) {
	srv, _ := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4<<20))
	})

	failure := requireError(t, NewSender().Send(t.Context(), goldenDelivery(srv.URL)))

	require.Equal(t, strings.Repeat("x", 512), failure.Detail)
}

func Test_Send_DoesNotReadAnAnswerToItsEnd(t *testing.T) {
	srv, _ := newReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	// The answer has no end: a sender that read it whole would do so until its timeout.
	sender := newSender(http.ProxyFromEnvironment, 30*time.Second)

	started := time.Now()
	failure := requireError(t, sender.Send(t.Context(), goldenDelivery(srv.URL)))

	require.Less(t, time.Since(started), 10*time.Second)
	require.Equal(t, ReasonRejected, failure.Reason)
	require.Len(t, failure.Detail, 512)
}

func Test_Send_MakesTheExcerptPrintable(t *testing.T) {
	srv, _ := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("  not\x00 here\r\n\x1b[31mnow\xff\xfe  "))
	})

	failure := requireError(t, NewSender().Send(t.Context(), goldenDelivery(srv.URL)))

	require.Equal(t, "not  here   [31mnow", failure.Detail)
}

func Test_Send_ReusesTheConnection(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			srv, _ := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "an answer nobody reads")
			})
			sender := NewSender()

			var reused []bool
			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
				GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) },
			})
			_ = sender.Send(ctx, goldenDelivery(srv.URL))
			_ = sender.Send(ctx, goldenDelivery(srv.URL))

			require.Equal(t, []bool{false, true}, reused)
		})
	}
}

func Test_Send_GivesUpOnAReceiverThatDoesNotAnswer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	sender := newSender(http.ProxyFromEnvironment, 50*time.Millisecond)

	started := time.Now()
	err := sender.Send(t.Context(), goldenDelivery(srv.URL+"/hook?token=abc"))

	require.Less(t, time.Since(started), 5*time.Second)
	failure := requireError(t, err)
	require.Equal(t, ReasonTransport, failure.Reason)
	require.False(t, failure.Permanent())
	require.Zero(t, failure.Status)
	require.NotContains(t, failure.Error(), "token")
	require.NotContains(t, failure.Error(), "/hook")
}

func Test_NewSender_GivesARequestTenSeconds(t *testing.T) {
	sender := NewSender()
	for _, client := range []*http.Client{
		sender.verified.direct, sender.verified.proxied, sender.unverified.direct, sender.unverified.proxied,
	} {
		require.Equal(t, 10*time.Second, client.Timeout)
	}
}

func Test_Send_ReportsAnUnreachableReceiverWithoutItsURL(t *testing.T) {
	srv := httptest.NewServer(status(http.StatusOK))
	target := srv.URL + "/hook?token=abc"
	srv.Close()

	err := NewSender().Send(t.Context(), goldenDelivery(strings.Replace(target, "http://", "http://user:password@", 1)))

	failure := requireError(t, err)
	require.Equal(t, ReasonTransport, failure.Reason)
	require.False(t, failure.Permanent())
	require.Equal(t, strings.TrimPrefix(srv.URL, "http://"), failure.Host)
	for _, private := range []string{"token", "/hook", "password", "user"} {
		require.NotContains(t, failure.Error(), private)
	}
	require.Error(t, errors.Unwrap(failure))
}

func Test_Send_VerifiesTheCertificateUnlessToldNotTo(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sender := NewSender()

	delivery := goldenDelivery(srv.URL)
	failure := requireError(t, sender.Send(t.Context(), delivery))
	require.Equal(t, ReasonTransport, failure.Reason)
	require.False(t, failure.Permanent())
	require.Zero(t, calls.Load())

	delivery.SkipTLSVerify = true
	require.NoError(t, sender.Send(t.Context(), delivery))
	require.EqualValues(t, 1, calls.Load())
}

func Test_Send_SpeaksHTTP2ToAReceiverThatOffersIt(t *testing.T) {
	protocols := make(chan string, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocols <- r.Proto
		w.WriteHeader(http.StatusOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	delivery := goldenDelivery(srv.URL)
	delivery.SkipTLSVerify = true
	require.NoError(t, NewSender().Send(t.Context(), delivery))
	require.Equal(t, "HTTP/2.0", <-protocols)
}

func Test_Send_RefusesAURLThatIsNotHTTPWithAHost(t *testing.T) {
	for _, target := range []string{
		"",
		"example.com/hook",
		"ftp://example.com/hook",
		"file:///etc/passwd",
		"http:///hook",
		"https://",
		"http://exa mple.com/?token=abc",
		"mailto:someone@example.com",
	} {
		t.Run(target, func(t *testing.T) {
			failure := requireError(t, NewSender().Send(t.Context(), goldenDelivery(target)))
			require.Equal(t, ReasonInvalidURL, failure.Reason)
			require.True(t, failure.Permanent())
			require.NotContains(t, failure.Error(), "token")
		})
	}
}

func Test_Send_RefusesADestinationBeforeConnecting(t *testing.T) {
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/?token=abc",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"http://[fe80::1]/",
		"http://0.0.0.0/",
		"https://169.254.169.254/",
	} {
		t.Run(target, func(t *testing.T) {
			delivery := goldenDelivery(target)
			delivery.SkipTLSVerify = true

			failure := requireError(t, NewSender().Send(t.Context(), delivery))

			require.Equal(t, ReasonDestination, failure.Reason)
			require.True(t, failure.Permanent())
			require.NotContains(t, failure.Error(), "token")
			require.NotContains(t, failure.Error(), "meta-data")
		})
	}
}

// With a proxy the connection is made to the proxy, which is then the one to decide where
// requests may go.
func Test_Send_GoesThroughTheProxyOfTheEnvironment(t *testing.T) {
	for _, skipVerify := range []bool{false, true} {
		t.Run("skip verification "+strconv.FormatBool(skipVerify), func(t *testing.T) {
			proxy, proxied := newReceiver(t, status(http.StatusOK))
			proxyURL, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			sender := newSender(func(*http.Request) (*url.URL, error) { return proxyURL, nil }, requestTimeout)

			delivery := goldenDelivery("http://169.254.169.254/hook")
			delivery.SkipTLSVerify = skipVerify
			require.NoError(t, sender.Send(t.Context(), delivery))

			got := proxied()
			require.Len(t, got, 1)
			require.Equal(t, "http://169.254.169.254/hook", got[0].path)
			require.Equal(t, goldenSucceededBody, string(got[0].body))
		})
	}
}

func Test_Send_StopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The server notices that the client is gone once the request is read.
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	err := NewSender().Send(ctx, goldenDelivery(srv.URL))

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, ReasonTransport, requireError(t, err).Reason)
}

func Test_Error_SaysWhatHappenedWithoutTheURL(t *testing.T) {
	tests := []struct {
		failure *Error
		want    string
	}{
		{&Error{Reason: ReasonRejected, Status: 404, Host: "example.com", Detail: "no such hook"}, "webhook to example.com: rejected, status 404: no such hook"},
		{&Error{Reason: ReasonUnavailable, Status: 503, Host: "example.com"}, "webhook to example.com: unavailable, status 503"},
		{&Error{Reason: ReasonTransport, Host: "example.com:8443", Detail: "connection refused"}, "webhook to example.com:8443: transport: connection refused"},
		{&Error{Reason: ReasonInvalidURL, Detail: "the scheme is neither http nor https"}, "webhook: invalid url: the scheme is neither http nor https"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, tt.failure.Error())
	}
}

func Test_Reasons_ArePermanentOrTransient(t *testing.T) {
	permanent := []Reason{ReasonInvalidURL, ReasonDestination, ReasonRedirected, ReasonRejected}
	for _, reason := range []Reason{
		ReasonInvalidURL, ReasonDestination, ReasonRedirected, ReasonRejected, ReasonUnavailable, ReasonTransport,
	} {
		require.Equal(t, slices.Contains(permanent, reason), (&Error{Reason: reason}).Permanent(), reason)
	}
}
