package presidio

import (
	"encoding/json"
	"errors"
)

// DefaultOperatorKey is the key of the operator applied to the entity types that have none of
// their own.
const DefaultOperatorKey = "DEFAULT"

// HashType is an algorithm the hash operator knows.
type HashType string

const (
	HashMD5    HashType = "md5"
	HashSHA256 HashType = "sha256"
	HashSHA512 HashType = "sha512"
)

const (
	operatorReplace = "replace"
	operatorRedact  = "redact"
	operatorHash    = "hash"
	operatorMask    = "mask"
)

// Operator is what the anonymizer does to a finding. It is built by Replace, Redact, Hash or
// Mask, and two operators that do the same are equal.
type Operator struct {
	kind string
	// with is what the operator works with: the new value of a replace, the algorithm of a
	// hash, the masking character of a mask.
	with        string
	charsToMask int
	fromEnd     bool
}

// Replace puts a value in place of what was found. With an empty value the anonymizer puts the
// entity type between angle brackets.
func Replace(newValue string) Operator {
	return Operator{kind: operatorReplace, with: newValue}
}

// Redact removes what was found.
func Redact() Operator {
	return Operator{kind: operatorRedact}
}

// Hash puts the hash of what was found in its place.
func Hash(algo HashType) Operator {
	return Operator{kind: operatorHash, with: string(algo)}
}

// Mask overwrites charsToMask characters of what was found with maskingChar, from its end or
// from its start.
func Mask(maskingChar string, charsToMask int, fromEnd bool) Operator {
	return Operator{kind: operatorMask, with: maskingChar, charsToMask: charsToMask, fromEnd: fromEnd}
}

// MarshalJSON gives the operator as the anonymizer reads it.
func (o Operator) MarshalJSON() ([]byte, error) {
	switch o.kind {
	case operatorReplace:
		return json.Marshal(struct {
			Type     string `json:"type"`
			NewValue string `json:"new_value"`
		}{o.kind, o.with})
	case operatorRedact:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{o.kind})
	case operatorHash:
		return json.Marshal(struct {
			Type     string `json:"type"`
			HashType string `json:"hash_type"`
		}{o.kind, o.with})
	case operatorMask:
		return json.Marshal(struct {
			Type        string `json:"type"`
			MaskingChar string `json:"masking_char"`
			CharsToMask int    `json:"chars_to_mask"`
			FromEnd     bool   `json:"from_end"`
		}{o.kind, o.with, o.charsToMask, o.fromEnd})
	default:
		return nil, errors.New("presidio: an operator is built by Replace, Redact, Hash or Mask")
	}
}
