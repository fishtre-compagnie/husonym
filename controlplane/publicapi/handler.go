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

// NewHandler returns the handler of the public server: POST /v1/usage-reports and GET /healthz.
// Replies have no body. At most one line is logged per request, made of the path, the status
// and a fixed word for the outcome: never what the caller sent.
func NewHandler(receiver Receiver, logger *slog.Logger) http.Handler {
	return &handler{receiver: receiver, logger: logger}
}

type handler struct {
	receiver Receiver
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
		h.reply(w, "-", http.StatusNotFound, "not_found")
	}
}

func (h *handler) report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.reply(w, reportPath, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.reply(w, reportPath, http.StatusBadRequest, "bad_content_type")
		return
	}
	seal, fingerprint := r.Header.Get(sealHeader), r.Header.Get(fingerprintHeader)
	if seal == "" || fingerprint == "" {
		h.reply(w, reportPath, http.StatusBadRequest, "missing_header")
		return
	}
	// What can be refused without the body is refused before it is read.
	if !intake.HexShaped(seal) || !intake.HexShaped(fingerprint) {
		h.reply(w, reportPath, http.StatusBadRequest, "malformed_header")
		return
	}
	if r.ContentLength > MaxBodyBytes {
		h.reply(w, reportPath, http.StatusRequestEntityTooLarge, "too_large")
		return
	}

	// A body that does not tell its length is cut at the cap.
	document, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.reply(w, reportPath, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		h.reply(w, reportPath, http.StatusBadRequest, "unreadable_body")
		return
	}

	outcome, err := h.receiver.Receive(r.Context(), document, seal, fingerprint)
	if err != nil {
		if r.Context().Err() != nil {
			// The caller went away, or the server is stopping: not a failure of ours.
			h.logger.Info("request", "path", reportPath, "status", http.StatusServiceUnavailable, "outcome", "interrupted")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// A failure of ours: fixed words and the error, nothing of the request.
		h.logger.Error("unable to receive a usage report", "path", reportPath,
			"status", http.StatusServiceUnavailable, "error", err.Error())
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	status, name := describe(outcome)
	h.reply(w, reportPath, status, name)
}

// describe gives the status and the fixed word of an outcome. Stored and pending answer the
// same, so that a reply never tells whether the license is known.
func describe(outcome intake.Outcome) (status int, name string) {
	switch outcome {
	case intake.Stored:
		return http.StatusNoContent, "stored"
	case intake.Pending:
		return http.StatusNoContent, "pending"
	case intake.Repeat:
		return http.StatusNoContent, "repeat"
	case intake.Conflict:
		return http.StatusNoContent, "conflict"
	case intake.Refused:
		return http.StatusBadRequest, "refused"
	case intake.Full:
		return http.StatusServiceUnavailable, "full"
	default:
		return http.StatusServiceUnavailable, "unknown"
	}
}

// reply answers with a status and no body, and logs the line of the request. A path that is not
// one of ours is logged as "-", never as the caller wrote it.
func (h *handler) reply(w http.ResponseWriter, path string, status int, outcome string) {
	w.WriteHeader(status)
	h.logger.Info("request", "path", path, "status", status, "outcome", outcome)
}
