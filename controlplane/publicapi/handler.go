// Package publicapi is the public HTTP face of the control plane: it receives the sealed usage
// reports of the product instances, and keeps the pending ones tidy.
package publicapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
)

const (
	reportPath        = "/v1/usage-reports"
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

// Observer counts the report requests by outcome. It is given the fixed words of the log line.
type Observer interface {
	ReportReceived(outcome string)
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

// NewHandler returns the handler of the public server: POST /v1/usage-reports and GET /healthz.
// Replies have no body. At most one line is logged per request, made of the path, the status
// and a fixed word for the outcome, never of what the caller sent; a failure of ours adds the
// text of our own error. Each report request is also counted by that word in observer, unless it
// is nil; one that ends in a panic is answered 503 and counted as "panicked".
func NewHandler(receiver Receiver, observer Observer, logger *slog.Logger) http.Handler {
	return &handler{receiver: receiver, observer: observer, logger: logger}
}

type handler struct {
	receiver Receiver
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
			// Only a report request can panic: the other paths call nothing. count leaves them out.
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
	default:
		h.reply(w, "-", http.StatusNotFound, wordNotFound)
	}
}

func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.reply(w, reportPath, http.StatusMethodNotAllowed, wordMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.reply(w, reportPath, http.StatusBadRequest, wordBadContentType)
		return
	}
	seal, fingerprint := r.Header.Get(sealHeader), r.Header.Get(fingerprintHeader)
	if seal == "" || fingerprint == "" {
		h.reply(w, reportPath, http.StatusBadRequest, wordMissingHeader)
		return
	}
	// What can be refused without the body is refused before it is read.
	if !intake.HexShaped(seal) || !intake.HexShaped(fingerprint) {
		h.reply(w, reportPath, http.StatusBadRequest, wordMalformedHeader)
		return
	}
	if r.ContentLength > MaxBodyBytes {
		h.reply(w, reportPath, http.StatusRequestEntityTooLarge, wordTooLarge)
		return
	}

	// A body that does not tell its length is cut at the cap.
	document, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.reply(w, reportPath, http.StatusRequestEntityTooLarge, wordTooLarge)
			return
		}
		h.reply(w, reportPath, http.StatusBadRequest, wordUnreadableBody)
		return
	}

	outcome, err := h.receiver.Receive(r.Context(), document, seal, fingerprint)
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
	status, name := describe(outcome)
	h.reply(w, reportPath, status, name)
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

// count tells the observer how a report request ended; the other paths are not counted.
func (h *handler) count(path string, outcome word) {
	if path == reportPath && h.observer != nil {
		h.observer.ReportReceived(string(outcome))
	}
}
