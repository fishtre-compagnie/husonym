// Package publicapi is the public HTTP face of the control plane: it receives the sealed usage
// reports of the product instances, keeps the pending ones tidy, and answers an instance that asks
// for the license that succeeds its own.
package publicapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

const (
	reportPath        = "/v1/usage-reports"
	renewalPath       = telemetry.RenewalPath
	healthPath        = "/healthz"
	sealHeader        = "Husonym-Seal"
	fingerprintHeader = "Husonym-Key-Fingerprint"
	// MaxBodyBytes is the largest body a report may have.
	MaxBodyBytes = 128 << 10
)

// Receiver checks and stores one report. It is what the handler needs of the intake.
type Receiver interface {
	Receive(ctx context.Context, document []byte, seal, fingerprint string) (intake.Outcome, error)
}

// Renewer checks one request for a renewal and gives the license to answer with. It is what the
// handler needs of the renewal.
type Renewer interface {
	Answer(ctx context.Context, body []byte, seal, fingerprint string) (renewal.Outcome, *telemetry.RenewalAnswer, error)
}

// Observer counts the report requests and the requests for a renewal by outcome, each apart from
// the other. It is given the fixed words of the log line.
type Observer interface {
	ReportReceived(outcome string)
	RenewalAsked(outcome string)
}

// The fixed words that say how a request ended. They are the ones logged, and the only ones
// counted: nothing a caller sent can become one.
// word is a fixed word of the log line.
type word string

const (
	wordNotFound         word = "not_found"
	wordMethodNotAllowed word = "method_not_allowed"
	wordBadContentType   word = "bad_content_type"
	wordMissingHeader    word = "missing_header"
	wordMalformedHeader  word = "malformed_header"
	wordTooLarge         word = "too_large"
	wordUnreadableBody   word = "unreadable_body"
	wordInterrupted      word = "interrupted"
	wordFailed           word = "failed"
	wordPanicked         word = "panicked"
	wordStored           word = "stored"
	wordPending          word = "pending"
	wordRepeat           word = "repeat"
	wordConflict         word = "conflict"
	wordRefused          word = "refused"
	wordTooManyInstances word = "too_many_instances"
	wordFull             word = "full"
	wordUnknown          word = "unknown"
	wordServed           word = "served"
	wordNothing          word = "nothing"
)

// reportWords are the words a report request can end by. wordNotFound is not one of them: it is
// the word of the other paths, which are not counted.
var reportWords = []word{
	wordMethodNotAllowed, wordBadContentType, wordMissingHeader, wordMalformedHeader, wordTooLarge,
	wordUnreadableBody, wordInterrupted, wordFailed, wordPanicked, wordStored, wordPending, wordRepeat,
	wordConflict, wordRefused, wordTooManyInstances, wordFull, wordUnknown,
}

// Outcomes lists the fixed words a report request is counted by, for an observer that wants to
// know them before the first request.
func Outcomes() []string {
	outcomes := make([]string, 0, len(reportWords))
	for _, name := range reportWords {
		outcomes = append(outcomes, string(name))
	}
	return outcomes
}

// renewalWords are the words a request for a renewal can end by: fewer than a report's, as what
// the caller got wrong is one word whatever it is, and what kept us from answering another. A
// caller that went away has its own, as for a report: it is no failure of ours.
var renewalWords = []word{wordServed, wordNothing, wordRefused, wordInterrupted, wordFailed}

// RenewalOutcomes lists the fixed words a request for a renewal is counted by, as Outcomes does
// for a report.
func RenewalOutcomes() []string {
	outcomes := make([]string, 0, len(renewalWords))
	for _, name := range renewalWords {
		outcomes = append(outcomes, string(name))
	}
	return outcomes
}

// NewHandler returns the handler of the public server: POST /v1/usage-reports,
// POST /v1/license-renewals and GET /healthz. Replies have no body, but the one that carries a
// license. The handler logs at most one line per request, made of the path, the status and a fixed
// word for the outcome, never of what the caller sent nor of a license answered; a failure of ours
// adds the text of our own error. Each report request and each request for a renewal is also
// counted by that word in observer, unless it is nil; one that ends in a panic is answered 503,
// and counted as "panicked" for a report and as "failed" for a renewal. A request whose caller
// went away is counted as "interrupted" for both.
func NewHandler(receiver Receiver, renewer Renewer, observer Observer, logger *slog.Logger) http.Handler {
	return &handler{receiver: receiver, renewer: renewer, observer: observer, logger: logger}
}

type handler struct {
	receiver Receiver
	renewer  Renewer
	observer Observer
	logger   *slog.Logger
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recover() != nil {
			// Fixed words only: what a panic carries, and its stack, may hold something of the
			// request. Left to net/http, the line would name the address of the caller as well.
			h.logger.Error("the handler of the public server panicked", "status", http.StatusServiceUnavailable)
			w.WriteHeader(http.StatusServiceUnavailable)
			// Only a report and a renewal can panic: the other paths call nothing, and count
			// leaves them out.
			if r.URL.Path == renewalPath {
				h.count(renewalPath, wordFailed)
				return
			}
			h.count(r.URL.Path, wordPanicked)
		}
	}()
	switch r.URL.Path {
	case healthPath:
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	case reportPath:
		h.report(w, r)
	case renewalPath:
		h.renewal(w, r)
	default:
		h.reply(w, "-", http.StatusNotFound, wordNotFound)
	}
}

// sealedRequest is what a request that carries a sealed JSON body brought.
type sealedRequest struct {
	body        []byte
	seal        string
	fingerprint string
}

// readSealed checks a request that carries a sealed JSON body and reads that body, limit bytes of
// it at most. What is wrong with the request is told by a status other than zero and by its word;
// what can be refused without the body is refused before it is read.
func readSealed(w http.ResponseWriter, r *http.Request, limit int64) (sealed *sealedRequest, status int, name word) {
	if r.Method != http.MethodPost {
		return nil, http.StatusMethodNotAllowed, wordMethodNotAllowed
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, http.StatusBadRequest, wordBadContentType
	}
	seal, fingerprint := r.Header.Get(sealHeader), r.Header.Get(fingerprintHeader)
	if seal == "" || fingerprint == "" {
		return nil, http.StatusBadRequest, wordMissingHeader
	}
	if !intake.HexShaped(seal) || !intake.HexShaped(fingerprint) {
		return nil, http.StatusBadRequest, wordMalformedHeader
	}
	if r.ContentLength > limit {
		return nil, http.StatusRequestEntityTooLarge, wordTooLarge
	}

	// A body that does not tell its length is cut at the cap.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, http.StatusRequestEntityTooLarge, wordTooLarge
		}
		return nil, http.StatusBadRequest, wordUnreadableBody
	}
	return &sealedRequest{body: body, seal: seal, fingerprint: fingerprint}, 0, ""
}

func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	sealed, status, name := readSealed(w, r, MaxBodyBytes)
	if sealed == nil {
		h.reply(w, reportPath, status, name)
		return
	}

	outcome, err := h.receiver.Receive(r.Context(), sealed.body, sealed.seal, sealed.fingerprint)
	if err != nil {
		if r.Context().Err() != nil {
			// The caller went away, or the server is stopping: not a failure of ours.
			h.reply(w, reportPath, http.StatusServiceUnavailable, wordInterrupted)
			return
		}
		// A failure of ours: fixed words and the error, nothing of the request.
		h.count(reportPath, wordFailed)
		h.logger.Error("unable to receive a usage report", "path", reportPath,
			"status", http.StatusServiceUnavailable, "error", err.Error())
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	status, name = describe(outcome)
	h.reply(w, reportPath, status, name)
}

// renewal answers a request for the license that succeeds the one of an instance. The checks made
// before the body is read are the ones of a report; what they refuse is one word here. A request
// that is given nothing is answered the same whatever the reason: the status, the headers, and no
// body.
func (h *handler) renewal(w http.ResponseWriter, r *http.Request) {
	sealed, status, _ := readSealed(w, r, telemetry.RenewalBodyCap)
	if sealed == nil {
		h.reply(w, renewalPath, status, wordRefused)
		return
	}

	outcome, answer, err := h.renewer.Answer(r.Context(), sealed.body, sealed.seal, sealed.fingerprint)
	if err != nil {
		if r.Context().Err() != nil {
			// The caller went away, or the server is stopping: not a failure of ours.
			h.reply(w, renewalPath, http.StatusServiceUnavailable, wordInterrupted)
			return
		}
		h.failRenewal(w, err)
		return
	}
	switch outcome {
	case renewal.Served:
		h.serveRenewal(w, answer)
	case renewal.Refused:
		h.reply(w, renewalPath, http.StatusBadRequest, wordRefused)
	default:
		h.reply(w, renewalPath, http.StatusNoContent, wordNothing)
	}
}

// serveRenewal answers a request for a renewal with its license. It is the only answer of the
// public server that carries a key: the line of the request says the path, the status and the
// word, and nothing of the answer.
func (h *handler) serveRenewal(w http.ResponseWriter, answer *telemetry.RenewalAnswer) {
	if answer == nil {
		h.failRenewal(w, errors.New("a license was served without an answer"))
		return
	}
	document, err := answer.Marshal()
	if err != nil {
		h.failRenewal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// A caller that went away does not read its answer: there is nobody to tell.
	_, _ = w.Write(document)
	h.logger.Info("request", "path", renewalPath, "status", http.StatusOK, "outcome", string(wordServed))
	h.count(renewalPath, wordServed)
}

// failRenewal answers 503 to a request for a renewal that a failure of ours ended, and logs it:
// fixed words and the error, nothing of the request.
func (h *handler) failRenewal(w http.ResponseWriter, err error) {
	h.count(renewalPath, wordFailed)
	h.logger.Error("unable to answer a request for a license renewal", "path", renewalPath,
		"status", http.StatusServiceUnavailable, "error", err.Error())
	w.WriteHeader(http.StatusServiceUnavailable)
}

// describe gives the status and the fixed word of an outcome. A stored report and a pending one
// answer the same. That is all that is promised: the other answers differ with the license, as a
// wrong seal is only refused under a license that is known.
func describe(outcome intake.Outcome) (status int, name word) {
	switch outcome {
	case intake.Stored:
		return http.StatusNoContent, wordStored
	case intake.Pending:
		return http.StatusNoContent, wordPending
	case intake.Repeat:
		return http.StatusNoContent, wordRepeat
	case intake.Conflict:
		return http.StatusNoContent, wordConflict
	case intake.Refused:
		return http.StatusBadRequest, wordRefused
	case intake.TooManyInstances:
		return http.StatusBadRequest, wordTooManyInstances
	case intake.Full:
		return http.StatusServiceUnavailable, wordFull
	default:
		return http.StatusServiceUnavailable, wordUnknown
	}
}

// reply answers with a status and no body, and logs the line of the request. A path that is not
// one of ours is logged as "-", never as the caller wrote it.
func (h *handler) reply(w http.ResponseWriter, path string, status int, outcome word) {
	w.WriteHeader(status)
	h.logger.Info("request", "path", path, "status", status, "outcome", string(outcome))
	h.count(path, outcome)
}

// count tells the observer how a report request or a request for a renewal ended; the other paths
// are not counted.
func (h *handler) count(path string, outcome word) {
	if h.observer == nil {
		return
	}
	switch path {
	case reportPath:
		h.observer.ReportReceived(string(outcome))
	case renewalPath:
		h.observer.RenewalAsked(string(outcome))
	}
}
