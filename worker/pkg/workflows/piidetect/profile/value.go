package profile

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
)

// sampled is what the profile reads of one non-null value.
type sampled struct {
	kind string
	// text is set for a text, and for a whole number in its printed form.
	text string
	// size is the number of bytes of a binary value.
	size int
	// number is set for a number, moment for a date or a date and time.
	number float64
	moment time.Time
}

// Bytes as PostgreSQL writes them in a text: "\x" and pairs of hexadecimal digits, alone
// or as the elements of an array. The driver returns that text for a column whose type
// it does not know, as an array of a domain over bytea.
var (
	writtenBytes      = regexp.MustCompile(`(?i)^\\x(?:[0-9a-f]{2})+$`)
	writtenBytesArray = regexp.MustCompile(
		`(?i)^\{(?:"?\\\\x(?:[0-9a-f]{2})+"?|NULL)(?:,(?:"?\\\\x(?:[0-9a-f]{2})+"?|NULL))*\}$`,
	)
)

// readText tells what a text holds: a text, or bytes. A text that is not valid UTF-8 is
// bytes whatever its column says, and so is one PostgreSQL wrote bytes in.
func readText(text string) sampled {
	switch {
	case !utf8.ValidString(text):
		return sampled{kind: KindBinary, size: len(text)}
	case writtenBytes.MatchString(text):
		return sampled{kind: KindBinary, size: (len(text) - len(`\x`)) / 2}
	case strings.Contains(text, `\\x`) && writtenBytesArray.MatchString(text):
		return sampled{kind: KindArray}
	}
	return sampled{kind: KindText, text: text}
}

// read tells what kind of value the record mapper of a database returned. The kind comes
// from the Go type: the type of the column in the catalogue does not always say what the
// driver returns.
func read(value any) sampled {
	switch v := value.(type) {
	case string:
		return readText(v)
	case []byte:
		return readText(string(v))
	case bool:
		return sampled{kind: KindBoolean}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return sampled{kind: KindInteger, text: fmt.Sprint(v), number: toFloat(v)}
	case float32, float64:
		return sampled{kind: KindDecimal, number: toFloat(v)}
	case time.Time:
		return sampled{kind: KindDateTime, moment: v}
	case *time.Time:
		if v != nil {
			return sampled{kind: KindDateTime, moment: *v}
		}
	case husonymtypes.HusonymDateTime:
		return sampled{kind: KindDateTime, moment: momentOf(&v)}
	case *husonymtypes.HusonymDateTime:
		if v != nil {
			return sampled{kind: KindDateTime, moment: momentOf(v)}
		}
	case husonymtypes.Binary:
		return sampled{kind: KindBinary, size: len(v.Bytes)}
	case *husonymtypes.Binary:
		if v != nil {
			return sampled{kind: KindBinary, size: len(v.Bytes)}
		}
	case husonymtypes.Bits:
		return sampled{kind: KindBinary, size: len(v.Bytes)}
	case *husonymtypes.Bits:
		if v != nil {
			return sampled{kind: KindBinary, size: len(v.Bytes)}
		}
	case map[string]any:
		return sampled{kind: KindJSON}
	case []any, [][]byte, husonymtypes.HusonymArray, *husonymtypes.HusonymArray:
		return sampled{kind: KindArray}
	}
	return sampled{kind: KindOther}
}

func toFloat(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	}
	return 0
}

func momentOf(dt *husonymtypes.HusonymDateTime) time.Time {
	year := dt.Year
	if dt.IsBC && year > 0 {
		year = -year
	}
	return time.Date(year, time.Month(dt.Month), dt.Day, dt.Hour, dt.Minute, dt.Second, dt.Nano, time.UTC)
}

func atMidnight(moment time.Time) bool {
	return moment.Hour() == 0 && moment.Minute() == 0 && moment.Second() == 0 && moment.Nanosecond() == 0
}

// intDigits counts the digits of the integer part of a number.
func intDigits(number float64) int {
	number = math.Trunc(math.Abs(number))
	if number < 1 || math.IsInf(number, 0) || math.IsNaN(number) {
		return 1
	}
	return int(math.Floor(math.Log10(number))) + 1
}

// TextOf returns the text form of a sampled value: a text as it is, a number or a boolean
// as it prints, a moment in its ISO 8601 form, a JSON value or an array as compact JSON.
// ok is false for a value that has no text form: null, binary, a bit string, anything
// else.
func TextOf(value any) (text string, ok bool) {
	if value == nil {
		return "", false
	}
	s := read(value)
	switch s.kind {
	case KindText:
		return s.text, true
	case KindInteger, KindDecimal, KindBoolean:
		return fmt.Sprint(value), true
	case KindDateTime:
		if atMidnight(s.moment) {
			return s.moment.Format(time.DateOnly), true
		}
		return s.moment.Format(time.RFC3339), true
	case KindJSON, KindArray:
		switch value.(type) {
		case map[string]any, []any:
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", false
			}
			return string(encoded), true
		}
	}
	return "", false
}
