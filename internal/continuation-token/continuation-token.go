// Package continuation_token carries, from one page of a table sync to the next, the
// order values of the last row read.
//
// The next page resumes with "order columns > these values", so the values must come
// back exactly as they were read. Plain JSON does not do that: every number becomes a
// float64, which merges neighbors beyond 2^53 (BIGINT keys, snowflake ids), and raw
// bytes come back as base64 text. Each value is therefore stored with its type.
package continuation_token

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
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

const (
	typeNull   = "null"
	typeBool   = "bool"
	typeInt    = "int"
	typeUint   = "uint"
	typeFloat  = "float"
	typeString = "string"
	typeBytes  = "bytes"
	typeTime   = "time"
)

// typedValue is one order value in the token. Numbers travel as text: JSON numbers are
// read back as float64.
type typedValue struct {
	Type  string `json:"t"`
	Value string `json:"v,omitempty"`
}

type contentsJSON struct {
	LastReadOrderValues []json.RawMessage `json:"lastReadOrderValues"`
}

func (c Contents) MarshalJSON() ([]byte, error) {
	out := contentsJSON{LastReadOrderValues: make([]json.RawMessage, len(c.LastReadOrderValues))}
	for i, value := range c.LastReadOrderValues {
		typed, err := encodeValue(value)
		if err != nil {
			return nil, fmt.Errorf("continuation token: order value %d: %w", i, err)
		}
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		out.LastReadOrderValues[i] = encoded
	}
	return json.Marshal(out)
}

func (c *Contents) UnmarshalJSON(data []byte) error {
	var in contentsJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	c.LastReadOrderValues = make([]any, len(in.LastReadOrderValues))
	for i, raw := range in.LastReadOrderValues {
		value, err := decodeValue(raw)
		if err != nil {
			return fmt.Errorf("continuation token: order value %d: %w", i, err)
		}
		c.LastReadOrderValues[i] = value
	}
	return nil
}

func encodeValue(value any) (typedValue, error) {
	switch v := value.(type) {
	case nil:
		return typedValue{Type: typeNull}, nil
	case bool:
		return typedValue{Type: typeBool, Value: strconv.FormatBool(v)}, nil
	case int:
		return typedValue{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int8:
		return typedValue{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int16:
		return typedValue{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int32:
		return typedValue{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int64:
		return typedValue{Type: typeInt, Value: strconv.FormatInt(v, 10)}, nil
	case uint:
		return typedValue{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint8:
		return typedValue{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint16:
		return typedValue{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint32:
		return typedValue{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint64:
		return typedValue{Type: typeUint, Value: strconv.FormatUint(v, 10)}, nil
	case float32:
		return typedValue{Type: typeFloat, Value: strconv.FormatFloat(float64(v), 'g', -1, 32)}, nil
	case float64:
		return typedValue{Type: typeFloat, Value: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case string:
		return typedValue{Type: typeString, Value: v}, nil
	case []byte:
		return typedValue{Type: typeBytes, Value: base64.StdEncoding.EncodeToString(v)}, nil
	case time.Time:
		return typedValue{Type: typeTime, Value: v.Format(time.RFC3339Nano)}, nil
	default:
		return typedValue{}, fmt.Errorf("type %T cannot be carried exactly to the next page", value)
	}
}

func decodeValue(raw json.RawMessage) (any, error) {
	// Tokens written before values were typed hold bare JSON values. A run in flight
	// during an upgrade still resumes with them, as approximately as it did before.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		var legacy any
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return nil, err
		}
		return legacy, nil
	}
	var typed typedValue
	if err := json.Unmarshal(raw, &typed); err != nil {
		return nil, err
	}
	switch typed.Type {
	case typeNull:
		return nil, nil
	case typeBool:
		return strconv.ParseBool(typed.Value)
	case typeInt:
		return strconv.ParseInt(typed.Value, 10, 64)
	case typeUint:
		return strconv.ParseUint(typed.Value, 10, 64)
	case typeFloat:
		return strconv.ParseFloat(typed.Value, 64)
	case typeString:
		return typed.Value, nil
	case typeBytes:
		return base64.StdEncoding.DecodeString(typed.Value)
	case typeTime:
		return time.Parse(time.RFC3339Nano, typed.Value)
	default:
		return nil, fmt.Errorf("unknown value type %q", typed.Type)
	}
}
