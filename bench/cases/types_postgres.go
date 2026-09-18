package cases

import (
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// postgresTypeCases copy, unchanged, the values PostgreSQL types are known to lose on the
// way through a driver, a Go type, JSON or a bound parameter — the counterpart of the
// MySQL family, over the types PostgreSQL has instead.
//
// Every value is written the way the server prints it, since that is what rows are
// compared in. Some of those spellings are the database's own doing and identical on both
// sides of the copy: jsonb reorders its keys and drops duplicates, real rounds to its own
// precision, money follows the locale. What the case watches is that the copy does not add
// a change of its own on top.
func postgresTypeCases() []*Case {
	all := []*Case{
		valuesCase("types-pg-numeric", "numeric sans borne, NaN, double precision et real aux bornes, money", []valueTable{
			{name: "V_NUMERIC", typ: postgresType("numeric"), values: []any{
				"12345678901234567890123456789012345.123456789012345678901234567890",
				"-0.000000000000000000000000000001",
				// Trailing zeros belong to the value: numeric keeps the scale it was given.
				"0.100000000000000000000000000000",
				"NaN",
			}},
			{name: "V_DOUBLE", typ: schema.Float64(), values: []any{
				"1.7976931348623157e+308", "-2.2250738585072014e-308", "0.1",
			}},
			{name: "V_REAL", typ: postgresType("real"), values: []any{"3.14159", "1.6777216e+07"}},
			{name: "V_MONEY", typ: postgresType("money"), values: []any{"$92,233,720,368,547,758.07", "-$0.01"}},
			{name: "V_BIGINT", typ: schema.Int64(), values: []any{"-9223372036854775808", "9223372036854775807", int64(0)}},
			{name: "V_SMALLINT", typ: postgresType("smallint"), values: []any{"-32768", "32767"}},
		}),
		valuesCase("types-pg-temporal", "timestamp et timestamptz à la microseconde, date avant notre ère, time, interval", []valueTable{
			{name: "V_TIMESTAMP", typ: schema.DateTime(6), values: []any{
				"1000-01-01 00:00:00", "2024-02-29 23:59:59.999999", "9999-12-31 23:59:59.999999", "infinity",
			}},
			{name: "V_TIMESTAMPTZ", typ: postgresType("timestamptz(6)"), values: []any{
				"1970-01-01 00:00:01+00", "2024-03-31 00:30:00.123456+00", "2038-01-19 03:14:07.999999+00",
			}},
			{name: "V_DATE", typ: schema.Date(), values: []any{"0001-01-01 BC", "2024-02-29", "9999-12-31"}},
			{name: "V_TIME", typ: postgresType("time(6)"), values: []any{"00:00:00", "23:59:59.999999"}},
			{name: "V_INTERVAL", typ: postgresType("interval"), values: []any{
				"1 year 2 mons 3 days 04:05:06.789", "-838:59:59", "00:00:00",
			}},
		}),
		valuesCase("types-pg-text", "Texte : emojis, guillemets, 'null' et 'DEFAULT' comme vraies valeurs, texte long", []valueTable{
			// PostgreSQL refuses a NUL byte in text, where MySQL stores it: that value
			// belongs to the MySQL family.
			{name: "V_VARCHAR", typ: schema.Varchar(100), values: []any{
				"🚀🧪 emoji", "accentué éàüœ", `'simple' "double" \ antislash`, "null", "NULL", "DEFAULT", "default",
				"", "  espaces  ", "tab\tligne\nretour\r",
			}},
			{name: "V_TEXT", typ: schema.Text(), values: []any{strings.Repeat("ligne longue é🚀 ", 2500)}},
		}),
		valuesCase("types-pg-binary", "bytea : octets non UTF-8, vide, 2 Mo", []valueTable{
			{name: "V_BYTEA", typ: schema.Blob(), values: []any{
				[]byte{0xff, 0xfe, 0x00, 0x80}, []byte{}, []byte("texte valide"), patternBytes(2 << 20),
			}},
		}),
		//nolint:misspell // titre du rapport, rédigé en français
		valuesCase("types-pg-json", "json gardé mot pour mot, jsonb normalisé par la base", []valueTable{
			// json keeps the text it was given, duplicate keys and all.
			{name: "V_JSON", typ: postgresType("json"), values: []any{
				`{"z": 1, "a": 2, "a": 3}`, `{"texte": "é🚀 \"cité\""}`, `[]`, `{}`, `null`, `"chaîne"`, `12`,
			}},
			// jsonb reorders its keys, drops the duplicates and keeps big numbers exact.
			{name: "V_JSONB", typ: schema.JSON(), values: []any{
				`{"a": 3, "z": 1}`, `{"d": 0.1, "e": 1.0, "grand": 12345678901234567890}`, `null`, `[]`,
			}},
		}),
		valuesCase("types-pg-arrays", "Tableaux : entiers, texte à virgules et guillemets, NULL dans le tableau, imbriqué", []valueTable{
			{name: "V_INT_ARRAY", typ: postgresType("integer[]"), values: []any{
				"{1,2,3}", "{}", "{1,NULL,3}", "{-2147483648,2147483647}",
			}},
			{name: "V_TEXT_ARRAY", typ: postgresType("text[]"), values: []any{
				`{"a,b","c\"d",NULL,""}`, `{}`, `{"🚀"}`,
			}},
			{name: "V_NESTED_ARRAY", typ: postgresType("integer[][]"), values: []any{"{{1,2},{3,4}}"}},
		}),
		valuesCase("types-pg-network", "uuid, inet, cidr, macaddr", []valueTable{
			{name: "V_UUID", typ: postgresType("uuid"), values: []any{
				"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", "00000000-0000-0000-0000-000000000000",
			}},
			{name: "V_INET", typ: postgresType("inet"), values: []any{"192.168.0.1/24", "::1/128"}},
			{name: "V_CIDR", typ: postgresType("cidr"), values: []any{"2001:db8::/32", "10.0.0.0/8"}},
			{name: "V_MACADDR", typ: postgresType("macaddr"), values: []any{"08:00:2b:01:02:03"}},
		}),
		enumAndDomain(),
		identityColumns(),
	}
	for _, c := range all {
		c.Dialects = postgresOnly
	}
	return all
}

// postgresOnly marks a case about something only PostgreSQL has.
var postgresOnly = []schema.Dialect{schema.Postgres}

func postgresType(native string) schema.Type {
	return schema.Native(map[schema.Dialect]string{schema.Postgres: native})
}

// enumAndDomain: a column of a user-defined type. Both are declared in the schema of the
// case, so both the source and the destination must carry them before their tables exist.
func enumAndDomain() *Case {
	c := valuesCase("types-pg-enum-domain", "Type énuméré et domaine déclarés dans le schéma", []valueTable{
		{name: "V_ENUM", typ: postgresType("{db}.etat_commande"), values: []any{"brouillon", "validée", ""}},
		{name: "V_DOMAIN", typ: postgresType("{db}.code_postal"), values: []any{"75001", "00000"}},
	})
	c.SchemaSetup = []string{
		"CREATE TYPE {db}.{q:etat_commande} AS ENUM ('brouillon', 'validée', '')",
		"CREATE DOMAIN {db}.{q:code_postal} AS varchar(5) CHECK (VALUE ~ '^[0-9]{5}$')",
	}
	return c
}

// identityColumns: PostgreSQL numbers a GENERATED ALWAYS AS IDENTITY column itself and
// refuses a value of its own unless the insert overrides it. A copy writes exactly what
// the source holds, keys included: without the override every row of such a table is
// refused, and with a column numbered by the destination instead the rows would silently
// change identity and orphan whatever references them.
func identityColumns() *Case {
	return &Case{
		ID:       "types-pg-identity",
		Priority: P1,
		Title:    "Colonnes numérotées par la base : GENERATED ALWAYS, GENERATED BY DEFAULT et serial",
		Tables: []*schema.Table{{
			Name: "COMPTEUR",
			Columns: []schema.Column{
				{Name: idColumn, Type: postgresType("integer GENERATED ALWAYS AS IDENTITY")},
				{Name: "par_defaut", Type: postgresType("integer GENERATED BY DEFAULT AS IDENTITY")},
				{Name: "sequentiel", Type: postgresType("serial")},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			// Identifiers a destination numbering the rows itself could not land on: it
			// would start its own sequence at 1, and the gap would show.
			for i := int64(1); i <= 20; i++ {
				emit.Row("COMPTEUR", []any{1000 + i, 2000 + i, 3000 + i, fmt.Sprintf("ligne %d", i)}, Kept())
			}
		},
	}
}
