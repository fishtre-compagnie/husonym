package cases

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// typeCases copy, unchanged, the values MySQL types are known to lose on the way through
// a driver, a Go type, JSON or a bound parameter. One table per type, grouped by family
// so a family that fails its run does not hide the others. All P1: a value altered on the
// way is silent corruption.
func typeCases() []*Case {
	return []*Case{
		valuesCase("types-integers", "Entiers aux bornes : BIGINT UNSIGNED, TINYINT(1) hors booléen, YEAR", []valueTable{
			{name: "V_BIGINT", typ: schema.Int64(), values: []any{"-9223372036854775808", "9223372036854775807", int64(0)}},
			{name: "V_UBIGINT", typ: schema.Uint64(), values: []any{"18446744073709551615", "9007199254740993", int64(0)}},
			{name: "V_TINYINT1", typ: schema.Bool(), values: []any{int64(0), int64(1), int64(2), int64(-1), int64(127)}},
			{name: "V_UINT", typ: mysqlType("INT UNSIGNED"), values: []any{"4294967295"}},
			{name: "V_YEAR", typ: mysqlType("YEAR"), values: []any{"1901", "2155"}},
		}),
		valuesCase("types-decimal-float", "DECIMAL(65,30), DOUBLE et FLOAT aux bornes", []valueTable{
			{name: "V_DECIMAL", typ: schema.Decimal(65, 30), values: []any{
				"12345678901234567890123456789012345.123456789012345678901234567890",
				"-0.000000000000000000000000000001",
				"0.100000000000000000000000000000",
			}},
			{name: "V_DOUBLE", typ: schema.Float64(), values: []any{"1.7976931348623157e308", "-2.2250738585072014e-308", "0.1"}},
			{name: "V_FLOAT", typ: mysqlType("FLOAT"), values: []any{"3.14159", "1.17549e-38", "16777217"}},
		}),
		valuesCase("types-temporal", "DATETIME(6), DATE, TIME négatif ou au-delà de 24 h, TIMESTAMP(6)", []valueTable{
			{name: "V_DATETIME6", typ: schema.DateTime(6), values: []any{
				"1000-01-01 00:00:00.000000", "2024-02-29 23:59:59.999999", "9999-12-31 23:59:59.999999",
			}},
			{name: "V_DATE", typ: schema.Date(), values: []any{"1000-01-01", "2024-02-29", "9999-12-31"}},
			{name: "V_TIME6", typ: mysqlType("TIME(6)"), values: []any{
				"-838:59:59.000000", "838:59:59.000000", "25:00:00.000001", "00:00:00.000000",
			}},
			{name: "V_TIMESTAMP6", typ: mysqlType("TIMESTAMP(6) NULL"), values: []any{
				"1970-01-01 00:00:01.000000", "2024-03-31 02:30:00.123456", "2038-01-19 03:14:07.999999",
			}},
		}),
		valuesCase("types-text", "Texte : emojis, guillemets, NUL, 'null' et 'DEFAULT' comme vraies valeurs, CHAR, latin1", []valueTable{
			{name: "V_VARCHAR", typ: schema.Varchar(100), values: []any{
				"🚀🧪 emoji", "accentué éàüœ", `'simple' "double" \ antislash`, "null", "NULL", "DEFAULT", "default",
				"", "  espaces  ", "tab\tligne\nretour\r", "a\x00b",
			}},
			{name: "V_CHAR", typ: mysqlType("CHAR(10)"), values: []any{"abc", "ab  ", ""}},
			// "Ã©tÃ©" is what UTF-8 stored through a latin1 connection looks like (mojibake):
			// it must be copied as it is, not repaired nor encoded once more.
			{name: "V_LATIN1", typ: mysqlType("VARCHAR(50) CHARACTER SET latin1"), values: []any{"café crème", "æøå", "Ã©tÃ©"}},
			{name: "V_TEXT", typ: schema.Text(), values: []any{strings.Repeat("ligne longue é🚀 ", 2500)}},
		}),
		valuesCase("types-binary", "Binaire : BINARY(16), VARBINARY non UTF-8, LONGBLOB de 2 Mo", []valueTable{
			{name: "V_BINARY16", typ: schema.Binary(16), values: []any{
				[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
				[]byte{0xff, 0xfe, 0xfd, 0x80, 0x00, 0x27, 0x22, 0x5c, 1, 2, 3, 4, 5, 6, 7, 8},
			}},
			{name: "V_VARBINARY", typ: mysqlType("VARBINARY(64)"), rawBytes: true, values: []any{
				[]byte{0xff, 0xfe, 0x00, 0x80}, []byte{}, []byte("texte valide"),
			}},
			{name: "V_LONGBLOB", typ: schema.Blob(), values: []any{patternBytes(2 << 20)}},
		}),
		valuesCase("types-json", "JSON : unicode, grands nombres, imbrication, null JSON distinct du NULL SQL", []valueTable{
			{name: "V_JSON", typ: schema.JSON(), values: []any{
				`{"a": 1, "b": [1, 2, {"c": null}]}`, `{"texte": "é🚀 \"cité\""}`, `[]`, `{}`, `null`, `"chaîne"`, `12`,
				`{"grand": 12345678901234567890, "decimal": 0.1, "entier_flottant": 1.0}`,
				`{"z": 1, "a": {"z": 1, "a": 2}}`,
			}},
		}),
		valuesCase("types-enum-set-bit", "ENUM avec valeur vide, SET, BIT(1), BIT(10) et BIT(64)", []valueTable{
			{name: "V_ENUM", typ: mysqlType("ENUM('a','b','')"), values: []any{"a", "b", ""}},
			{name: "V_SET", typ: mysqlType("SET('x','y','z')"), values: []any{"x,z", "", "x,y,z"}},
			{name: "V_BIT1", typ: mysqlType("BIT(1)"), rawBytes: true, values: []any{[]byte{1}, []byte{0}}},
			{name: "V_BIT10", typ: mysqlType("BIT(10)"), rawBytes: true, values: []any{[]byte{0x03, 0xff}, []byte{0x00, 0x80}}},
			{name: "V_BIT64", typ: mysqlType("BIT(64)"), rawBytes: true, values: []any{
				[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				[]byte{0x80, 0, 0, 0, 0, 0, 0, 0x27},
			}},
		}),
		valuesCase("types-geometry", "POINT et GEOMETRY (format interne : SRID puis WKB)", []valueTable{
			{name: "V_POINT", typ: mysqlType("POINT"), rawBytes: true, values: []any{wkbPoint(0, 1, 2), wkbPoint(0, -73.5, 45.25)}},
			{name: "V_GEOMETRY", typ: mysqlType("GEOMETRY"), rawBytes: true, values: []any{wkbPoint(4326, 48.85, 2.35)}},
		}),
		autoIncrementZero(),
		zeroDates(),
	}
}

// valueTable is a table holding the tricky values of one type, plus NULL.
type valueTable struct {
	name     string
	typ      schema.Type
	rawBytes bool
	values   []any
}

// valuesCase copies, unchanged and without subset, one table per type.
func valuesCase(id, title string, tables []valueTable) *Case {
	c := &Case{ID: id, Priority: P1, Title: title}
	for _, vt := range tables {
		c.Tables = append(c.Tables, &schema.Table{
			Name: vt.name,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "v", Type: vt.typ, Nullable: true, RawBytes: vt.rawBytes},
			},
			PrimaryKey: []string{idColumn},
		})
	}
	c.Seed = func(p Params, emit Emitter) {
		for _, vt := range tables {
			for i, value := range vt.values {
				emit.Row(vt.name, []any{int64(i + 1), value}, Kept())
			}
			emit.Row(vt.name, []any{int64(len(vt.values) + 1), nil}, Kept())
		}
	}
	return c
}

func mysqlType(native string) schema.Type {
	return schema.Native(map[schema.Dialect]string{schema.MySQL: native})
}

// patternBytes returns n bytes covering every byte value, NUL and quotes included.
func patternBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// wkbPoint returns a point in the MySQL internal geometry format: SRID, then
// little-endian WKB.
func wkbPoint(srid uint32, x, y float64) []byte {
	b := binary.LittleEndian.AppendUint32(nil, srid)
	b = append(b, 1)                           // little endian
	b = binary.LittleEndian.AppendUint32(b, 1) // wkbPoint
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
	return binary.LittleEndian.AppendUint64(b, math.Float64bits(y))
}

// autoIncrementZero: a row with id 0 in an AUTO_INCREMENT column, the usual target of a
// "parent 0" sentinel. Written without NO_AUTO_VALUE_ON_ZERO, MySQL gives it the next
// free id: the row changes identity and whatever references 0 is orphaned.
func autoIncrementZero() *Case {
	return &Case{
		ID:            "types-auto-increment-zero",
		Priority:      P1,
		Title:         "Ligne id = 0 sur une colonne AUTO_INCREMENT : renumérotée à l'écriture",
		SourceSQLMode: "NO_AUTO_VALUE_ON_ZERO,STRICT_TRANS_TABLES",
		Tables: []*schema.Table{{
			Name: "CATEGORIE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64(), AutoIncrement: true},
				{Name: "nom", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			emit.Row("CATEGORIE", []any{int64(0), "aucune"}, Kept())
			emit.Row("CATEGORIE", []any{int64(1), "pneus"}, Kept())
			emit.Row("CATEGORIE", []any{int64(2), "jantes"}, Kept())
		},
	}
}

// zeroDates: dates a permissive sql_mode once let in, which the strict default of the
// destination refuses. Legacy rows hold them by the thousand; they must be copied as
// they are.
func zeroDates() *Case {
	c := valuesCase("types-zero-dates", "Dates zéro (0000-00-00, mois ou jour à zéro) héritées d'un sql_mode permissif", []valueTable{
		{name: "V_DATE", typ: schema.Date(), values: []any{"0000-00-00", "2024-00-00", "2024-02-00"}},
		{name: "V_DATETIME", typ: schema.DateTime(0), values: []any{"0000-00-00 00:00:00", "2024-00-15 10:00:00"}},
	})
	c.SourceSQLMode = "NO_ENGINE_SUBSTITUTION"
	return c
}
