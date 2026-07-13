package piidetect_table_activities

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_shapeOf(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1 86 03 75 116 001 23", "9 99 99 99 999 999 99"},
		{"jean.dupont@mail.fr", "aaaa.aaaaaa@aaaa.aa"},
		{"FR7630006000011234567890189", "AA9999999999999999999999999"},
		{"Abc-123", "Aaa-999"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, shapeOf(tt.input))
		})
	}
}

func Test_BuildColumnProfiles(t *testing.T) {
	t.Run("computes stats, shapes and format verdicts", func(t *testing.T) {
		records := Records{
			{"email": "jean.dupont@mail.fr", "age": 42},
			{"email": "anna.schmidt@mail.de", "age": 31},
			{"email": nil, "age": 27},
		}

		profiles := BuildColumnProfiles(records, []string{"email", "age"})
		require.Len(t, profiles, 2)

		emailProfile := profiles[0]
		assert.Equal(t, "email", emailProfile.Column)
		assert.Equal(t, 3, emailProfile.SampleCount)
		assert.Equal(t, 1, emailProfile.NullCount)
		assert.Equal(t, 1.0, emailProfile.DistinctRatio)
		assert.Equal(t, 19, emailProfile.MinLen)
		assert.Equal(t, 20, emailProfile.MaxLen)
		assert.Contains(t, emailProfile.Charset, "lower")
		assert.Contains(t, emailProfile.Charset, "@")
		require.NotEmpty(t, emailProfile.ShapePatterns)

		foundEmailFormat := false
		for _, match := range emailProfile.FormatMatches {
			if match.Name == "email" {
				foundEmailFormat = true
				assert.Equal(t, 2, match.Matched)
				assert.Equal(t, 2, match.Total)
			}
		}
		assert.True(t, foundEmailFormat, "expected an email format verdict")

		ageProfile := profiles[1]
		assert.Equal(t, "age", ageProfile.Column)
		assert.Equal(t, 0, ageProfile.NullCount)
		assert.Equal(t, "digits", ageProfile.Charset)
	})

	t.Run("all null column", func(t *testing.T) {
		records := Records{
			{"empty": nil},
			{"empty": map[string]any{}},
		}
		profiles := BuildColumnProfiles(records, []string{"empty"})
		require.Len(t, profiles, 1)
		assert.Equal(t, 2, profiles[0].SampleCount)
		assert.Equal(t, 2, profiles[0].NullCount)
		assert.Empty(t, profiles[0].ShapePatterns)
	})
}

func Test_topShapes(t *testing.T) {
	shapes := topShapes(map[string]int{
		"999": 3,
		"aaa": 1,
		"AAA": 1,
		"9a9": 1,
	}, 6, 3)
	require.Len(t, shapes, 3)
	assert.Equal(t, "999", shapes[0].Shape)
	assert.InDelta(t, 0.5, shapes[0].Proportion, 0.001)
}

func Test_formatDetectors(t *testing.T) {
	tests := []struct {
		detector string
		value    string
		expected bool
	}{
		{"email", "jean.dupont@mail.fr", true},
		{"email", "not-an-email", false},
		{"phone", "+33 6 12 34 56 78", true},
		{"phone", "abc", false},
		{"iban", "FR76 3000 6000 0112 3456 7890 189", true},
		{"iban", "XX00", false},
		{"luhn_checksum", "4539578763621486", true},
		{"luhn_checksum", "4539578763621487", false},
		{"fr_nir", "1 86 03 75 116 001 23", true},
		{"fr_nir", "3 86 03 75 116 001 23", false},
		{"it_codice_fiscale", "RSSMRA85M01H501Z", true},
		{"it_codice_fiscale", "RSSMRA85M01H50", false},
		{"pl_pesel", "44051401359", true},
		{"pl_pesel", "44051401358", false},
		{"es_dni_nie", "12345678Z", true},
		{"es_dni_nie", "X1234567L", true},
		{"es_dni_nie", "1234567", false},
		{"nl_bsn", "111222333", true},
		{"nl_bsn", "111222334", false},
	}

	detectors := map[string]func(string) bool{}
	for _, d := range getFormatDetectors() {
		detectors[d.name] = d.match
	}

	for _, tt := range tests {
		t.Run(tt.detector+"/"+tt.value, func(t *testing.T) {
			match, ok := detectors[tt.detector]
			require.True(t, ok, "unknown detector %s", tt.detector)
			assert.Equal(t, tt.expected, match(tt.value))
		})
	}
}

func Test_FormatColumnProfiles(t *testing.T) {
	profiles := []*ColumnProfile{
		{
			Column:        "num_secu",
			SampleCount:   5,
			NullCount:     0,
			DistinctRatio: 1.0,
			MinLen:        21,
			AvgLen:        21,
			MaxLen:        21,
			Charset:       "digits,other( )",
			ShapePatterns: []ShapeFrequency{{Shape: "9 99 99 99 999 999 99", Proportion: 1.0}},
			FormatMatches: []FormatMatch{{Name: "fr_nir", Matched: 5, Total: 5}},
		},
		{
			Column:      "empty_col",
			SampleCount: 5,
			NullCount:   5,
		},
	}

	text := FormatColumnProfiles(profiles)
	assert.Contains(t, text, "num_secu")
	assert.Contains(t, text, "fr_nir 5/5")
	assert.Contains(t, text, `"9 99 99 99 999 999 99"×1.00`)
	assert.Contains(t, text, "empty_col: samples=5 nulls=5")
	// The rendered profile must never contain raw values.
	assert.NotContains(t, text, "1 86 03 75 116 001 23")
}
