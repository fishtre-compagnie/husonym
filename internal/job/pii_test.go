package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func Test_LooksSensitive(t *testing.T) {
	t.Run("recognises personal data from the name and type", func(t *testing.T) {
		category, sensitive := LooksSensitive("email", "character varying(255)")
		require.True(t, sensitive)
		require.Equal(t, "email", category)

		// The column behind a real incident: a login is personal data, and this is the kind of
		// column that turns up in a schema without anyone mapping it.
		category, sensitive = LooksSensitive("LIB_LOGIN", "varchar(50)")
		require.True(t, sensitive)
		require.Equal(t, "username", category)
	})

	t.Run("says nothing about a column it does not recognise", func(t *testing.T) {
		// And "nothing" is the point: `champ_libre` may hold an address. The caller may rank on
		// this verdict, never clear a column with it.
		category, sensitive := LooksSensitive("champ_libre", "text")
		require.False(t, sensitive)
		require.Empty(t, category)

		category, sensitive = LooksSensitive("total", "numeric")
		require.False(t, sensitive)
		require.Empty(t, category)
	})
}

func Test_SuggestedTransformer(t *testing.T) {
	t.Run("a phone column stored as text keeps its format", func(t *testing.T) {
		source, category, ok := SuggestedTransformer("telephone", "character varying")
		require.True(t, ok)
		require.Equal(t, "phone_number", category)
		require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER, source)
	})

	t.Run("nothing for a column it does not recognise", func(t *testing.T) {
		_, _, ok := SuggestedTransformer("champ_libre", "text")
		require.False(t, ok)
	})

	t.Run("nothing when the column's type does not take the suggestion", func(t *testing.T) {
		// The detection reads the name: `state` names a generator of "CA", which a smallint
		// column refuses on every row. A run that writes the suggestion into the job would fail
		// that run and every one after it, until somebody edits the mapping by hand.
		for _, column := range []struct{ name, dataType string }{
			{"state", "smallint"},
			{"city", "int"},
			{"country", "smallint"},
		} {
			_, _, ok := SuggestedTransformer(column.name, column.dataType)
			require.False(t, ok, "%s %s", column.name, column.dataType)
		}
	})

	t.Run("the same column as a string is suggested", func(t *testing.T) {
		source, _, ok := SuggestedTransformer("state", "character varying(2)")
		require.True(t, ok)
		require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE, source)
	})

	t.Run("a date column keeps its suggestion, type spelled out in full", func(t *testing.T) {
		// PostgreSQL reports "timestamp without time zone": a table that only knew "timestamp"
		// would drop the suggestion of every timestamp column in the product.
		_, _, ok := SuggestedTransformer("date_naissance", "timestamp without time zone")
		require.True(t, ok)
	})

	t.Run("a gender stored as a yes or a no gets a boolean", func(t *testing.T) {
		source, _, ok := SuggestedTransformer("gender", "boolean")
		require.True(t, ok)
		require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL, source)
	})
}

// A column name of each category the name rules answer.
var nameOfCategory = map[string]string{
	"secret": "password", "email": "email", "phone_number": "phone", "username": "username",
	"person_full_name": "full_name", "person_first_name": "first_name", "person_last_name": "last_name",
	"ip_address": "ip_address", "street_address": "street", "city": "city", "state": "state",
	"postal_code": "postal_code", "country": "country", "ssn": "ssn", "national_id": "passport",
	"credit_card": "card_number", "iban": "iban", "bank_account": "bank_account", "salary": "salary",
	"ethnicity": "ethnicity", "gender": "gender", "birth_date": "birth_date", "age": "age",
	"mac_address": "mac_address", "marital_status": "marital_status",
}

// A column the name rules find sensitive has a transformer its type takes, for every
// category and every type the three dialects declare a column with: AutoMap never leaves
// it as it is for want of one. A category added to the rules without a name here, or
// without a transformer, fails.
func Test_SuggestedTransformer_EverySensitiveCategory(t *testing.T) {
	dataTypes := []string{
		// PostgreSQL
		"text", "character varying(255)", "character(2)", "citext", "integer", "bigint", "smallint",
		"numeric(10,2)", "real", "double precision", "boolean", "date", "timestamp without time zone",
		// MySQL
		"varchar(255)", "char(36)", "longtext", "int", "tinyint", "tinyint(1)", "decimal(10,2)", "float",
		"double", "datetime", "year",
		// SQL Server
		"nvarchar", "nchar", "ntext", "bit", "money", "smallmoney", "datetime2", "smalldatetime",
	}
	for _, category := range piidetect.NameCategories() {
		name, known := nameOfCategory[category]
		require.True(t, known, "no column name for the category %s", category)
		got, sensitive := LooksSensitive(name, "text")
		require.True(t, sensitive, name)
		require.Equal(t, category, got, name)

		for _, dataType := range dataTypes {
			if _, sensitive := LooksSensitive(name, dataType); !sensitive {
				continue
			}
			_, _, ok := SuggestedTransformer(name, dataType)
			require.True(t, ok, "%s %s is sensitive and has no transformer", name, dataType)
		}
	}
}

func Test_SuggestedTransformer_SecretsIdentifiersAndMoney(t *testing.T) {
	const (
		scramble = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE
		integer  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
		none     = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED
	)
	for _, tc := range []struct {
		name     string
		category string
		// onInteger is none for a datum an integer column cannot hold: the column is then
		// not a finding.
		onInteger mgmtv1alpha1.TransformerSource
	}{
		{"user_pass", "secret", integer},
		{"email_verification_code", "secret", integer},
		{"api_key", "secret", integer},
		{"passnummer", "national_id", integer},
		{"reisepassnummer", "national_id", integer},
		{"id_card_number", "national_id", integer},
		{"tax_id", "national_id", integer},
		{"national_id", "national_id", integer},
		{"ethnicity", "ethnicity", none},
		{"salary", "salary", integer},
		{"age", "age", integer},
		{"iban", "iban", none},
		{"bank_account", "bank_account", integer},
	} {
		source, category, ok := SuggestedTransformer(tc.name, "character varying(64)")
		require.True(t, ok, tc.name)
		require.Equal(t, tc.category, category, tc.name)
		require.Equal(t, scramble, source, tc.name)

		source, _, ok = SuggestedTransformer(tc.name, "integer")
		require.Equal(t, tc.onInteger != none, ok, "%s integer", tc.name)
		require.Equal(t, tc.onInteger, source, "%s integer", tc.name)
		if tc.onInteger == none {
			_, sensitive := LooksSensitive(tc.name, "integer")
			require.False(t, sensitive, "%s integer", tc.name)
		}

		// A type that is not given: the column is a finding, and no transformer can be
		// chosen for a type nobody knows.
		_, sensitive := LooksSensitive(tc.name, "")
		require.True(t, sensitive, tc.name)
		_, _, ok = SuggestedTransformer(tc.name, "")
		require.False(t, ok, "%s without a type", tc.name)
	}
}

// The ranges the catalogue gives a generated number are those of no datum: an age and a
// salary are generated in a range of their own.
func Test_SuggestedConfig(t *testing.T) {
	generateInt := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
	generateFloat := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FLOAT64

	age, ok := SuggestedConfig(generateInt, "age", nil)
	require.True(t, ok)
	require.EqualValues(t, 18, age.GetGenerateInt64Config().GetMin())
	require.EqualValues(t, 90, age.GetGenerateInt64Config().GetMax())

	salary, ok := SuggestedConfig(generateInt, "salary", nil)
	require.True(t, ok)
	require.EqualValues(t, 20000, salary.GetGenerateInt64Config().GetMin())
	require.EqualValues(t, 90000, salary.GetGenerateInt64Config().GetMax())

	salary, ok = SuggestedConfig(generateFloat, "salary", nil)
	require.True(t, ok)
	require.InDelta(t, 20000, salary.GetGenerateFloat64Config().GetMin(), 0)
	require.InDelta(t, 90000, salary.GetGenerateFloat64Config().GetMax(), 0)

	// Another category keeps the catalogue's own config, and a config is never shared.
	pin, ok := SuggestedConfig(generateInt, "secret", nil)
	require.True(t, ok)
	require.EqualValues(t, 1, pin.GetGenerateInt64Config().GetMin())
	require.EqualValues(t, 40, pin.GetGenerateInt64Config().GetMax())
	again, _ := SuggestedConfig(generateInt, "age", nil)
	require.NotSame(t, age, again)

	_, ok = SuggestedConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED, "age", nil)
	require.False(t, ok)
}

// The range a number is generated in fits the column: the plausible range of the category
// where the column holds it, cut at what the column holds otherwise, from zero when the
// lowest plausible value does not fit either.
func Test_SuggestedConfig_FitsTheColumn(t *testing.T) {
	generateInt := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
	generateFloat := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FLOAT64
	column := func(dataType string) *sqlmanager_shared.DatabaseSchemaRow {
		return &sqlmanager_shared.DatabaseSchemaRow{DataType: dataType, NumericPrecision: -1, NumericScale: -1}
	}

	for _, tc := range []struct {
		category, dataType string
		min, max           int64
	}{
		{"salary", "bigint", 20000, 90000},
		{"salary", "integer", 20000, 90000},
		{"salary", "int(11)", 20000, 90000},
		{"salary", "int unsigned", 20000, 90000},
		{"salary", "mediumint", 20000, 90000},
		{"salary", "smallint", 20000, 32767},
		{"salary", "smallint(6)", 20000, 32767},
		{"salary", "smallint unsigned", 20000, 32767},
		{"salary", "tinyint", 0, 127},
		{"salary", "tinyint(4)", 0, 127},
		{"age", "smallint", 18, 90},
		{"age", "tinyint", 18, 90},
		{"age", "tinyint unsigned", 18, 90},
		{"secret", "tinyint", 1, 40},
		{"secret", "bigint", 1, 40},
		// A type the table of widths does not know keeps the range of the category.
		{"salary", "int64", 20000, 90000},
	} {
		config, ok := SuggestedConfig(generateInt, tc.category, column(tc.dataType))
		require.True(t, ok)
		generated := config.GetGenerateInt64Config()
		require.Equalf(t, tc.min, generated.GetMin(), "%s %s: min", tc.category, tc.dataType)
		require.Equalf(t, tc.max, generated.GetMax(), "%s %s: max", tc.category, tc.dataType)
	}

	for _, tc := range []struct {
		category, dataType string
		min, max           float64
	}{
		{"salary", "numeric(10,2)", 20000, 90000},
		{"salary", "numeric(7,2)", 20000, 90000},
		{"salary", "numeric(6,2)", 0, 9999},
		{"salary", "numeric(4,2)", 0, 99},
		{"salary", "decimal(5,0)", 20000, 90000},
		{"salary", "decimal(5)", 20000, 90000},
		{"salary", "decimal(4)", 0, 9999},
		{"salary", "decimal(8,2) unsigned", 20000, 90000},
		{"salary", "numeric", 20000, 90000},
		{"salary", "double precision", 20000, 90000},
		{"salary", "real", 20000, 90000},
		{"salary", "money", 20000, 90000},
		{"age", "numeric(3,2)", 0, 9},
		{"age", "numeric(3,1)", 18, 90},
		{"age", "numeric(2,0)", 18, 90},
		{"age", "numeric(1,0)", 0, 9},
		// No digit before the point: scale equal to precision.
		{"age", "numeric(2,2)", 0, 0.99},
		{"age", "numeric(4,4)", 0, 0.9999},
		{"secret", "numeric(2,2)", 0, 0.99},
		{"secret", "numeric(3,2)", 1, 9},
		{"secret", "numeric(10,2)", 1, 100},
	} {
		config, ok := SuggestedConfig(generateFloat, tc.category, column(tc.dataType))
		require.True(t, ok)
		generated := config.GetGenerateFloat64Config()
		require.InDeltaf(t, tc.min, generated.GetMin(), 1e-9, "%s %s: min", tc.category, tc.dataType)
		require.InDeltaf(t, tc.max, generated.GetMax(), 1e-9, "%s %s: max", tc.category, tc.dataType)
	}

	// SQL Server names the type alone and gives its precision and scale beside it.
	config, ok := SuggestedConfig(generateFloat, "salary", &sqlmanager_shared.DatabaseSchemaRow{
		DataType: "decimal", NumericPrecision: 4, NumericScale: 2,
	})
	require.True(t, ok)
	require.InDelta(t, 0, config.GetGenerateFloat64Config().GetMin(), 0)
	require.InDelta(t, 99, config.GetGenerateFloat64Config().GetMax(), 0)

	// The bits of an integer, which the catalogues report as a precision, bound nothing.
	config, ok = SuggestedConfig(generateFloat, "salary", &sqlmanager_shared.DatabaseSchemaRow{
		DataType: "double precision", NumericPrecision: 32, NumericScale: 0,
	})
	require.True(t, ok)
	require.InDelta(t, 90000, config.GetGenerateFloat64Config().GetMax(), 0)
}
