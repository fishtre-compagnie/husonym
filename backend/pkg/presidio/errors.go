package presidio

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoAnswer is the error of a call Presidio did not answer: it could not be reached, took too
// long, was given up by the caller, or was answered for by a proxy. It will not answer the next
// call either. What prevented the answer stays in the chain of the error.
var ErrNoAnswer = errors.New("presidio did not answer")

// ErrInvalidResponse is the error of a success that cannot be used: it is not the JSON the
// operation answers with, lacks a field the caller reads, or is too large.
var ErrInvalidResponse = errors.New("presidio answered with an invalid response")

// RefusedError is the error of a call Presidio answered and did not do. The next call may pass:
// what is refused is this request, most often for what it carries.
type RefusedError struct {
	// Operation is what was asked: "analyze", "supported entities" or "anonymize".
	Operation string
	// StatusCode is the HTTP status of the answer.
	StatusCode int
	// Message is why, in the words of Presidio when it gives them, or else the beginning of
	// its answer.
	Message string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("presidio %s refused (status %d): %s", e.Operation, e.StatusCode, e.Message)
}

// maxQuotedBytes is how much of an answer that does not say why is quoted in its place.
const maxQuotedBytes = 512

// refusalMessage reads why Presidio refused: both services answer {"error": "<why>"}.
func refusalMessage(body []byte) string {
	var answer struct {
		Error *string `json:"error"`
	}
	if err := json.Unmarshal(body, &answer); err == nil && answer.Error != nil {
		return *answer.Error
	}
	// A cut may fall inside a character: what is left of it is dropped.
	return strings.ToValidUTF8(string(body[:min(len(body), maxQuotedBytes)]), "")
}

func invalid(operation, format string, args ...any) error {
	return fmt.Errorf("presidio %s: %w: %s", operation, ErrInvalidResponse, fmt.Sprintf(format, args...))
}
