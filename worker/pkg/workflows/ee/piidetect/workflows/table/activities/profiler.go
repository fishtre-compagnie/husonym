package piidetect_table_activities

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ColumnProfile is a locally computed, aggregated description of a column's sampled values.
// It carries the format signal of the raw values without exposing any of them,
// so it is safe to send to an LLM (including external providers).
type ColumnProfile struct {
	Column        string
	SampleCount   int
	NullCount     int
	DistinctRatio float64 // distinct non-null values / non-null values
	MinLen        int
	AvgLen        float64
	MaxLen        int
	Charset       string
	ShapePatterns []ShapeFrequency
	FormatMatches []FormatMatch
}

// ShapeFrequency is a shape pattern (digits->9, upper->A, lower->a, separators kept)
// with the proportion of sampled values matching it.
type ShapeFrequency struct {
	Shape      string
	Proportion float64
}

// FormatMatch is the verdict of a local format detector over the sampled raw values.
type FormatMatch struct {
	Name    string
	Matched int
	Total   int
}

const (
	maxShapePatterns = 3
	maxShapeLen      = 32
)

// BuildColumnProfiles computes a profile per column from sampled records.
// Computation is pure Go and local; raw values never leave this function.
func BuildColumnProfiles(records Records, columns []string) []*ColumnProfile {
	valuesByColumn := map[string][]any{}
	for _, record := range records {
		for _, col := range columns {
			valuesByColumn[col] = append(valuesByColumn[col], record[col])
		}
	}

	profiles := make([]*ColumnProfile, 0, len(columns))
	for _, col := range columns {
		profiles = append(profiles, buildColumnProfile(col, valuesByColumn[col]))
	}
	return profiles
}

func buildColumnProfile(column string, values []any) *ColumnProfile {
	profile := &ColumnProfile{
		Column:      column,
		SampleCount: len(values),
	}

	nonNull := []string{}
	for _, value := range values {
		if isNullValue(value) {
			profile.NullCount++
			continue
		}
		nonNull = append(nonNull, fmt.Sprintf("%v", value))
	}
	if len(nonNull) == 0 {
		return profile
	}

	distinct := map[string]struct{}{}
	shapeCounts := map[string]int{}
	charset := charsetSummary{}
	totalLen := 0
	profile.MinLen = len([]rune(nonNull[0]))
	for _, value := range nonNull {
		distinct[value] = struct{}{}
		shapeCounts[shapeOf(value)]++
		charset.observe(value)
		length := len([]rune(value))
		totalLen += length
		if length < profile.MinLen {
			profile.MinLen = length
		}
		if length > profile.MaxLen {
			profile.MaxLen = length
		}
	}
	profile.DistinctRatio = float64(len(distinct)) / float64(len(nonNull))
	profile.AvgLen = float64(totalLen) / float64(len(nonNull))
	profile.Charset = charset.String()
	profile.ShapePatterns = topShapes(shapeCounts, len(nonNull), maxShapePatterns)
	profile.FormatMatches = detectFormats(nonNull)

	return profile
}

func isNullValue(value any) bool {
	if value == nil {
		return true
	}
	if m, ok := value.(map[string]any); ok && len(m) == 0 {
		return true
	}
	return false
}

// shapeOf replaces each character by its class: digit -> 9, upper -> A, lower -> a.
// Separators and other characters are kept as-is. Long values are truncated.
func shapeOf(value string) string {
	var sb strings.Builder
	for i, r := range []rune(value) {
		if i >= maxShapeLen {
			sb.WriteRune('…')
			break
		}
		switch {
		case r >= '0' && r <= '9':
			sb.WriteRune('9')
		case r >= 'A' && r <= 'Z':
			sb.WriteRune('A')
		case r >= 'a' && r <= 'z':
			sb.WriteRune('a')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func topShapes(shapeCounts map[string]int, total, k int) []ShapeFrequency {
	shapes := make([]ShapeFrequency, 0, len(shapeCounts))
	for shape, count := range shapeCounts {
		shapes = append(shapes, ShapeFrequency{
			Shape:      shape,
			Proportion: float64(count) / float64(total),
		})
	}
	sort.Slice(shapes, func(i, j int) bool {
		if shapes[i].Proportion != shapes[j].Proportion {
			return shapes[i].Proportion > shapes[j].Proportion
		}
		return shapes[i].Shape < shapes[j].Shape
	})
	if len(shapes) > k {
		shapes = shapes[:k]
	}
	return shapes
}

type charsetSummary struct {
	hasLower bool
	hasUpper bool
	hasDigit bool
	others   map[rune]struct{}
}

func (c *charsetSummary) observe(value string) {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			c.hasDigit = true
		case r >= 'A' && r <= 'Z':
			c.hasUpper = true
		case r >= 'a' && r <= 'z':
			c.hasLower = true
		default:
			if c.others == nil {
				c.others = map[rune]struct{}{}
			}
			c.others[r] = struct{}{}
		}
	}
}

func (c *charsetSummary) String() string {
	parts := []string{}
	if c.hasLower {
		parts = append(parts, "lower")
	}
	if c.hasUpper {
		parts = append(parts, "upper")
	}
	if c.hasDigit {
		parts = append(parts, "digits")
	}
	if len(c.others) > 0 {
		others := make([]string, 0, len(c.others))
		for r := range c.others {
			others = append(others, string(r))
		}
		sort.Strings(others)
		parts = append(parts, fmt.Sprintf("other(%s)", strings.Join(others, "")))
	}
	return strings.Join(parts, ",")
}

// Local format detectors. Regex-level checks (plus cheap checksums to reduce
// false positives on all-digit formats). They run on raw values, locally only.
var (
	emailFormatRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneFormatRegex = regexp.MustCompile(`^\+?[0-9][0-9 .\-()]{6,19}$`)
	ibanFormatRegex  = regexp.MustCompile(`^[A-Z]{2}\d{2}[A-Z0-9]{10,30}$`)
	// French NIR (social security number): 15 digits starting with 1 or 2
	nirFormatRegex = regexp.MustCompile(`^[12]\d{14}$`)
	// Italian Codice Fiscale
	codiceFiscaleFormatRegex = regexp.MustCompile(`^[A-Z]{6}\d{2}[A-Z]\d{2}[A-Z]\d{3}[A-Z]$`)
	// Spanish DNI / NIE
	dniNieFormatRegex = regexp.MustCompile(`^(\d{8}|[XYZ]\d{7})[A-Z]$`)
	digitsOnlyRegex   = regexp.MustCompile(`^\d+$`)
)

type formatDetector struct {
	name  string
	match func(value string) bool
}

func getFormatDetectors() []formatDetector {
	return []formatDetector{
		{"email", func(v string) bool { return emailFormatRegex.MatchString(v) }},
		{"phone", func(v string) bool { return phoneFormatRegex.MatchString(v) && countDigits(v) >= 7 }},
		{"iban", func(v string) bool { return ibanFormatRegex.MatchString(compactUpper(v)) }},
		{"luhn_checksum", isLuhnValid},
		{"fr_nir", func(v string) bool { return nirFormatRegex.MatchString(compactUpper(v)) }},
		{"it_codice_fiscale", func(v string) bool { return codiceFiscaleFormatRegex.MatchString(compactUpper(v)) }},
		{"pl_pesel", isPeselValid},
		{"es_dni_nie", func(v string) bool { return dniNieFormatRegex.MatchString(compactUpper(v)) }},
		{"nl_bsn", isBsnValid},
	}
}

// detectFormats runs every local format detector against the raw values and
// returns only the detectors that matched at least one value.
func detectFormats(values []string) []FormatMatch {
	matches := []FormatMatch{}
	for _, detector := range getFormatDetectors() {
		matched := 0
		for _, value := range values {
			if detector.match(strings.TrimSpace(value)) {
				matched++
			}
		}
		if matched > 0 {
			matches = append(matches, FormatMatch{
				Name:    detector.name,
				Matched: matched,
				Total:   len(values),
			})
		}
	}
	return matches
}

func compactUpper(value string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(value))
}

func countDigits(value string) int {
	count := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			count++
		}
	}
	return count
}

// isLuhnValid checks whether the value is a 12-19 digit number with a valid Luhn checksum
// (payment card numbers and similar identifiers).
func isLuhnValid(value string) bool {
	digits := compactUpper(value)
	if !digitsOnlyRegex.MatchString(digits) || len(digits) < 12 || len(digits) > 19 {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// isPeselValid checks the Polish PESEL format (11 digits with checksum).
func isPeselValid(value string) bool {
	if !digitsOnlyRegex.MatchString(value) || len(value) != 11 {
		return false
	}
	weights := []int{1, 3, 7, 9, 1, 3, 7, 9, 1, 3}
	sum := 0
	for i, w := range weights {
		sum += int(value[i]-'0') * w
	}
	control := (10 - sum%10) % 10
	return control == int(value[10]-'0')
}

// isBsnValid checks the Dutch BSN format (9 digits passing the "elfproef" 11-test).
func isBsnValid(value string) bool {
	if !digitsOnlyRegex.MatchString(value) || len(value) != 9 {
		return false
	}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += int(value[i]-'0') * (9 - i)
	}
	sum -= int(value[8] - '0')
	return sum%11 == 0
}

// FormatColumnProfiles renders profiles as compact text for the LLM prompt.
func FormatColumnProfiles(profiles []*ColumnProfile) string {
	var sb strings.Builder
	for _, p := range profiles {
		sb.WriteString(fmt.Sprintf("- %s: samples=%d nulls=%d", p.Column, p.SampleCount, p.NullCount))
		if p.SampleCount > p.NullCount {
			sb.WriteString(fmt.Sprintf(
				" distinct=%.2f len[min=%d avg=%.1f max=%d] charset=%s",
				p.DistinctRatio, p.MinLen, p.AvgLen, p.MaxLen, p.Charset,
			))
			if len(p.ShapePatterns) > 0 {
				shapes := make([]string, 0, len(p.ShapePatterns))
				for _, s := range p.ShapePatterns {
					shapes = append(shapes, fmt.Sprintf("%q×%.2f", s.Shape, s.Proportion))
				}
				sb.WriteString(" shapes=" + strings.Join(shapes, ","))
			}
			if len(p.FormatMatches) > 0 {
				formats := make([]string, 0, len(p.FormatMatches))
				for _, f := range p.FormatMatches {
					formats = append(formats, fmt.Sprintf("%s %d/%d", f.Name, f.Matched, f.Total))
				}
				sb.WriteString(" formats=" + strings.Join(formats, ","))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
