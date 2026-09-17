// Package continuation_token carries, from one page of a table sync to the next, the
// order values of the last row read.
//
// The next page resumes with "order columns > these values", so the values must come
// back exactly as they were read. Plain JSON does not do that: every number becomes a
// float64, which merges neighbors beyond 2^53 (BIGINT keys, snowflake ids), and raw
// bytes come back as base64 text. Each value is therefore stored with its type.
package continuation_token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/typedvalue"
)

func FromTokenString(tokenStr string) (*ContinuationToken, error) {
	if tokenStr == "" {
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("unable to decode continuation token: %w", err)
	}

	var token ContinuationToken
	if err := json.Unmarshal(decoded, &token); err != nil {
		return nil, fmt.Errorf("unable to unmarshal continuation token: %w", err)
	}

	return &token, nil
}

type ContinuationToken struct {
	Contents *Contents `json:"contents"`
}

// Contents holds the order values of the last row read: nil, bool, int64, uint64,
// float64, string, []byte or time.Time once decoded.
type Contents struct {
	LastReadOrderValues []any
}

func NewContents(lastReadOrderValues []any) *Contents {
	return &Contents{
		LastReadOrderValues: lastReadOrderValues,
	}
}

func NewFromContents(contents *Contents) *ContinuationToken {
	return &ContinuationToken{
		Contents: contents,
	}
}

// Encode returns the token as text. It fails on an order value whose type cannot be
// carried exactly: resuming after an approximate value reads rows twice or skips them,
// so the table sync must fail instead.
func (c *ContinuationToken) Encode() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("unable to encode continuation token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encoded), nil
}

type contentsJSON struct {
	LastReadOrderValues []json.RawMessage `json:"lastReadOrderValues"`
}

func (c Contents) MarshalJSON() ([]byte, error) {
	out := contentsJSON{LastReadOrderValues: make([]json.RawMessage, len(c.LastReadOrderValues))}
	for i, value := range c.LastReadOrderValues {
		encoded, err := typedvalue.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("continuation token: order value %d: %w", i, err)
		}
		out.LastReadOrderValues[i] = encoded
	}
	return json.Marshal(out)
}

// UnmarshalJSON also reads the tokens written before values were typed: a run in flight
// during an upgrade still resumes with them, as approximately as it did before.
func (c *Contents) UnmarshalJSON(data []byte) error {
	var in contentsJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	c.LastReadOrderValues = make([]any, len(in.LastReadOrderValues))
	for i, raw := range in.LastReadOrderValues {
		value, err := typedvalue.Unmarshal(raw)
		if err != nil {
			return fmt.Errorf("continuation token: order value %d: %w", i, err)
		}
		c.LastReadOrderValues[i] = value
	}
	return nil
}
