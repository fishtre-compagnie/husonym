package profile

import (
	"cmp"
	"fmt"
	"hash/maphash"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// Under this number of values a statistic describes one row or two rather than the
	// column: the profile then holds counts and the kind of the values, nothing else.
	minValues = 3
	// A layout, or a format, is published when at least this many rows have it. Under
	// it, it is what one row or two look like. Three is the smallest number for which
	// "several rows" holds, and the floor the format checks of the API's scan use; a
	// higher one would hide the second format of a small sample, which is evidence.
	minRowsForShare = 3
	// Only the first characters of a value feed the shapes and the character classes,
	// and a longer value passes no format check.
	readLimit = 256
	// A profile names at most this many shapes and this many format checks.
	maxShares = 3
)

// Detector is a format check: it tells whether a value has the format it is named after.
type Detector struct {
	Name  string
	Match func(string) bool
}

// Table accumulates the profiles of the columns of one table, row after row.
type Table struct {
	detectors []Detector
	rows      int
	columns   map[string]*column
	// The distinct values of a column are counted by their hashes. The seed is drawn
	// for this table and is gone with it.
	seed maphash.Seed
	now  func() time.Time
}

func NewTable(detectors []Detector) *Table {
	return &Table{
		detectors: detectors,
		columns:   map[string]*column{},
		seed:      maphash.MakeSeed(),
		now:       time.Now,
	}
}

// Rows returns the number of rows added.
func (t *Table) Rows() int {
	return t.rows
}

// Add counts the values of a row. Nothing of the row is retained.
func (t *Table) Add(row map[string]any) {
	t.rows++
	for name, value := range row {
		c, ok := t.columns[name]
		if !ok {
			c = &column{
				kinds:    map[string]int{},
				distinct: map[uint64]struct{}{},
				shapes:   map[string]int{},
				hits:     make([]int, len(t.detectors)),
			}
			t.columns[name] = c
		}
		if value != nil {
			c.add(t, value)
		}
	}
}

// column holds the counts of one column. It holds sizes, counts and layouts, never a
// value.
type column struct {
	present  int // non-null values
	kinds    map[string]int
	distinct map[uint64]struct{}

	blank   int
	lengths []int // of the non-blank texts, in characters
	sizes   []int // of the binary values, in bytes
	letters int
	digits  int
	spaces  int
	marks   int
	words   int
	shapes  map[string]int

	checkedTexts   int // non-blank texts the format checks read
	checkedNumbers int // whole numbers the format checks read
	hits           []int

	intDigits  []int
	fractional int
	negative   int

	midnight int
	ages     [len(ageBuckets)]int
}

func (c *column) add(t *Table, value any) {
	s := read(value)
	c.present++
	c.kinds[s.kind]++
	c.distinct[t.hash(s, value)] = struct{}{}

	switch s.kind {
	case KindText:
		c.addText(t, s.text)
	case KindBinary:
		c.sizes = append(c.sizes, s.size)
	case KindInteger:
		c.addNumber(s.number)
		c.checkedNumbers++
		c.check(t, s.text)
	case KindDecimal:
		c.addNumber(s.number)
	case KindDateTime:
		if atMidnight(s.moment) {
			c.midnight++
		}
		c.ages[ageBucket(t.now().Sub(s.moment))]++
	}
}

func (c *column) addText(t *Table, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		c.blank++
		return
	}
	c.lengths = append(c.lengths, utf8.RuneCountInString(text))

	head := FirstRunes(trimmed, readLimit)
	for _, r := range head {
		switch {
		case unicode.IsLetter(r):
			c.letters++
		case unicode.IsDigit(r):
			c.digits++
		case unicode.IsSpace(r):
			c.spaces++
		default:
			c.marks++
		}
	}
	c.words += len(strings.Fields(head))
	// A layout that gathers no character is the class of each one: it is not counted, so
	// that it is never published.
	if layout, gathers := layoutOf(head); gathers {
		c.shapes[layout]++
	}

	c.checkedTexts++
	if len(head) == len(trimmed) {
		c.check(t, trimmed)
	}
}

func (c *column) check(t *Table, text string) {
	for i, detector := range t.detectors {
		if detector.Match(text) {
			c.hits[i]++
		}
	}
}

func (c *column) addNumber(number float64) {
	c.intDigits = append(c.intDigits, intDigits(number))
	if number != math.Trunc(number) {
		c.fractional++
	}
	if number < 0 {
		c.negative++
	}
}

// hash identifies a value among those of its column, for the count of distinct values.
func (t *Table) hash(s sampled, value any) uint64 {
	var h maphash.Hash
	h.SetSeed(t.seed)
	h.WriteString(s.kind)
	h.WriteByte(0)
	if s.kind == KindText {
		h.WriteString(s.text)
	} else if text, ok := TextOf(value); ok {
		h.WriteString(text)
	} else {
		fmt.Fprintf(&h, "%v", value)
	}
	return h.Sum64()
}

// FirstRunes returns the first limit characters of a text, the whole text when it has
// no more.
func FirstRunes(text string, limit int) string {
	count := 0
	for i := range text {
		if count == limit {
			return text[:i]
		}
		count++
	}
	return text
}

// Profile returns the profile of a column, nil when no row held the column.
func (t *Table) Profile(name string) *Profile {
	c, ok := t.columns[name]
	if !ok {
		return nil
	}
	p := &Profile{
		Rows:     t.rows,
		Nulls:    t.rows - c.present,
		Blank:    c.blank,
		Distinct: len(c.distinct),
		Kind:     c.kind(),
	}
	moments := c.kinds[KindDateTime]
	if p.Kind == KindDateTime && moments >= minValues && c.midnight == moments {
		p.Kind = KindDate
	}
	// One value, however many rows hold it: a statistic of the column would be a
	// description of that value. The format checks it passes are counted all the same,
	// for the rules; ForModel leaves them out of what the model is told.
	if p.Distinct <= 1 {
		p.Hits = c.formatHits(t, p.Kind)
		return p
	}
	switch p.Kind {
	case KindText:
		texts := len(c.lengths)
		if texts < minValues {
			break
		}
		p.Len = spread(c.lengths)
		if characters := c.letters + c.digits + c.spaces + c.marks; characters > 0 {
			p.Letters = share(c.letters, characters)
			p.Digits = share(c.digits, characters)
			p.Spaces = share(c.spaces, characters)
			p.Marks = share(c.marks, characters)
		}
		p.Words = float64(c.words*10/texts) / 10
		p.Shapes = topShares(c.shapes, texts)
		p.Hits = c.formatHits(t, p.Kind)
	case KindBinary:
		if len(c.sizes) >= minValues {
			p.Len = spread(c.sizes)
		}
	case KindInteger, KindDecimal:
		numbers := len(c.intDigits)
		if numbers < minValues {
			break
		}
		p.IntDigits = spread(c.intDigits)
		p.Fraction = share(c.fractional, numbers)
		p.Negative = share(c.negative, numbers)
		p.Hits = c.formatHits(t, p.Kind)
	case KindDate:
		p.Age = medianAge(c.ages, moments)
	case KindDateTime:
		if moments < minValues {
			break
		}
		p.Midnight = share(c.midnight, moments)
		p.Age = medianAge(c.ages, moments)
	}
	return p
}

// kind is the kind most of the values are of; the order of kindOrder decides between two
// that are as frequent.
func (c *column) kind() string {
	best, most := "", 0
	for _, kind := range kindOrder {
		if count := c.kinds[kind]; count > most {
			best, most = kind, count
		}
	}
	return best
}

var kindOrder = []string{
	KindText, KindInteger, KindDecimal, KindDateTime, KindBoolean, KindBinary, KindJSON, KindArray, KindOther,
}

// formatHits are the format checks the values of a column pass, for the kinds whose
// values are checked — texts and integers — once enough of them were seen.
func (c *column) formatHits(t *Table, kind string) []Share {
	switch {
	case kind == KindText && len(c.lengths) >= minValues:
		return c.hitShares(t, c.checkedTexts)
	case kind == KindInteger && len(c.intDigits) >= minValues:
		return c.hitShares(t, c.checkedNumbers)
	}
	return nil
}

func (c *column) hitShares(t *Table, checked int) []Share {
	counts := make(map[string]int, len(t.detectors))
	order := make(map[string]int, len(t.detectors))
	for i, detector := range t.detectors {
		counts[detector.Name] = c.hits[i]
		order[detector.Name] = i
	}
	return topSharesBy(counts, checked, func(a, b string) int { return cmp.Compare(order[a], order[b]) })
}

func topShares(counts map[string]int, total int) []Share {
	return topSharesBy(counts, total, strings.Compare)
}

// topSharesBy returns the most frequent names with their share of total, the most
// frequent first; tie decides between two that are as frequent. A name that fewer than
// minRowsForShare rows have is left out, and so is a share that is cut to zero.
func topSharesBy(counts map[string]int, total int, tie func(a, b string) int) []Share {
	names := make([]string, 0, len(counts))
	for name, count := range counts {
		if count >= minRowsForShare && share(count, total) > 0 {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		if byCount := cmp.Compare(counts[b], counts[a]); byCount != 0 {
			return byCount
		}
		return tie(a, b)
	})
	if len(names) > maxShares {
		names = names[:maxShares]
	}
	shares := make([]Share, 0, len(names))
	for _, name := range names {
		shares = append(shares, Share{Name: name, Share: share(counts[name], total)})
	}
	if len(shares) == 0 {
		return nil
	}
	return shares
}

// share is count over total, cut to two decimals: a share is never rounded up to a
// threshold it does not reach.
func share(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count*100/total) / 100
}

func spread(sizes []int) *Spread {
	if len(sizes) == 0 {
		return nil
	}
	sorted := slices.Sorted(slices.Values(sizes))
	return &Spread{sorted[0], sorted[(len(sorted)-1)/2], sorted[len(sorted)-1]}
}

// The buckets of the distance between a moment and today, from the nearest future to the
// farthest past.
var ageBuckets = [...]string{"future", "<1y", "1-5y", "5-20y", "20-60y", ">60y"}

const year = 365*24*time.Hour + 6*time.Hour

func ageBucket(age time.Duration) int {
	switch {
	case age < 0:
		return 0
	case age < year:
		return 1
	case age < 5*year:
		return 2
	case age < 20*year:
		return 3
	case age < 60*year:
		return 4
	}
	return 5
}

// medianAge names the bucket the median moment falls in.
func medianAge(ages [len(ageBuckets)]int, moments int) string {
	seen := 0
	for bucket, count := range ages {
		seen += count
		if seen*2 >= moments {
			return ageBuckets[bucket]
		}
	}
	return ""
}
