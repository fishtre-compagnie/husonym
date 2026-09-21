// Package phoneformat pseudonymizes phone numbers in place: the prefix, every
// separator and the length are kept, the digits that identify the subscriber are
// replaced by a keyed permutation (FF1).
//
// Kept as they are:
//   - "0" and the next digit of a national number: 06 12 34 56 78 → 06 ·· ·· ·· ··
//   - "+" or "00", the country code and the next digit: +33 6 12 34 56 78 → +33 6 ·· ·· ·· ··
//   - every character that is not a digit, at its position.
//
// The next digit is also the tweak of the permutation, and the subscriber digits
// are its input. So the same number written nationally and internationally keeps
// the same subscriber digits, and two distinct numbers never meet: the permutation
// is a bijection for a given prefix.
package phoneformat

import (
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/worker/pkg/fpe"
)

// minSubscriberDigits is the fewest digits worth hiding behind a kept prefix. Below
// it, a number is too short for its prefix to be kept: all its digits are replaced.
const minSubscriberDigits = 6

// Pseudonymizer replaces the subscriber digits of phone numbers under one key.
type Pseudonymizer struct {
	ff1 *fpe.FF1
}

// New builds a Pseudonymizer from a 256-bit key. The same key gives the same output
// for the same number: its scope is the scope of consistency.
func New(key [32]byte) *Pseudonymizer {
	ff1, err := fpe.NewFF1(key[:])
	if err != nil {
		// Unreachable: 32 bytes is an AES-256 key, the only error NewFF1 reports.
		panic(fmt.Sprintf("phoneformat: %v", err))
	}
	return &Pseudonymizer{ff1: ff1}
}

// Pseudonymize returns value with its subscriber digits replaced. A value with fewer
// than two digits identifies no one and is returned as it is.
func (p *Pseudonymizer) Pseudonymize(value string) (string, error) {
	out := []byte(value)
	var positions []int
	var digits []byte
	for i := range len(out) {
		if out[i] >= '0' && out[i] <= '9' {
			positions = append(positions, i)
			digits = append(digits, out[i]-'0')
		}
	}
	if len(digits) < fpe.MinLength {
		return value, nil
	}

	kept := keptDigits(value, digits)
	if len(digits)-kept < minSubscriberDigits {
		kept = 0
	}
	var tweak []byte
	if kept > 0 {
		tweak = []byte{'0' + digits[kept-1]}
	}

	subscriber, err := p.ff1.Encrypt(digits[kept:], tweak)
	if err != nil {
		return "", fmt.Errorf("phoneformat: %w", err)
	}
	for i, d := range subscriber {
		out[positions[kept+i]] = '0' + d
	}
	return string(out), nil
}

// keptDigits is how many leading digits form the prefix: 0 and the next digit, or the
// country code and the next digit. A number with neither keeps no prefix.
func keptDigits(value string, digits []byte) int {
	international := strings.HasPrefix(strings.TrimSpace(value), "+")
	start := 0
	if !international && len(digits) >= 2 && digits[0] == 0 && digits[1] == 0 {
		international = true
		start = 2
	}
	if international {
		return start + countryCodeLength(digits[start:]) + 1
	}
	if digits[0] == 0 {
		return 2
	}
	return 0
}

// twoDigitCountryCodes are the ITU-T E.164 country codes of two digits; 1 and 7 are
// the only codes of one digit, every other code has three.
var twoDigitCountryCodes = map[int]bool{
	20: true, 27: true,
	30: true, 31: true, 32: true, 33: true, 34: true, 36: true, 39: true,
	40: true, 41: true, 43: true, 44: true, 45: true, 46: true, 47: true, 48: true, 49: true,
	51: true, 52: true, 53: true, 54: true, 55: true, 56: true, 57: true, 58: true,
	60: true, 61: true, 62: true, 63: true, 64: true, 65: true, 66: true,
	81: true, 82: true, 84: true, 86: true,
	90: true, 91: true, 92: true, 93: true, 94: true, 95: true, 98: true,
}

func countryCodeLength(digits []byte) int {
	switch {
	case len(digits) == 0:
		return 0
	case digits[0] == 1 || digits[0] == 7:
		return 1
	case len(digits) >= 2 && twoDigitCountryCodes[int(digits[0])*10+int(digits[1])]:
		return 2
	default:
		return 3
	}
}
