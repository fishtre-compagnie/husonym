// Package profile reduces the sampled values of a column to a Profile: counts, shares,
// lengths, layouts and the share of values that pass each format check.
//
// A profile is what leaves the activity that read the rows. It is written in the history
// of the run and sent to the language model, so it must say nothing of a value. It never
// holds a value, a prefix or a suffix of one, a letter or a digit of one, a hash of one
// (the hash of a value of few possible forms is undone by trying them all), a smallest
// or a largest value (an extreme belongs to one person), nor a list of frequent values.
package profile

import (
	"encoding/json"
	"errors"
)

// A profile travels in three payloads of the history of a table's run. Its serialized
// size is bounded, so that the payloads of a table of a thousand columns stay well under
// what a payload may weigh; past that number of columns the shapes are left out.
const (
	MaxSize              = 600
	MaxSizeWithoutShapes = 300
)

// The kinds of values, from what the database driver returned.
const (
	KindText     = "text"
	KindInteger  = "integer"
	KindDecimal  = "decimal"
	KindBoolean  = "boolean"
	KindDate     = "date" // every moment of the sample is at midnight
	KindDateTime = "datetime"
	KindBinary   = "binary"
	KindJSON     = "json"
	KindArray    = "array"
	KindOther    = "other"
)

// Profile is what is kept of the sampled values of one column. Beyond the counts, it
// holds the statistics of the kind most of the values are of.
type Profile struct {
	Rows int `json:"rows"`
	// Nulls counts the null values, Blank the texts that are empty or made of spaces.
	Nulls int `json:"nulls,omitempty"`
	Blank int `json:"blank,omitempty"`
	// Distinct counts the distinct non-null values of the sample.
	Distinct int    `json:"distinct,omitempty"`
	Kind     string `json:"kind,omitempty"`

	// Len is the length of the values: characters of a text, bytes of a binary value.
	Len *Spread `json:"len,omitempty"`
	// The share of each class of characters over all the characters of the texts.
	Letters float64 `json:"letters,omitempty"`
	Digits  float64 `json:"digits,omitempty"`
	Spaces  float64 `json:"spaces,omitempty"`
	Marks   float64 `json:"marks,omitempty"`
	// Words is the mean number of words of a text.
	Words float64 `json:"words,omitempty"`
	// Shapes are the three most frequent layouts of the texts, each with the share of
	// the texts that have it.
	Shapes []Share `json:"shapes,omitempty"`
	// Hits are up to three format checks, each with the share of the values that pass.
	Hits []Share `json:"hits,omitempty"`

	// IntDigits is the number of digits of the integer part of the numbers.
	IntDigits *Spread `json:"int_digits,omitempty"`
	Fraction  float64 `json:"fraction,omitempty"`
	Negative  float64 `json:"negative,omitempty"`

	// Midnight is the share of the moments that are at midnight.
	Midnight float64 `json:"midnight,omitempty"`
	// Age says how far from today the median moment is: "<1y", "1-5y", "5-20y",
	// "20-60y", ">60y" or "future".
	Age string `json:"age,omitempty"`
}

// WithoutShapes returns the profile without its shapes, its largest part.
func (p *Profile) WithoutShapes() *Profile {
	if p == nil {
		return nil
	}
	smaller := *p
	smaller.Shapes = nil
	return &smaller
}

// ForModel returns what a model is told of the profile. A column of one distinct value
// keeps its counts and its kind: the format checks that value passes, which serve the
// rules, would describe it.
func (p *Profile) ForModel() *Profile {
	if p == nil || p.Distinct > 1 || len(p.Hits) == 0 {
		return p
	}
	told := *p
	told.Hits = nil
	return &told
}

// Spread is the smallest, the median and the largest of a series of sizes.
type Spread [3]int

// Share is a name and the share of the values it holds for. It is serialized as a pair.
type Share struct {
	Name  string
	Share float64
}

func (s Share) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{s.Name, s.Share})
}

func (s *Share) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return errors.New("a share is a pair: a name and a number")
	}
	if err := json.Unmarshal(pair[0], &s.Name); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &s.Share)
}
