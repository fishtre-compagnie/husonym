// Package typedvalue carries a database value through text and back, exactly.
//
// Plain JSON does not: every number comes back as a float64, which merges integers
// beyond 2^53 (BIGINT keys, snowflake ids), and raw bytes come back as base64 text. A
// value is therefore stored with its type. It serves wherever a key must survive a trip
// through text: the continuation token of a table sync, the transformed keys foreign
// keys follow.
package typedvalue

import (
	"bytes"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

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

// Value is one value with its type. Numbers travel as text: JSON numbers are read back
// as float64.
type Value struct {
	Type  string `json:"t"`
	Value string `json:"v,omitempty"`
}

// Marshal encodes a value as JSON. It fails on a type that cannot be carried exactly.
func Marshal(value any) ([]byte, error) {
	typed, err := Encode(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(typed)
}

// Unmarshal decodes a value written by Marshal: nil, bool, int64, uint64, float64,
// string, []byte or time.Time. A bare JSON value, as written before values were typed, is
// returned the way JSON reads it.
func Unmarshal(raw []byte) (any, error) {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		var legacy any
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return nil, err
		}
		return legacy, nil
	}
	var typed Value
	if err := json.Unmarshal(raw, &typed); err != nil {
		return nil, err
	}
	return typed.Decode()
}

// Encode types a value.
func Encode(value any) (Value, error) {
	switch v := value.(type) {
	case nil:
		return Value{Type: typeNull}, nil
	case bool:
		return Value{Type: typeBool, Value: strconv.FormatBool(v)}, nil
	case int:
		return Value{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int8:
		return Value{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int16:
		return Value{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int32:
		return Value{Type: typeInt, Value: strconv.FormatInt(int64(v), 10)}, nil
	case int64:
		return Value{Type: typeInt, Value: strconv.FormatInt(v, 10)}, nil
	case uint:
		return Value{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint8:
		return Value{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint16:
		return Value{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint32:
		return Value{Type: typeUint, Value: strconv.FormatUint(uint64(v), 10)}, nil
	case uint64:
		return Value{Type: typeUint, Value: strconv.FormatUint(v, 10)}, nil
	case float32:
		return Value{Type: typeFloat, Value: strconv.FormatFloat(float64(v), 'g', -1, 32)}, nil
	case float64:
		return Value{Type: typeFloat, Value: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case string:
		return Value{Type: typeString, Value: v}, nil
	case []byte:
		return Value{Type: typeBytes, Value: base64.StdEncoding.EncodeToString(v)}, nil
	case time.Time:
		return Value{Type: typeTime, Value: v.Format(time.RFC3339Nano)}, nil
	case driver.Valuer:
		// A driver may hand back a wrapper of its own rather than a plain Go value — pgx
		// answers *pgtype.Timestamp for a PostgreSQL timestamp. The wrapper knows the
		// value it stands for, and that is what travels.
		return encodeDriverValue(v)
	default:
		return Value{}, fmt.Errorf("type %T cannot be carried exactly through text", value)
	}
}

func encodeDriverValue(valuer driver.Valuer) (Value, error) {
	value, err := valuer.Value()
	if err != nil {
		return Value{}, fmt.Errorf("type %T cannot say the value it carries: %w", valuer, err)
	}
	if _, again := value.(driver.Valuer); again {
		return Value{}, fmt.Errorf("type %T carries another %T", valuer, value)
	}
	return Encode(value)
}

// Decode returns the value with the Go type it was encoded from, integers as int64 or
// uint64 and floats as float64.
func (typed Value) Decode() (any, error) {
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
