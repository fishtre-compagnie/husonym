package publicapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/stretchr/testify/require"
)

const (
	seal        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeReceiver struct {
	outcome intake.Outcome
	err     error
	calls   int
	gotBody []byte
	gotSeal string
	gotFP   string
}

func (f *fakeReceiver) Receive(_ context.Context, document []byte, seal, fingerprint string) (intake.Outcome, error) {
	f.calls++
	f.gotBody, f.gotSeal, f.gotFP = document, seal, fingerprint
	return f.outcome, f.err
}

type rig struct {
	receiver *fakeReceiver
	logs     *bytes.Buffer
	handler  http.Handler
	observer *recordingObserver
}

type recordingObserver struct{ outcomes []string }

func (o *recordingObserver) ReportReceived(outcome string) { o.outcomes = append(o.outcomes, outcome) }

func newRig(outcome intake.Outcome, err error) *rig {
	r := &rig{receiver: &fakeReceiver{outcome: outcome, err: err}, logs: &bytes.Buffer{}, observer: &recordingObserver{}}
	r.handler = publicapi.NewHandler(r.receiver, r.observer, slog.New(slog.NewTextHandler(r.logs, nil)))
	return r
}

func (r *rig) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

// post builds a well-formed report request around body.
func post(body io.Reader) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/usage-reports", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Husonym-Seal", seal)
	req.Header.Set("Husonym-Key-Fingerprint", fingerprint)
	return req
}

func Test_Handler_OutcomesBecomeStatuses(t *testing.T) {
	tests := map[string]struct {
		outcome intake.Outcome
		err     error
		status  int
	}{
		"stored":   {outcome: intake.Stored, status: http.StatusNoContent},
		"pending":  {outcome: intake.Pending, status: http.StatusNoContent},
		"repeat":   {outcome: intake.Repeat, status: http.StatusNoContent},
		"conflict": {outcome: intake.Conflict, status: http.StatusNoContent},
		"refused":  {outcome: intake.Refused, status: http.StatusBadRequest},
		"full":     {outcome: intake.Full, status: http.StatusServiceUnavailable},
		"too_many_instances": {
			outcome: intake.TooManyInstances, status: http.StatusBadRequest,
		},
		"failure": {outcome: intake.Stored, err: errors.New("boom"), status: http.StatusServiceUnavailable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRig(tt.outcome, tt.err)

			rec := r.do(post(strings.NewReader(`{"a":1}`)))

			require.Equal(t, tt.status, rec.Code)
			if tt.err == nil {
				// The name of the case is the word of its outcome in the log.
				require.Contains(t, r.logs.String(), "outcome="+name+"\n")
			}
			require.Empty(t, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.Equal(t, 1, r.receiver.calls)
			require.JSONEq(t, `{"a":1}`, string(r.receiver.gotBody))
			require.Equal(t, seal, r.receiver.gotSeal)
			require.Equal(t, fingerprint, r.receiver.gotFP)
		})
	}
}

func Test_Handler_StoredAndPending_AnswerTheSame(t *testing.T) {
	stored := newRig(intake.Stored, nil).do(post(strings.NewReader("{}")))
	pending := newRig(intake.Pending, nil).do(post(strings.NewReader("{}")))

	require.Equal(t, stored.Code, pending.Code)
	require.Equal(t, stored.Header(), pending.Header())
	require.Equal(t, stored.Body.String(), pending.Body.String())
}

func Test_Handler_RejectsBeforeReadingTheBody(t *testing.T) {
	tests := map[string]struct {
		mutate func(*http.Request)
		status int
	}{
		"wrong method": {
			mutate: func(r *http.Request) { r.Method = http.MethodPut },
			status: http.StatusMethodNotAllowed,
		},
		"get": {
			mutate: func(r *http.Request) { r.Method = http.MethodGet },
			status: http.StatusMethodNotAllowed,
		},
		"other path": {
			mutate: func(r *http.Request) { r.URL.Path = "/v1/other" },
			status: http.StatusNotFound,
		},
		"trailing slash": {
			mutate: func(r *http.Request) { r.URL.Path = "/v1/usage-reports/" },
			status: http.StatusNotFound,
		},
		"no content type": {
			mutate: func(r *http.Request) { r.Header.Del("Content-Type") },
			status: http.StatusBadRequest,
		},
		"text content type": {
			mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
			status: http.StatusBadRequest,
		},
		"look-alike content type": {
			mutate: func(r *http.Request) { r.Header.Set("Content-Type", "application/jsonx") },
			status: http.StatusBadRequest,
		},
		"no seal": {
			mutate: func(r *http.Request) { r.Header.Del("Husonym-Seal") },
			status: http.StatusBadRequest,
		},
		"no fingerprint": {
			mutate: func(r *http.Request) { r.Header.Del("Husonym-Key-Fingerprint") },
			status: http.StatusBadRequest,
		},
		"seal too short": {
			mutate: func(r *http.Request) { r.Header.Set("Husonym-Seal", seal[:63]) },
			status: http.StatusBadRequest,
		},
		"seal in upper case": {
			mutate: func(r *http.Request) { r.Header.Set("Husonym-Seal", strings.ToUpper(seal)) },
			status: http.StatusBadRequest,
		},
		"fingerprint too long": {
			mutate: func(r *http.Request) { r.Header.Set("Husonym-Key-Fingerprint", fingerprint+"b") },
			status: http.StatusBadRequest,
		},
		"fingerprint not hex": {
			mutate: func(r *http.Request) { r.Header.Set("Husonym-Key-Fingerprint", strings.Repeat("g", 64)) },
			status: http.StatusBadRequest,
		},
		"a length over the cap": {
			mutate: func(r *http.Request) { r.ContentLength = publicapi.MaxBodyBytes + 1 },
			status: http.StatusRequestEntityTooLarge,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRig(intake.Stored, nil)
			body := &countingReader{reader: strings.NewReader("{}")}
			req := post(body)
			tt.mutate(req)

			rec := r.do(req)

			require.Equal(t, tt.status, rec.Code)
			require.Empty(t, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.Zero(t, r.receiver.calls)
			require.Zero(t, body.read)
		})
	}
}

func Test_Handler_ContentTypeWithParameters_Passes(t *testing.T) {
	r := newRig(intake.Stored, nil)
	req := post(strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	require.Equal(t, http.StatusNoContent, r.do(req).Code)
}

func Test_Handler_BodyOfExactlyTheCap_Passes(t *testing.T) {
	r := newRig(intake.Stored, nil)

	rec := r.do(post(bytes.NewReader(bytes.Repeat([]byte("x"), publicapi.MaxBodyBytes))))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, r.receiver.gotBody, publicapi.MaxBodyBytes)
}

func Test_Handler_BodyOneByteOverTheCap_Answers413WithoutReadingOn(t *testing.T) {
	r := newRig(intake.Stored, nil)
	body := &countingReader{reader: &endlessReader{}}

	rec := r.do(post(body))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Zero(t, r.receiver.calls)
	// The limit reader takes at most one byte past the cap.
	require.LessOrEqual(t, body.read, publicapi.MaxBodyBytes+1)
}

func Test_Handler_BodyOfCapPlusOneByte_Answers413(t *testing.T) {
	r := newRig(intake.Stored, nil)

	rec := r.do(post(bytes.NewReader(bytes.Repeat([]byte("x"), publicapi.MaxBodyBytes+1))))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Zero(t, r.receiver.calls)
}

func Test_Handler_ALengthOfExactlyTheCap_Passes(t *testing.T) {
	r := newRig(intake.Stored, nil)
	req := post(&countingReader{reader: bytes.NewReader(bytes.Repeat([]byte("x"), publicapi.MaxBodyBytes))})
	req.ContentLength = publicapi.MaxBodyBytes

	require.Equal(t, http.StatusNoContent, r.do(req).Code)
	require.Len(t, r.receiver.gotBody, publicapi.MaxBodyBytes)
}

func Test_MaxBodyBytes_Is128KiB(t *testing.T) {
	require.Equal(t, 128<<10, publicapi.MaxBodyBytes)
}

type panickingReceiver struct{}

func (panickingReceiver) Receive(context.Context, []byte, string, string) (intake.Outcome, error) {
	panic("SECRET")
}

func Test_Handler_APanic_Answers503IsCountedAndSaysNothingOfIt(t *testing.T) {
	logs := &bytes.Buffer{}
	observer := &recordingObserver{}
	handler := publicapi.NewHandler(panickingReceiver{}, observer, slog.New(slog.NewTextHandler(logs, nil)))
	req := post(strings.NewReader("{}"))
	require.Equal(t, "192.0.2.1:1234", req.RemoteAddr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Equal(t, []string{"panicked"}, observer.outcomes)
	logged := logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line")
	require.Contains(t, logged, "level=ERROR")
	require.NotContains(t, logged, "SECRET")
	require.NotContains(t, logged, "192.0.2.1")
	require.NotContains(t, logged, "goroutine")
}

func Test_Handler_CancelledRequest_IsNotLoggedAsAnError(t *testing.T) {
	r := newRig(intake.Stored, context.Canceled)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	rec := r.do(post(strings.NewReader("{}")).WithContext(ctx))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, r.logs.String(), "level=ERROR")
}

func Test_Handler_Health(t *testing.T) {
	r := newRig(intake.Stored, nil)

	rec := r.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Body.String())

	rec = r.do(httptest.NewRequest(http.MethodPost, "/healthz", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Zero(t, r.receiver.calls)
}

func Test_Handler_LogsNothingOfTheCaller(t *testing.T) {
	for _, err := range []error{nil, errors.New("boom")} {
		r := newRig(intake.Refused, err)
		req := post(strings.NewReader("SECRET-BODY-CONTENT"))
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		req.Header.Set("Authorization", "SECRET-AUTH")

		r.do(req)

		logged := r.logs.String()
		require.NotEmpty(t, logged)
		require.Equal(t, 1, strings.Count(logged, "\n"), "one line per request")
		for _, secret := range []string{
			"SECRET-BODY-CONTENT", seal, fingerprint, "192.0.2.1", "203.0.113.9", "SECRET-AUTH", "application/json",
		} {
			require.NotContains(t, logged, secret)
		}
	}
}

func Test_Handler_LogLine_IsPathStatusAndOutcome(t *testing.T) {
	r := newRig(intake.Pending, nil)

	r.do(post(strings.NewReader("{}")))

	logged := r.logs.String()
	require.Contains(t, logged, "path=/v1/usage-reports")
	require.Contains(t, logged, "status=204")
	require.Contains(t, logged, "outcome=pending")
}

func Test_Handler_UnknownPath_IsNotLogged(t *testing.T) {
	r := newRig(intake.Stored, nil)

	r.do(httptest.NewRequest(http.MethodGet, "/secret-path-xyz", nil))

	require.NotContains(t, r.logs.String(), "secret-path-xyz")
}

func Test_Handler_FailureOfOurs_LogsTheErrorOnly(t *testing.T) {
	r := newRig(intake.Stored, errors.New("database is down"))

	r.do(post(strings.NewReader("{}")))

	require.Contains(t, r.logs.String(), "database is down")
}

// countingReader counts what is read from it.
type countingReader struct {
	reader io.Reader
	read   int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.read += n
	return n, err
}

// endlessReader never ends.
type endlessReader struct{}

func (*endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func Test_Handler_CountsEachReportRequestByTheWordItLogs(t *testing.T) {
	cases := map[string]struct {
		outcome intake.Outcome
		err     error
		req     func() *http.Request
	}{
		"stored":             {outcome: intake.Stored, req: func() *http.Request { return post(strings.NewReader("{}")) }},
		"pending":            {outcome: intake.Pending, req: func() *http.Request { return post(strings.NewReader("{}")) }},
		"refused":            {outcome: intake.Refused, req: func() *http.Request { return post(strings.NewReader("{}")) }},
		"full":               {outcome: intake.Full, req: func() *http.Request { return post(strings.NewReader("{}")) }},
		"failed":             {err: errors.New("down"), req: func() *http.Request { return post(strings.NewReader("{}")) }},
		"method_not_allowed": {req: func() *http.Request { return httptest.NewRequest(http.MethodGet, "/v1/usage-reports", nil) }},
		"missing_header": {req: func() *http.Request {
			req := post(strings.NewReader("{}"))
			req.Header.Del("Husonym-Seal")
			return req
		}},
	}
	for want, tc := range cases {
		t.Run(want, func(t *testing.T) {
			r := newRig(tc.outcome, tc.err)
			r.do(tc.req())
			require.Equal(t, []string{want}, r.observer.outcomes)
		})
	}
}

// The series of the counter are started from this list: a word counted that is not in it would
// have no series until it is first seen.
func Test_Outcomes_NamesEveryWordAReportRequestIsCountedBy(t *testing.T) {
	outcomes := publicapi.Outcomes()

	require.ElementsMatch(t, []string{
		"method_not_allowed", "bad_content_type", "missing_header", "malformed_header", "too_large", "unreadable_body",
		"interrupted", "failed", "panicked", "stored", "pending", "repeat", "conflict", "refused", "too_many_instances",
		"full", "unknown",
	}, outcomes)
	require.NotContains(t, outcomes, "not_found", "the word of the other paths, which are not counted")

	for name, outcome := range map[string]intake.Outcome{
		"stored": intake.Stored, "pending": intake.Pending, "repeat": intake.Repeat, "conflict": intake.Conflict,
		"refused": intake.Refused, "too_many_instances": intake.TooManyInstances, "full": intake.Full,
		"an outcome this handler does not know": intake.Outcome(-1),
	} {
		r := newRig(outcome, nil)
		r.do(post(strings.NewReader("{}")))
		require.Len(t, r.observer.outcomes, 1, name)
		require.Contains(t, outcomes, r.observer.outcomes[0], name)
	}
}

func Test_Handler_DoesNotCountTheOtherPaths(t *testing.T) {
	r := newRig(intake.Stored, nil)
	r.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	r.do(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	r.do(httptest.NewRequest(http.MethodGet, "/whatever-a-caller-wrote", nil))
	require.Empty(t, r.observer.outcomes)
}
