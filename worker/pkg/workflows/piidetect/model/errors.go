package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/openai/openai-go/v3"
)

// Reason says why the model could not be asked.
type Reason string

const (
	ReasonRejected    Reason = "rejected"    // the endpoint refuses the request: permanent
	ReasonUnavailable Reason = "unavailable" // the endpoint cannot answer now: transient
	ReasonTransport   Reason = "transport"   // connection, TLS, timeout, an answer that is too large: transient
)

// maxDetail is the length the cause of a transport failure is cut at.
const maxDetail = 300

// Error is the failure of a request to the model.
type Error struct {
	Reason Reason
	Status int // the HTTP status, 0 when no answer came
	// Detail is what is kept of the endpoint's refusal: the type and the code of its
	// error, when they are identifiers. It is never the request, and never the message of
	// the endpoint, which may quote the request. For a failure without an answer it is the
	// cause of the transport.
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
//
// status is the HTTP status of the answer, 0 when none came. It decides alone whether the
// failure may heal, whatever the body of the answer is: the error object of the API, a
// text, a page of a proxy. sent are the values the request carried.
func failure(ctx context.Context, err error, status int, sent []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var apiErr *openai.Error
	isAPIError := errors.As(err, &apiErr)
	if status == 0 && isAPIError {
		status = apiErr.StatusCode
	}
	if status < http.StatusBadRequest {
		// What a transport error says holds addresses, not the request.
		return &Error{Reason: ReasonTransport, Detail: profile.FirstRunes(lastCause(err), maxDetail)}
	}

	failed := &Error{Reason: ReasonRejected, Status: status}
	switch {
	case status == http.StatusRequestTimeout,
		status == http.StatusConflict,
		status == http.StatusTooManyRequests,
		status >= http.StatusInternalServerError:
		failed.Reason = ReasonUnavailable
	}
	if isAPIError {
		failed.Detail = strings.TrimSpace(identifier(apiErr.Type, sent) + " " + identifier(apiErr.Code, sent))
	}
	return failed
}

// An identifier of an error: a short word in lowercase, as the types and the codes of
// the API are written.
var identifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// identifier keeps a type or a code of an error when it is an identifier and nothing
// that was sent: anything else could be a text that quotes the request, or a value. An
// identifier that is a value, or that holds one of some length, is not kept.
func identifier(word string, sent []string) string {
	if !identifierRe.MatchString(word) {
		return ""
	}
	for _, value := range sent {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == word || (len(value) >= minQuotedValue && strings.Contains(word, value)) {
			return ""
		}
	}
	return word
}

// minQuotedValue is the length from which a value found inside an identifier is taken
// for a quotation: a shorter one is in many words by chance.
const minQuotedValue = 4

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
