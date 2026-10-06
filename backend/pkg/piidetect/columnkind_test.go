package piidetect

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// The kind of a column is told by the whole name of its type: neither a schema, nor a
// length, nor the values of an enum, nor a longer name that holds the name of a type.
func TestKindOf(t *testing.T) {
	for dataType, want := range map[string]columnKind{
		"":                                 kindAny,
		"text":                             kindText,
		"character varying(255)":           kindText,
		"VARCHAR(64)":                      kindText,
		"nvarchar":                         kindText,
		"character(6)":                     kindText,
		"public.citext":                    kindText,
		"longtext":                         kindText,
		"integer":                          kindInteger,
		"INTEGER":                          kindInteger,
		"int(11) unsigned":                 kindInteger,
		"bigint unsigned zerofill":         kindInteger,
		"tinyint(4)":                       kindInteger,
		"serial":                           kindInteger,
		"pg_catalog.int4":                  kindInteger,
		"numeric(10,2)":                    kindDecimal,
		"decimal(12,0) unsigned":           kindDecimal,
		"double precision":                 kindDecimal,
		"money":                            kindDecimal,
		"date":                             kindMoment,
		"timestamp(6) with time zone":      kindMoment,
		"timestamp without time zone":      kindMoment,
		"datetime2(7)":                     kindMoment,
		"interval day to second":           kindMoment,
		"year":                             kindMoment,
		"boolean":                          kindBoolean,
		"tinyint(1)":                       kindBoolean,
		"bit":                              kindBoolean,
		"bit(1)":                           kindBoolean,
		"bit(8)":                           kindAny,
		"uuid":                             kindReference,
		"uniqueidentifier":                 kindReference,
		"schema.contact_point":             kindAny,
		"internal.email_t":                 kindAny,
		"maintenance.mood":                 kindAny,
		"updates.label":                    kindAny,
		"real_name_t":                      kindAny,
		"printable":                        kindAny,
		"point":                            kindAny,
		"geometry(Point,4326)":             kindAny,
		"enum('male','female','intersex')": kindAny,
		"set('date','time')":               kindAny,
		`"Audit"."timeline"`:               kindAny,
		"daterange":                        kindAny,
		"integer[]":                        kindAny,
		"character varying(20)[]":          kindAny,
		"jsonb":                            kindAny,
		"bytea":                            kindAny,
	} {
		if got := kindOf(dataType); got != want {
			t.Errorf("kindOf(%q) = %d, want %d", dataType, got, want)
		}
	}
}

func TestClassify_ATypeOfTheSchemaRefusesNothing(t *testing.T) {
	for _, tc := range []struct{ name, dataType, want string }{
		{"email", "schema.contact_point", "email"},
		{"email", "internal.email_t", "email"},
		{"first_name", "real_name_t", "person_first_name"},
		{"last_name", "updates.label", "person_last_name"},
		{"city", "printable", "city"},
		{"address", "geometry(Point,4326)", "street_address"},
		{"gender", "enum('male','female','intersex')", "gender"},
		{"country", "maintenance.mood", "country"},
		{"email", "integer", ""},
		{"email", "pg_catalog.int4", ""},
	} {
		got := ""
		if c, ok := Classify(tc.name, tc.dataType); ok {
			got = c.Category
		}
		if got != tc.want {
			t.Errorf("Classify(%q, %q) = %q, want %q", tc.name, tc.dataType, got, tc.want)
		}
	}
}

// A token, a key or a password is a secret whatever it is stored as; no other datum is
// read in a column made to identify a row.
func TestClassify_ASecretInAnIdentifierColumn(t *testing.T) {
	for _, dataType := range []string{"uuid", "uniqueidentifier"} {
		for _, name := range []string{"api_token", "password_reset_token", "session_token", "secret", "api_key"} {
			c, ok := Classify(name, dataType)
			if !ok || c.Category != "secret" || !c.Sensitive {
				t.Errorf("Classify(%q, %q) = %+v, want a secret", name, dataType, c)
				continue
			}
			if c.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UUID {
				t.Errorf("Classify(%q, %q) suggests %s", name, dataType, c.Suggested)
			}
		}
		for _, name := range []string{"email", "last_name", "user_id", "tax_id", "passport", "phone"} {
			if c, ok := Classify(name, dataType); ok {
				t.Errorf("Classify(%q, %q) = %s, want none", name, dataType, c.Category)
			}
		}
	}
}

// A suggestion is a transformer that takes the type of the column, or none: the datum's
// own in a text column, a generator of the type in a numeric, boolean or temporal one,
// nothing in a column of a type no transformer writes.
func TestClassify_NoSuggestionForATypeNoTransformerWrites(t *testing.T) {
	const scramble = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE
	for _, dataType := range []string{
		"jsonb", "json", "bytea", "text[]", "character varying(20)[]", "inet", "varbinary(64)",
		"enum('a','b')", "set('a','b')", "internal.secret_t", "hstore", "xml",
	} {
		for _, name := range []string{"api_key", "email", "tax_id", "phone"} {
			c, ok := Classify(name, dataType)
			if !ok || !c.Sensitive {
				t.Errorf("Classify(%q, %q) is not a finding", name, dataType)
				continue
			}
			if c.Suggested != unspecified {
				t.Errorf("Classify(%q, %q) suggests %s", name, dataType, c.Suggested)
			}
		}
		if got := SuggestionForBirthDate(dataType); got != unspecified {
			t.Errorf("SuggestionForBirthDate(%q) = %s", dataType, got)
		}
		if c, ok := SuggestionForEntity("IBAN_CODE", dataType); !ok || c.Suggested != unspecified {
			t.Errorf("SuggestionForEntity(IBAN_CODE, %q) = %s", dataType, c.Suggested)
		}
	}
	for _, dataType := range []string{"", "text", "character varying(34)", "nvarchar(34)", "citext", "mediumtext"} {
		if c, ok := Classify("api_key", dataType); !ok || c.Suggested != scramble {
			t.Errorf("Classify(api_key, %q) suggests %s", dataType, c.Suggested)
		}
	}
}

// What the connection schema carries to every reader: the finding, and no transformer
// for a type none writes.
func TestEnrich_ASensitiveColumnOfATypeNoTransformerWrites(t *testing.T) {
	columns := []*mgmtv1alpha1.DatabaseColumn{
		{Column: "api_key", DataType: "jsonb"},
		{Column: "emails", DataType: "text[]"},
		{Column: "email", DataType: "character varying(255)"},
	}
	Enrich(columns)
	for _, column := range columns[:2] {
		if !column.GetIsSensitive() || column.GetSuggestedTransformerSource() != unspecified {
			t.Errorf("%s %s: sensitive %v, suggested %s", column.GetColumn(), column.GetDataType(),
				column.GetIsSensitive(), column.GetSuggestedTransformerSource())
		}
	}
	if columns[2].GetSuggestedTransformerSource() != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL {
		t.Errorf("email suggests %s", columns[2].GetSuggestedTransformerSource())
	}
}

// The values refine what an entity of the content analysis is — a full name or a first
// name, an address or a city — and its transformer with it, when the column takes the
// text transformer of the entity. A column of another type keeps what its type takes.
func TestRefineByValues_KeepsTheSuggestionOfTheType(t *testing.T) {
	const (
		fullName = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME
		city     = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY
		address  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS
	)
	names := []string{"Jean Dupont", "Marie Martin", "Luc Bernard"}
	addresses := []string{"12 rue de la Paix", "3 avenue Foch", "45 boulevard Voltaire"}

	for _, tc := range []struct {
		entity, dataType string
		values           []string
		category         string
		want             mgmtv1alpha1.TransformerSource
	}{
		{"PERSON", "text", names, "person_full_name", fullName},
		{"PERSON", "", names, "person_full_name", fullName},
		{"PERSON", "jsonb", names, "person_full_name", unspecified},
		{"PERSON", "text[]", names, "person_full_name", unspecified},
		{"LOCATION", "character varying(80)", addresses, "street_address", address},
		{"LOCATION", "text", []string{"Lyon", "Paris", "Lille"}, "city", city},
		{"LOCATION", "jsonb", addresses, "street_address", unspecified},
		{"LOCATION", "integer", addresses, "street_address", generateInteger},
	} {
		suggestion, ok := SuggestionForEntity(tc.entity, tc.dataType)
		if !ok {
			t.Fatalf("SuggestionForEntity(%s, %q) finds nothing", tc.entity, tc.dataType)
		}
		category, suggested := RefineByValues(suggestion.Category, suggestion.Suggested, tc.values)
		if category != tc.category || suggested != tc.want {
			t.Errorf("%s in %q: %s, %s; want %s, %s", tc.entity, tc.dataType, category, suggested, tc.category, tc.want)
		}
	}
}

// A generator that writes numbers of ten digits or more is suggested for a column that
// holds them; a narrower integer gets the generator of integers, which is given a range.
func TestClassify_ANarrowIntegerGetsTheGeneratorOfIntegers(t *testing.T) {
	const (
		integer = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
		card    = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER
		phone   = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER
	)
	for _, tc := range []struct {
		name, dataType string
		want           mgmtv1alpha1.TransformerSource
	}{
		{"card_number", "bigint", card},
		{"card_number", "bigint unsigned", card},
		{"card_number", "int8", card},
		{"card_number", "integer", integer},
		{"card_number", "smallint", integer},
		{"phone", "bigint", phone},
		{"phone", "integer", integer},
		{"phone", "int(11)", integer},
		{"phone", "tinyint", integer},
	} {
		c, ok := Classify(tc.name, tc.dataType)
		if !ok || c.Suggested != tc.want {
			t.Errorf("Classify(%q, %q) suggests %s, want %s", tc.name, tc.dataType, c.Suggested, tc.want)
		}
	}
}
