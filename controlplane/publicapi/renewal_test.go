package publicapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

const (
	renewalsPath = "/v1/license-renewals"
	// servedLicense stands for the encoded license an answer carries.
	servedLicense = "SECRET-LICENSE-KEY"
)

type fakeRenewer struct {
	outcome renewal.Outcome
	answer  *telemetry.RenewalAnswer
	err     error
	calls   int
	gotBody []byte
	gotSeal string
	gotFP   string
}

func (f *fakeRenewer) Answer(
	_ context.Context, body []byte, seal, fingerprint string,
) (renewal.Outcome, *telemetry.RenewalAnswer, error) {
	f.calls++
	f.gotBody, f.gotSeal, f.gotFP = body, seal, fingerprint
	return f.outcome, f.answer, f.err
}

// newRenewalRig is a handler whose renewer gives the outcome, with the answer of a license served.
func newRenewalRig(outcome renewal.Outcome, err error) *rig {
	r := newRig(intake.Stored, nil)
	r.renewer.outcome, r.renewer.err = outcome, err
	if outcome == renewal.Served {
		r.renewer.answer = &telemetry.RenewalAnswer{SchemaVersion: telemetry.RenewalSchemaVersion, License: servedLicense}
	}
	return r
}

// ask builds a well-formed request for a renewal around body.
func ask(body io.Reader) *http.Request {
	req := post(body)
	req.URL.Path = renewalsPath
	return req
}

func Test_Renewal_ASuccessor_Answers200WithTheLicense(t *testing.T) {
	r := newRenewalRig(renewal.Served, nil)

	rec := r.do(ask(strings.NewReader(`{"a":1}`)))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	answer, err := telemetry.ParseRenewalAnswer(rec.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, servedLicense, answer.License)
	require.JSONEq(t, `{"schema_version":1,"license":"`+servedLicense+`"}`, rec.Body.String())

	require.Equal(t, 1, r.renewer.calls)
	require.JSONEq(t, `{"a":1}`, string(r.renewer.gotBody))
	require.Equal(t, seal, r.renewer.gotSeal)
	require.Equal(t, fingerprint, r.renewer.gotFP)
	require.Zero(t, r.receiver.calls, "a renewal is not a report")
}

func Test_Renewal_OutcomesBecomeStatusesAndWords(t *testing.T) {
	tests := map[string]struct {
		outcome renewal.Outcome
		err     error
		status  int
		word    string
	}{
		"served":                 {outcome: renewal.Served, status: http.StatusOK, word: "served"},
		"nothing to give":        {outcome: renewal.Nothing, status: http.StatusNoContent, word: "nothing"},
		"refused":                {outcome: renewal.Refused, status: http.StatusBadRequest, word: "refused"},
		"a failure of ours":      {outcome: renewal.Nothing, err: errors.New("boom"), status: http.StatusServiceUnavailable, word: "failed"},
		"an outcome never heard": {outcome: renewal.Outcome(-1), status: http.StatusNoContent, word: "nothing"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRenewalRig(tt.outcome, tt.err)

			rec := r.do(ask(strings.NewReader("{}")))

			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if tt.status != http.StatusOK {
				require.Empty(t, rec.Body.String())
				require.Empty(t, rec.Header().Get("Content-Type"))
			}
			logged := r.logs.String()
			require.Equal(t, 1, strings.Count(logged, "\n"), "one line per request")
			require.Contains(t, logged, "path=/v1/license-renewals")
			require.Contains(t, logged, " status="+strconv.Itoa(tt.status))
			if tt.err == nil {
				require.Contains(t, logged, "outcome="+tt.word+"\n")
			}
			require.Equal(t, []string{tt.word}, r.observer.renewals)
			require.Empty(t, r.observer.outcomes, "a renewal is not counted among the reports")
		})
	}
}

// A served license with no answer to carry is a failure of ours, not an empty 200.
func Test_Renewal_ServedWithoutAnAnswer_Answers503(t *testing.T) {
	r := newRenewalRig(renewal.Served, nil)
	r.renewer.answer = nil

	rec := r.do(ask(strings.NewReader("{}")))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Equal(t, []string{"failed"}, r.observer.renewals)
	require.Contains(t, r.logs.String(), "level=ERROR")
}

func Test_Renewal_RejectsBeforeReadingTheBody(t *testing.T) {
	tests := map[string]struct {
		mutate func(*http.Request)
		status int
	}{
		"get": {
			mutate: func(r *http.Request) { r.Method = http.MethodGet },
			status: http.StatusMethodNotAllowed,
		},
		"put": {
			mutate: func(r *http.Request) { r.Method = http.MethodPut },
			status: http.StatusMethodNotAllowed,
		},
		"no content type": {
			mutate: func(r *http.Request) { r.Header.Del("Content-Type") },
			status: http.StatusBadRequest,
		},
		"text content type": {
			mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
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
		"fingerprint in upper case": {
			mutate: func(r *http.Request) { r.Header.Set("Husonym-Key-Fingerprint", strings.ToUpper(fingerprint)) },
			status: http.StatusBadRequest,
		},
		"a length over the cap": {
			mutate: func(r *http.Request) { r.ContentLength = telemetry.RenewalBodyCap + 1 },
			status: http.StatusRequestEntityTooLarge,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRenewalRig(renewal.Served, nil)
			body := &countingReader{reader: strings.NewReader("{}")}
			req := ask(body)
			tt.mutate(req)

			rec := r.do(req)

			require.Equal(t, tt.status, rec.Code)
			require.Empty(t, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.Zero(t, r.renewer.calls)
			require.Zero(t, body.read)
			require.Equal(t, []string{"refused"}, r.observer.renewals, "what the caller got wrong is one word")
			require.Contains(t, r.logs.String(), "outcome=refused\n")
		})
	}
}

func Test_Renewal_ATrailingSlash_IsAnotherPath(t *testing.T) {
	r := newRenewalRig(renewal.Served, nil)
	req := ask(strings.NewReader("{}"))
	req.URL.Path = renewalsPath + "/"

	rec := r.do(req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, r.renewer.calls)
	require.Empty(t, r.observer.renewals)
}

func Test_Renewal_TheBodyIsCappedAt4KiB(t *testing.T) {
	require.Equal(t, 4<<10, telemetry.RenewalBodyCap)

	atTheCap := newRenewalRig(renewal.Nothing, nil)
	rec := atTheCap.do(ask(bytes.NewReader(bytes.Repeat([]byte("x"), telemetry.RenewalBodyCap))))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, atTheCap.renewer.gotBody, telemetry.RenewalBodyCap)

	over := newRenewalRig(renewal.Served, nil)
	rec = over.do(ask(bytes.NewReader(bytes.Repeat([]byte("x"), telemetry.RenewalBodyCap+1))))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Zero(t, over.renewer.calls)
	require.Equal(t, []string{"refused"}, over.observer.renewals)

	// A body that does not tell its length is not read past the cap.
	endless := newRenewalRig(renewal.Served, nil)
	body := &countingReader{reader: &endlessReader{}}
	rec = endless.do(ask(body))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Zero(t, endless.renewer.calls)
	require.LessOrEqual(t, body.read, telemetry.RenewalBodyCap+1)
}

// The license answered is in the answer and nowhere else; nothing of the caller is logged either.
func Test_Renewal_LogsNothingOfTheCallerNorOfTheLicense(t *testing.T) {
	for _, tt := range []struct {
		outcome renewal.Outcome
		err     error
	}{{renewal.Served, nil}, {renewal.Nothing, nil}, {renewal.Refused, nil}, {renewal.Nothing, errors.New("boom")}} {
		r := newRenewalRig(tt.outcome, tt.err)
		req := ask(strings.NewReader("SECRET-BODY-CONTENT"))
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		req.Header.Set("Authorization", "SECRET-AUTH")

		r.do(req)

		logged := r.logs.String()
		require.Equal(t, 1, strings.Count(logged, "\n"), "one line per request")
		for _, secret := range []string{
			"SECRET-BODY-CONTENT", servedLicense, seal, fingerprint, "192.0.2.1", "203.0.113.9", "SECRET-AUTH",
			"application/json",
		} {
			require.NotContains(t, logged, secret)
		}
	}
}

func Test_Renewal_FailureOfOurs_LogsTheErrorOnly(t *testing.T) {
	r := newRenewalRig(renewal.Nothing, errors.New("database is down"))

	rec := r.do(ask(strings.NewReader("{}")))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Contains(t, r.logs.String(), "level=ERROR")
	require.Contains(t, r.logs.String(), "database is down")
}

// A caller that goes away is not a failure of ours: it has its own word, and no Error line.
func Test_Renewal_ACallerThatWentAway_IsCountedAsInterrupted_NotAsFailed(t *testing.T) {
	r := newRenewalRig(renewal.Nothing, context.Canceled)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	rec := r.do(ask(strings.NewReader("{}")).WithContext(ctx))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, rec.Body.String())
	require.NotContains(t, r.logs.String(), "level=ERROR")
	require.Contains(t, r.logs.String(), "outcome=interrupted\n")
	require.Equal(t, []string{"interrupted"}, r.observer.renewals)
}

// The same error under a request that is still there is a failure of ours.
func Test_Renewal_AnErrorUnderALiveRequest_IsCountedAsFailed(t *testing.T) {
	r := newRenewalRig(renewal.Nothing, context.Canceled)

	rec := r.do(ask(strings.NewReader("{}")))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, r.logs.String(), "level=ERROR")
	require.Equal(t, []string{"failed"}, r.observer.renewals)
}

type panickingRenewer struct{}

func (panickingRenewer) Answer(context.Context, []byte, string, string) (renewal.Outcome, *telemetry.RenewalAnswer, error) {
	panic("SECRET")
}

func Test_Renewal_APanic_Answers503IsCountedAsFailedAndSaysNothingOfIt(t *testing.T) {
	logs := &bytes.Buffer{}
	observer := &recordingObserver{}
	handler := publicapi.NewHandler(&fakeReceiver{}, panickingRenewer{}, observer, slog.New(slog.NewTextHandler(logs, nil)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, ask(strings.NewReader("{}")))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Equal(t, []string{"failed"}, observer.renewals)
	require.Empty(t, observer.outcomes)
	logged := logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line")
	require.Contains(t, logged, "level=ERROR")
	require.NotContains(t, logged, "SECRET")
	require.NotContains(t, logged, "192.0.2.1")
	require.NotContains(t, logged, "goroutine")
}

// The series of the counter are started from this list, and it is the whole of what is counted.
func Test_RenewalOutcomes_AreTheFiveWordsARenewalIsCountedBy(t *testing.T) {
	require.ElementsMatch(t, []string{"served", "nothing", "refused", "interrupted", "failed"}, publicapi.RenewalOutcomes())
}
