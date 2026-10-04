package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
)

// Reason says why the model could not be asked.
type Reason string

const (
	ReasonRejected    Reason = "rejected"    // the endpoint refuses the request: permanent
	ReasonUnavailable Reason = "unavailable" // the endpoint cannot answer now: transient
	ReasonTransport   Reason = "transport"   // connection, TLS, timeout: transient
)

// maxDetail is the length the message of an endpoint is cut at.
const maxDetail = 300

// Error is the failure of a request to the model.
type Error struct {
	Reason Reason
	Status int // the HTTP status, 0 when no answer came
	// Detail is what the endpoint said of its refusal. It is never the request; when the
	// request carried values it is not the endpoint's message either, which may quote
	// the request, only the type and the code of its error.
	Detail string
}

func (e *Error) Error() string {
	var b strings.Builder
	switch e.Reason {
	case ReasonRejected:
		fmt.Fprintf(&b, "the model endpoint refused the request (HTTP %d)", e.Status)
	case ReasonUnavailable:
		fmt.Fprintf(&b, "the model endpoint could not answer (HTTP %d)", e.Status)
	default:
		b.WriteString("the model endpoint could not be reached")
	}
	if e.Detail != "" {
		b.WriteString(": " + e.Detail)
	}
	if e.Status == http.StatusBadRequest || e.Status == http.StatusUnprocessableEntity {
		b.WriteString(". The endpoint must accept a chat completion with a JSON schema response format and a temperature of 0")
	}
	return b.String()
}

// Permanent tells whether a new attempt would fail the same way.
func (e *Error) Permanent() bool {
	return e.Reason == ReasonRejected
}

// failure turns the error of a request into an Error. A canceled context is returned as
// it is: it is not a failure of the endpoint.
func failure(ctx context.Context, err error, carriesValues bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		// What a transport error says holds addresses, not the request.
		return &Error{Reason: ReasonTransport, Detail: cut(lastCause(err), maxDetail)}
	}
	failed := &Error{Reason: ReasonRejected, Status: apiErr.StatusCode}
	switch {
	case apiErr.StatusCode == http.StatusRequestTimeout,
		apiErr.StatusCode == http.StatusConflict,
		apiErr.StatusCode == http.StatusTooManyRequests,
		apiErr.StatusCode >= http.StatusInternalServerError:
		failed.Reason = ReasonUnavailable
	}
	if carriesValues {
		failed.Detail = strings.TrimSpace(identifier(apiErr.Type) + " " + identifier(apiErr.Code))
	} else {
		failed.Detail = cut(apiErr.Message, maxDetail)
	}
	return failed
}

// identifier keeps a type or a code of an error when it looks like one: a short word.
// Anything else could be a text that quotes the request.
func identifier(value string) string {
	if len(value) > 64 {
		return ""
	}
	for _, r := range value {
		word := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !word {
			return ""
		}
	}
	return value
}

// lastCause is the message of the innermost error: the outer ones repeat the URL.
func lastCause(err error) string {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err.Error()
		}
		err = inner
	}
}

func cut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
