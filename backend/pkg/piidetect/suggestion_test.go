package piidetect

import (
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// sampleName is a column name the rule matches: its first keyword, or its first guarded
// word as the whole name.
func sampleName(ru *rule) string {
	if len(ru.keywords) > 0 {
		return strings.ReplaceAll(strings.ReplaceAll(ru.keywords[0], "*", ""), " ", "_")
	}
	return ru.guarded[0].word
}

// The types a column of each kind is declared with.
var typesOfKind = map[columnKind][]string{
	kindText:    {"", "text", "character varying(255)", "varchar(64)", "nvarchar"},
	kindInteger: {"integer", "bigint", "smallint", "int", "tinyint"},
	kindDecimal: {"numeric(10,2)", "decimal(12,0)", "double precision", "float", "money"},
	kindMoment:  {"date", "timestamp without time zone", "datetime2"},
	kindBoolean: {"boolean", "tinyint(1)", "bit"},
}

// A column a rule reports is never left without a transformer to suggest, whatever the
// rule, when its type is one a transformer writes. A rule added without one fails here.
func TestClassify_EverySensitiveFindingSuggestsATransformer(t *testing.T) {
	for i := range rules {
		ru := &rules[i]
		name := sampleName(ru)
		for kind, dataTypes := range typesOfKind {
			for _, dataType := range dataTypes {
				c, ok := Classify(name, dataType)
				if kind == kindText && !ok {
					t.Fatalf("Classify(%q, %q) finds nothing: the sample name of %s matches no rule", name, dataType, ru.category)
				}
				if !ok {
					continue
				}
				if !c.Sensitive {
					t.Errorf("Classify(%q, %q): %s is not sensitive", name, dataType, c.Category)
				}
				if c.Suggested == unspecified {
					t.Errorf("Classify(%q, %q) = %s, without a suggested transformer", name, dataType, c.Category)
				}
			}
		}
	}
}

// The transformer follows the type of the column: text is written in a text column, a
// number in a numeric one, a yes or a no in a boolean one, a moment in a temporal one.
func TestClassify_TheSuggestionFitsTheType(t *testing.T) {
	const (
		scramble = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE
		integer  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
		decimal  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FLOAT64
		boolean  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL
		moment   = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP
	)
	for _, tc := range []struct {
		name, dataType string
		want           mgmtv1alpha1.TransformerSource
	}{
		{"password", "varchar(255)", scramble},
		{"password", "", scramble},
		{"pin", "integer", integer},
		{"api_key", "text", scramble},
		{"tax_id", "varchar(20)", scramble},
		{"tax_id", "bigint", integer},
		{"national_id", "numeric(11,0)", decimal},
		{"ethnicity", "text", scramble},
		{"iban", "varchar(34)", scramble},
		{"bank_account", "varchar(34)", scramble},
		{"account_number", "bigint", integer},
		{"salary", "integer", integer},
		{"salary", "numeric(10,2)", decimal},
		{"salary", "money", decimal},
		{"salary", "varchar(12)", scramble},
		{"age", "smallint", integer},
		{"age", "varchar(3)", scramble},
		{"birth_date", "varchar(10)", scramble},
		{"birth_date", "date", moment},
		{"dob", "integer", integer},
		{"gender", "boolean", boolean},
		{"gender", "tinyint(1)", boolean},
		{"gender", "smallint", integer},
		{"gender", "char(1)", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_GENDER},
		{"postal_code", "integer", integer},
		{"postal_code", "varchar(10)", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE},
		{"ssn", "bigint", integer},
		{"ssn", "varchar(11)", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN},
		{"ip", "bigint", integer},
		{"phone", "bigint", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER},
		{"phone", "numeric(12,0)", decimal},
		{"card_number", "bigint", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER},
		{"card_number", "varchar(19)", scramble},
		{"card_number", "numeric(16,0)", decimal},
	} {
		c, ok := Classify(tc.name, tc.dataType)
		if !ok || c.Suggested != tc.want {
			t.Errorf("Classify(%q, %q) suggests %v (found %v), want %v", tc.name, tc.dataType, c.Suggested, ok, tc.want)
		}
	}
}

// What the values say is a finding with a transformer too.
func TestEveryValueFindingSuggestsATransformer(t *testing.T) {
	for i := range validators {
		val := &validators[i]
		if val.suggested == unspecified {
			t.Errorf("the check of %s suggests no transformer", val.category)
		}
	}
	for _, entity := range []string{
		"EMAIL_ADDRESS", "PHONE_NUMBER", "PERSON", "LOCATION", "CREDIT_CARD", "IP_ADDRESS", "US_SSN", "FR_NIR",
		"FR_PHONE_NUMBER", "FR_POSTAL_CODE", "IBAN_CODE", "FR_SIRET",
	} {
		for _, dataType := range []string{"text", "bigint", "numeric(14,0)"} {
			c, ok := SuggestionForEntity(entity, dataType)
			if !ok || c.Suggested == unspecified {
				t.Errorf("SuggestionForEntity(%q, %q) suggests no transformer", entity, dataType)
			}
		}
	}
}

// A birth date found in the values of a column is scrambled in a text column and
// generated in a native one.
func TestSuggestionForBirthDate(t *testing.T) {
	for dataType, want := range map[string]mgmtv1alpha1.TransformerSource{
		"varchar(10)": mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE,
		"":            mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE,
		"date":        mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP,
		"integer":     mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64,
	} {
		if got := SuggestionForBirthDate(dataType); got != want {
			t.Errorf("SuggestionForBirthDate(%q) = %v, want %v", dataType, got, want)
		}
	}
}

func TestNameCategories(t *testing.T) {
	seen := map[string]bool{}
	for _, category := range NameCategories() {
		if seen[category] {
			t.Errorf("%s is listed twice", category)
		}
		seen[category] = true
	}
	for i := range rules {
		if !seen[rules[i].category] {
			t.Errorf("%s is not listed", rules[i].category)
		}
	}
}
