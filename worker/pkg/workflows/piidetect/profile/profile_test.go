package profile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)

func newTestTable(detectors ...Detector) *Table {
	table := NewTable(detectors)
	table.now = func() time.Time { return testNow }
	return table
}

func addValues(table *Table, column string, values ...any) {
	for _, value := range values {
		table.Add(map[string]any{column: value})
	}
}

var endsWithAt = Detector{Name: "at", Match: func(v string) bool { return strings.HasSuffix(v, "@") }}

func Test_Table_ProfilesATextColumn(t *testing.T) {
	table := newTestTable(endsWithAt)
	addValues(table, "c", "ab 12", "cd 34", "Ef-5@", nil, "   ", "ab 12")

	require.Equal(t, 6, table.Rows())
	require.Equal(t, &Profile{
		Rows:     6,
		Nulls:    1,
		Blank:    1,
		Distinct: 4, // the blank value is one of them
		Kind:     KindText,
		Len:      &Spread{5, 5, 5},
		Letters:  0.4, // 8 of 20 characters
		Digits:   0.35,
		Spaces:   0.15,
		Marks:    0.1,
		Words:    1.7, // 7 words in 4 values, cut to one decimal
		Shapes:   []Share{{"a+ 9+", 0.75}, {"A+a+-9+@", 0.25}},
		Hits:     []Share{{"at", 0.25}},
	}, table.Profile("c"))
}

func Test_Table_AColumnNeverSeenHasNoProfile(t *testing.T) {
	table := newTestTable()
	require.Nil(t, table.Profile("c"))
	table.Add(map[string]any{"c": nil})
	require.Equal(t, &Profile{Rows: 1, Nulls: 1}, table.Profile("c"))
	require.Nil(t, table.Profile("other"))
}

// Under three non-blank values a share says nothing: the profile holds counts only.
func Test_Table_NoShapeAndNoHitUnderThreeValues(t *testing.T) {
	table := newTestTable(endsWithAt)
	addValues(table, "c", "a@", "b@", "", nil)

	p := table.Profile("c")
	require.Empty(t, p.Shapes)
	require.Empty(t, p.Hits)
	require.Equal(t, 4, p.Rows)
	require.Equal(t, &Spread{2, 2, 2}, p.Len)

	addValues(table, "c", "c@")
	p = table.Profile("c")
	require.Equal(t, []Share{{"a+@", 1}}, p.Shapes)
	require.Equal(t, []Share{{"at", 1}}, p.Hits)
}

// A share is cut, never rounded up: 99 values of 200 are 0.49, under a half.
func Test_Table_ASharesIsCutToTwoDecimals(t *testing.T) {
	table := newTestTable(endsWithAt)
	for i := range 200 {
		value := fmt.Sprintf("v%d", i)
		if i < 99 {
			value += "@"
		}
		addValues(table, "c", value)
	}
	require.Equal(t, []Share{{"at", 0.49}}, table.Profile("c").Hits)
}

// At most three checks are kept, the most frequent first, and in the order they were
// given when two are as frequent.
func Test_Table_KeepsTheThreeMostFrequentHits(t *testing.T) {
	contains := func(name, part string) Detector {
		return Detector{Name: name, Match: func(v string) bool { return strings.Contains(v, part) }}
	}
	table := newTestTable(contains("w", "w"), contains("x", "x"), contains("y", "y"), contains("z", "z"), contains("none", "!"))
	addValues(table, "c", "wxyz", "xyz", "xyz", "z")

	require.Equal(t, []Share{{"z", 1}, {"x", 0.75}, {"y", 0.75}}, table.Profile("c").Hits)
}

func Test_Table_KeepsTheThreeMostFrequentShapes(t *testing.T) {
	table := newTestTable()
	addValues(table, "c", "a", "a", "a", "1", "1", "A", "A", "-", "a1")

	require.Equal(t, []Share{{"a+", 0.33}, {"9+", 0.22}, {"A+", 0.22}}, table.Profile("c").Shapes)
}

// Only the first 256 characters of a value feed the shapes and the character classes;
// its length counts all of them. A value longer than that passes no format check.
func Test_Table_ReadsTheFirst256CharactersOfAValue(t *testing.T) {
	table := newTestTable(Detector{Name: "any", Match: func(string) bool { return true }})
	long := strings.Repeat("a", 256) + strings.Repeat("1", 244)
	addValues(table, "c", long, long, long)

	p := table.Profile("c")
	require.Equal(t, &Spread{500, 500, 500}, p.Len)
	require.EqualValues(t, 1, p.Letters)
	require.Zero(t, p.Digits)
	require.Empty(t, p.Hits)
}

func Test_Table_ProfilesNumbers(t *testing.T) {
	table := newTestTable(Detector{Name: "three", Match: func(v string) bool { return len(v) == 3 }})
	addValues(table, "n", int64(5), int64(-120), int32(4500), uint8(7), nil)
	addValues(table, "d", 12.5, float32(3), -0.25, 1000.0)

	require.Equal(t, &Profile{
		Rows: 9, Nulls: 5, Distinct: 4, Kind: KindInteger,
		IntDigits: &Spread{1, 1, 4},
		Negative:  0.25,
	}, table.Profile("n"))
	require.Equal(t, &Profile{
		Rows: 9, Nulls: 5, Distinct: 4, Kind: KindDecimal,
		IntDigits: &Spread{1, 1, 4},
		Fraction:  0.5,
		Negative:  0.25,
	}, table.Profile("d"))
}

// Whole numbers are read by the format checks in their printed form: a number may be a
// telephone number.
func Test_Table_ChecksTheFormatOfWholeNumbers(t *testing.T) {
	table := newTestTable(Detector{Name: "nine", Match: func(v string) bool { return len(v) == 9 }})
	addValues(table, "n", int64(612345678), int64(712345678), int64(42))

	require.Equal(t, []Share{{"nine", 0.66}}, table.Profile("n").Hits)
}

func Test_Table_ProfilesDates(t *testing.T) {
	day := func(year int) time.Time { return time.Date(year, time.March, 12, 0, 0, 0, 0, time.UTC) }
	table := newTestTable()
	addValues(table, "birth", day(1950), day(1985), day(2001))
	addValues(table, "created", testNow.Add(-time.Hour), testNow.Add(-48*time.Hour), day(2026))
	addValues(table, "due", testNow.Add(time.Hour), testNow.AddDate(1, 0, 0), testNow.Add(-time.Hour))
	addValues(table, "old", day(1900), day(1920), day(2020))
	addValues(table, "typed", &husonymtypes.HusonymDateTime{Year: 2023, Month: 5, Day: 2, Hour: 9})

	require.Equal(t, &Profile{Rows: 13, Nulls: 10, Distinct: 3, Kind: KindDate, Age: "20-60y"}, table.Profile("birth"))
	require.Equal(t, &Profile{Rows: 13, Nulls: 10, Distinct: 3, Kind: KindDateTime, Midnight: 0.33, Age: "<1y"}, table.Profile("created"))
	require.Equal(t, "future", table.Profile("due").Age)
	require.Equal(t, ">60y", table.Profile("old").Age)
	require.Equal(t, &Profile{Rows: 13, Nulls: 12, Distinct: 1, Kind: KindDateTime, Age: "1-5y"}, table.Profile("typed"))
}

func Test_Table_AgeBuckets(t *testing.T) {
	for want, moment := range map[string]time.Time{
		"future": testNow.Add(time.Second),
		"<1y":    testNow.AddDate(0, -11, 0),
		"1-5y":   testNow.AddDate(-1, 0, -1),
		"5-20y":  testNow.AddDate(-5, 0, -1),
		"20-60y": testNow.AddDate(-20, 0, -1),
		">60y":   testNow.AddDate(-61, 0, 0),
	} {
		table := newTestTable()
		addValues(table, "c", moment)
		require.Equal(t, want, table.Profile("c").Age, want)
	}
}

func Test_Table_KindsOfValues(t *testing.T) {
	for want, value := range map[string]any{
		KindText:     "text",
		KindInteger:  42,
		KindDecimal:  4.2,
		KindBoolean:  true,
		KindDate:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		KindDateTime: time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC),
		KindBinary:   &husonymtypes.Binary{Bytes: []byte{0xff, 0x00}},
		KindJSON:     map[string]any{"a": 1},
		KindArray:    []any{1, 2},
		KindOther:    &husonymtypes.Interval{Days: 3},
	} {
		table := newTestTable()
		addValues(table, "c", value)
		require.Equal(t, want, table.Profile("c").Kind, want)
	}

	// Bytes that are text are text; a bit string is binary.
	table := newTestTable()
	addValues(table, "bytes", []byte("abc"))
	addValues(table, "raw", []byte{0xff, 0xfe})
	addValues(table, "bits", &husonymtypes.Bits{Bytes: []byte{1}, Len: 3})
	addValues(table, "list", &husonymtypes.HusonymArray{})
	require.Equal(t, KindText, table.Profile("bytes").Kind)
	require.Equal(t, KindBinary, table.Profile("raw").Kind)
	require.Equal(t, KindBinary, table.Profile("bits").Kind)
	require.Equal(t, KindArray, table.Profile("list").Kind)
}

// A binary value gives its kind and its size, nothing else.
func Test_Table_ProfilesBinaryValues(t *testing.T) {
	table := newTestTable(Detector{Name: "any", Match: func(string) bool { return true }})
	addValues(table, "c",
		&husonymtypes.Binary{Bytes: []byte("abc")},
		&husonymtypes.Binary{Bytes: []byte("abcdefgh")},
		&husonymtypes.Binary{Bytes: []byte("abcde")},
	)
	require.Equal(t, &Profile{Rows: 3, Distinct: 3, Kind: KindBinary, Len: &Spread{3, 5, 8}}, table.Profile("c"))
}

// The statistics are those of the kind most values are of.
func Test_Table_AColumnOfSeveralKindsIsProfiledAsItsMostFrequentOne(t *testing.T) {
	table := newTestTable()
	addValues(table, "c", "abc", "abd", 7, true)

	p := table.Profile("c")
	require.Equal(t, KindText, p.Kind)
	require.Equal(t, 4, p.Distinct)
	require.Nil(t, p.IntDigits)
	require.Equal(t, &Spread{3, 3, 3}, p.Len)
}

// Nothing of a row is kept: what the caller does with it afterwards changes nothing.
func Test_Table_Add_RetainsNothingOfTheRow(t *testing.T) {
	rows := func() []map[string]any {
		return []map[string]any{
			{"a": "first", "b": []byte("bytes-1"), "c": &husonymtypes.Binary{Bytes: []byte("xyz")}},
			{"a": "second", "b": []byte("bytes-2"), "c": &husonymtypes.Binary{Bytes: []byte("xyzw")}},
			{"a": "third", "b": []byte("bytes-3"), "c": &husonymtypes.Binary{Bytes: []byte("x")}},
		}
	}
	untouched, mutated := newTestTable(), newTestTable()
	for _, row := range rows() {
		untouched.Add(row)
	}
	for _, row := range rows() {
		mutated.Add(row)
		copy(row["b"].([]byte), "CHANGED")
		row["c"].(*husonymtypes.Binary).Bytes = []byte("something else entirely")
		row["a"] = "changed"
		delete(row, "b")
	}
	for _, column := range []string{"a", "b", "c"} {
		require.Equal(t, untouched.Profile(column), mutated.Profile(column), column)
	}
}

// The serialized form of a profile is in the histories of the runs and in the requests to
// the model.
func Test_Profile_SerializedForm(t *testing.T) {
	full := &Profile{
		Rows: 200, Nulls: 3, Blank: 1, Distinct: 197, Kind: KindText,
		Len:     &Spread{12, 22, 41},
		Letters: 0.78, Digits: 0.05, Spaces: 0.01, Marks: 0.16, Words: 1,
		Shapes: []Share{{"a+.a+@a+.a+", 0.61}, {"a+@a+.a+", 0.33}},
		Hits:   []Share{{"email", 0.99}},
	}
	encoded, err := json.Marshal(full)
	require.NoError(t, err)
	require.Equal(t,
		`{"rows":200,"nulls":3,"blank":1,"distinct":197,"kind":"text","len":[12,22,41],`+
			`"letters":0.78,"digits":0.05,"spaces":0.01,"marks":0.16,"words":1,`+
			`"shapes":[["a+.a+@a+.a+",0.61],["a+@a+.a+",0.33]],"hits":[["email",0.99]]}`,
		string(encoded),
	)
	var decoded Profile
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, full, &decoded)

	numbers := &Profile{
		Rows: 10, Distinct: 10, Kind: KindDecimal,
		IntDigits: &Spread{1, 3, 6}, Fraction: 0.4, Negative: 0.1,
	}
	encoded, err = json.Marshal(numbers)
	require.NoError(t, err)
	require.Equal(t, `{"rows":10,"distinct":10,"kind":"decimal","int_digits":[1,3,6],"fraction":0.4,"negative":0.1}`, string(encoded))

	dates := &Profile{Rows: 10, Distinct: 9, Kind: KindDateTime, Midnight: 0.2, Age: "5-20y"}
	encoded, err = json.Marshal(dates)
	require.NoError(t, err)
	require.Equal(t, `{"rows":10,"distinct":9,"kind":"datetime","midnight":0.2,"age":"5-20y"}`, string(encoded))

	require.Error(t, json.Unmarshal([]byte(`{"rows":1,"hits":[["email"]]}`), &decoded))
}

// The size of a profile bounds the payloads it travels in: see MaxSize.
func Test_Profile_SizeBounds(t *testing.T) {
	longMask := strings.Repeat("a+9+", 8)
	widest := []*Profile{
		{
			Rows: 200, Nulls: 199, Blank: 199, Distinct: 200, Kind: KindText,
			Len:     &Spread{2147483647, 2147483647, 2147483647},
			Letters: 0.33, Digits: 0.33, Spaces: 0.11, Marks: 0.23, Words: 12345.6,
			Shapes: []Share{{longMask, 0.33}, {longMask, 0.33}, {longMask, 0.33}},
			Hits:   []Share{{"phone_number", 0.33}, {"credit_card", 0.33}, {"ip_address", 0.33}},
		},
		{
			Rows: 200, Nulls: 199, Distinct: 200, Kind: KindDecimal,
			IntDigits: &Spread{308, 308, 308}, Fraction: 0.99, Negative: 0.99,
			Hits: []Share{{"phone_number", 0.33}, {"credit_card", 0.33}, {"ip_address", 0.33}},
		},
		{Rows: 200, Nulls: 199, Distinct: 200, Kind: KindDateTime, Midnight: 0.99, Age: "20-60y"},
	}
	for _, p := range widest {
		encoded, err := json.Marshal(p)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), MaxSize, string(encoded))

		encoded, err = json.Marshal(p.WithoutShapes())
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), MaxSizeWithoutShapes, string(encoded))
	}
	require.Equal(t, 600, MaxSize)
	require.Equal(t, 300, MaxSizeWithoutShapes)
}

func Test_Profile_WithoutShapes(t *testing.T) {
	p := &Profile{Rows: 3, Kind: KindText, Shapes: []Share{{"a+", 1}}, Hits: []Share{{"email", 1}}}
	require.Equal(t, &Profile{Rows: 3, Kind: KindText, Hits: []Share{{"email", 1}}}, p.WithoutShapes())
	require.Len(t, p.Shapes, 1, "the profile itself is left as it is")
	require.Nil(t, (*Profile)(nil).WithoutShapes())
}

// The text of a value is what may be shown of it. A binary value has none.
func Test_TextOf(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
		ok    bool
	}{
		{"  some text ", "  some text ", true},
		{[]byte("bytes"), "bytes", true},
		{int64(42), "42", true},
		{-3.5, "-3.5", true},
		{true, "true", true},
		{time.Date(1985, 3, 12, 0, 0, 0, 0, time.UTC), "1985-03-12", true},
		{time.Date(1985, 3, 12, 8, 30, 0, 0, time.UTC), "1985-03-12T08:30:00Z", true},
		{&husonymtypes.HusonymDateTime{Year: 2023, Month: 5, Day: 2, Hour: 9}, "2023-05-02T09:00:00Z", true},
		{map[string]any{"b": 1, "a": "x"}, `{"a":"x","b":1}`, true},
		{[]any{1, "two"}, `[1,"two"]`, true},
		{nil, "", false},
		{[]byte{0xff, 0xfe}, "", false},
		{&husonymtypes.Binary{Bytes: []byte("abc")}, "", false},
		{&husonymtypes.Bits{Bytes: []byte{1}, Len: 1}, "", false},
		{&husonymtypes.Interval{Days: 1}, "", false},
	} {
		got, ok := TextOf(tt.value)
		require.Equal(t, tt.ok, ok, "%v", tt.value)
		require.Equal(t, tt.want, got, "%v", tt.value)
	}
}
