package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// RenewalPath is the path a license renewal is asked at.
	RenewalPath = "/v1/license-renewals"
	// RenewalBodyCap is the largest request body, in bytes, a receiver reads.
	RenewalBodyCap = 4 << 10
	// RenewalAnswerCap is the largest answer body, in bytes, a caller reads.
	RenewalAnswerCap = 64 << 10
	// RenewalFreshness is how far, either side, the instant of a request may be from the clock of
	// the receiver.
	RenewalFreshness = 5 * time.Minute
	// RenewalSchemaVersion is the version of the request and of the answer.
	RenewalSchemaVersion = 1

	// renewalIdentifierMax is the longest license id or instance id, in characters.
	renewalIdentifierMax = 128
	// renewalLicenseMax is the longest license an answer carries, in bytes.
	renewalLicenseMax = 16 << 10
)

// RenewalRequest asks for the license that succeeds the one of the instance. It is sealed with
// Seal, like a report, with the license key in force.
type RenewalRequest struct {
	SchemaVersion int    `json:"schema_version"`
	LicenseID     string `json:"license_id"`
	InstanceID    string `json:"instance_id"`
	RequestedAt   string `json:"requested_at"`
}

// NewRenewalRequest is the request of an instance for its license, made at the given instant,
// written in UTC to the second. The license id passes through LicenseId.
func NewRenewalRequest(licenseID, instanceID string, at time.Time) *RenewalRequest {
	return &RenewalRequest{
		SchemaVersion: RenewalSchemaVersion,
		LicenseID:     LicenseId(licenseID),
		InstanceID:    instanceID,
		RequestedAt:   at.UTC().Truncate(time.Second).Format(time.RFC3339),
	}
}

// Marshal is the request as JSON, with no trailing newline: one request gives one sequence of
// bytes.
func (r *RenewalRequest) Marshal() ([]byte, error) {
	document, err := json.Marshal(r)
	if err != nil {
		return nil, errors.New("marshaling the renewal request failed")
	}
	return document, nil
}

// At is the instant of the request, or the zero time when it is not written the way
// NewRenewalRequest writes it.
func (r *RenewalRequest) At() time.Time {
	at, err := parseRenewalInstant(r.RequestedAt)
	if err != nil {
		return time.Time{}
	}
	return at
}

// ParseRenewalRequest reads a request. It is closed: an unknown field, data after the value, a
// version other than RenewalSchemaVersion, a missing or unbounded identifier and an instant that
// is not written the way NewRenewalRequest writes it are refused. Errors are in fixed words and
// never quote the body.
func ParseRenewalRequest(body []byte) (*RenewalRequest, error) {
	var request RenewalRequest
	if err := decodeClosed(body, &request); err != nil {
		return nil, fmt.Errorf("the renewal request %w", err)
	}
	if request.SchemaVersion != RenewalSchemaVersion {
		return nil, errors.New("the renewal request has an unsupported schema version")
	}
	if err := checkRenewalIdentifier(request.LicenseID); err != nil {
		return nil, fmt.Errorf("the renewal request has a license id that %w", err)
	}
	if err := checkRenewalIdentifier(request.InstanceID); err != nil {
		return nil, fmt.Errorf("the renewal request has an instance id that %w", err)
	}
	if _, err := parseRenewalInstant(request.RequestedAt); err != nil {
		return nil, errors.New("the renewal request has an instant that is not RFC 3339 in UTC to the second")
	}
	return &request, nil
}

// RenewalAnswer is the answer to a request when a successor exists: the encoded license.
type RenewalAnswer struct {
	SchemaVersion int    `json:"schema_version"`
	License       string `json:"license"`
}

// Marshal is the answer as JSON, with no trailing newline.
func (a *RenewalAnswer) Marshal() ([]byte, error) {
	document, err := json.Marshal(a)
	if err != nil {
		return nil, errors.New("marshaling the renewal answer failed")
	}
	return document, nil
}

// ParseRenewalAnswer reads an answer. It is closed like ParseRenewalRequest; the license is
// returned with its surrounding space trimmed, and is neither empty nor larger than 16 KiB.
func ParseRenewalAnswer(body []byte) (*RenewalAnswer, error) {
	var answer RenewalAnswer
	if err := decodeClosed(body, &answer); err != nil {
		return nil, fmt.Errorf("the renewal answer %w", err)
	}
	if answer.SchemaVersion != RenewalSchemaVersion {
		return nil, errors.New("the renewal answer has an unsupported schema version")
	}
	answer.License = strings.TrimSpace(answer.License)
	if answer.License == "" {
		return nil, errors.New("the renewal answer carries no license")
	}
	if len(answer.License) > renewalLicenseMax {
		return nil, errors.New("the renewal answer carries a license that is too large")
	}
	return &answer, nil
}

// decodeClosed reads exactly one JSON value into target, refusing unknown fields and anything
// after the value. The error finishes a sentence and says nothing of the body.
func decodeClosed(body []byte, target any) error {
	if !utf8.Valid(body) {
		return errors.New("is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("is not a JSON object of the expected shape")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("has data after the JSON object")
	}
	return nil
}

// checkRenewalIdentifier refuses an identifier that is empty, longer than 128 characters or
// holds a control character. The error finishes a sentence.
func checkRenewalIdentifier(value string) error {
	if value == "" {
		return errors.New("is missing")
	}
	if utf8.RuneCountInString(value) > renewalIdentifierMax {
		return errors.New("is too long")
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New("holds a control character")
	}
	return nil
}

// parseRenewalInstant reads the one spelling of an instant: RFC 3339, UTC, to the second.
func parseRenewalInstant(raw string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	at = at.UTC()
	if at.Format(time.RFC3339) != raw {
		return time.Time{}, errors.New("not the canonical spelling")
	}
	return at, nil
}
