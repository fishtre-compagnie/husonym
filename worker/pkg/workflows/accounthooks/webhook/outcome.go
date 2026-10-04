package webhook

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// Reason says why a delivery did not succeed.
type Reason string

const (
	ReasonEvent       Reason = "invalid event"       // there is no event, or it cannot be encoded
	ReasonInvalidURL  Reason = "invalid url"         // the URL is not http or https with a host
	ReasonDestination Reason = "destination refused" // the address is one webhooks are not sent to
	ReasonRedirected  Reason = "redirected"          // a 3xx answer: redirects are not followed
	ReasonRejected    Reason = "rejected"            // a 4xx answer other than 408, 425, 429, or a status that is none
	ReasonUnavailable Reason = "unavailable"         // a 5xx, 408, 425 or 429 answer
	ReasonTransport   Reason = "transport"           // no answer: connection, TLS, timeout
)

// Error is a delivery that did not succeed. It never holds the URL, which may carry a
// token: only its host. Its texts end up in logs and in workflow histories, and part of
// them is written by the receiver or by the owner of the hook: each is at most 512 bytes
// of printable text.
type Error struct {
	Reason Reason
	Status int    // 0 when no response was received
	Host   string // the host of the URL, never the URL
	Detail string // an excerpt of the response body, or the cause without the URL

	cause error // context.Canceled, or nothing
}

func (e *Error) Error() string {
	var text strings.Builder
	text.WriteString("webhook")
	if e.Host != "" {
		text.WriteString(" to " + e.Host)
	}
	text.WriteString(": " + string(e.Reason))
	if e.Status != 0 {
		text.WriteString(", status " + strconv.Itoa(e.Status))
	}
	if e.Detail != "" {
		text.WriteString(": " + e.Detail)
	}
	return text.String()
}

// Unwrap returns context.Canceled when the context of the request was canceled, and nil
// otherwise.
func (e *Error) Unwrap() error {
	return e.cause
}

// Permanent tells whether a new attempt cannot succeed without a change by the owner of
// the hook or by the operator. What can heal within the minutes of a retry is not
// permanent: a receiver that restarts, a network that drops.
func (e *Error) Permanent() bool {
	switch e.Reason {
	case ReasonEvent, ReasonInvalidURL, ReasonDestination, ReasonRedirected, ReasonRejected:
		return true
	default:
		return false
	}
}

// reasonOfStatus classifies an answer. ok is true for a 2xx, which is a success.
func reasonOfStatus(status int) (reason Reason, ok bool) {
	switch {
	case status >= 200 && status <= 299:
		return "", true
	case status >= 300 && status <= 399:
		return ReasonRedirected, false
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly, status == http.StatusTooManyRequests:
		return ReasonUnavailable, false
	case status >= 500 && status <= 599:
		return ReasonUnavailable, false
	default:
		return ReasonRejected, false
	}
}

// maxDetailBytes is how much of a response body is kept in an Error: enough to read what
// the receiver said, little enough for a log line and a workflow history.
const maxDetailBytes = 512

// excerpt returns the beginning of a response body as text that is safe to print.
func excerpt(body []byte) string {
	if len(body) > maxDetailBytes {
		body = body[:maxDetailBytes]
	}
	printable := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, strings.ToValidUTF8(string(body), ""))
	return strings.TrimSpace(printable)
}
