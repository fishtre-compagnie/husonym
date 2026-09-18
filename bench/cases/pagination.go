package cases

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// paginationCases exercise the keyset pagination both engines share: every table is
// read page by page, each page resuming after the order values of the last row read.
func paginationCases() []*Case {
	return []*Case{
		pageExactMultiple(),
		pageNullOrderValues(),
		pageDuplicateRows(),
		pageUnsignedBigintKey(),
		pageDecimalKey(),
		pageDatetimeKey(),
		pageBinaryKey(),
		pageCaseInsensitiveKey(),
		pageCompositeKey(),
		pageEmptyAndSingleRow(),
	}
}

// pageExactMultiple: the row count is an exact multiple of the page size, so the last
// page is full and one more, empty, page is read.
func pageExactMultiple() *Case {
	return &Case{
		ID:       "page-exact-multiple",
		Priority: P1,
		Title:    "Nombre de lignes exactement multiple de la taille de page",
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "label", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := 1; i <= 2*p.PageLimit; i++ {
				emit.Row("ARTICLE", []any{int64(i), fmt.Sprintf("article %d", i)}, Kept())
			}
		},
	}
}

// pageNullOrderValues: the table has no primary key and its only unique index is on a
// nullable column, which becomes the order column. MySQL sorts NULL first, so more NULL
// rows than a page holds end the first page on a NULL: "code > NULL" matches nothing and
// every following row is lost.
func pageNullOrderValues() *Case {
	return &Case{
		ID:       "page-null-order-values",
		Priority: P1,
		Title:    "Colonne de tri nullable : une page qui finit sur NULL perd la suite de la table",
		Tables: []*schema.Table{{
			Name: "BADGE",
			Columns: []schema.Column{
				{Name: "code", Type: schema.Varchar(20), Nullable: true},
				{Name: "label", Type: schema.Varchar(40)},
			},
			Indexes: []schema.Index{{Name: "uq_badge_code", Columns: []string{"code"}, Unique: true}},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := 1; i <= p.PageLimit+p.PageLimit/2; i++ {
				emit.Row("BADGE", []any{nil, fmt.Sprintf("sans code %d", i)}, Kept())
			}
			for i := 1; i <= p.PageLimit; i++ {
				emit.Row("BADGE", []any{fmt.Sprintf("B%05d", i), fmt.Sprintf("badge %d", i)}, Kept())
			}
		},
	}
}

// pageDuplicateRows: no key and no unique index at all, so the order falls back to
// every column, and strictly identical rows sit across a page boundary. "greater than
// the last row" can neither separate them nor read the rest of the group.
func pageDuplicateRows() *Case {
	return &Case{
		ID:       "page-duplicate-rows",
		Priority: P1,
		Title:    "Table sans clé : lignes strictement identiques à cheval sur une frontière de page",
		Tables: []*schema.Table{{
			Name: "JOURNAL",
			Columns: []schema.Column{
				{Name: "niveau", Type: schema.Int32()},
				{Name: "message", Type: schema.Varchar(60)},
			},
		}},
		Seed: func(p Params, emit Emitter) {
			// Sorted by (message, niveau): "a…" rows fill the first page but five, then ten
			// identical "m" rows straddle the boundary, then "z…" rows follow.
			for i := 1; i <= p.PageLimit-5; i++ {
				emit.Row("JOURNAL", []any{int64(1), fmt.Sprintf("a%06d", i)}, Kept())
			}
			for range 10 {
				emit.Row("JOURNAL", []any{int64(2), "m identique"}, Kept())
			}
			for i := 1; i <= p.PageLimit/2; i++ {
				emit.Row("JOURNAL", []any{int64(3), fmt.Sprintf("z%06d", i)}, Kept())
			}
		},
	}
}

// pageUnsignedBigintKey: keys above 2^53 do not survive the JSON number of the
// continuation token; neighbors one apart collapse to the same float and the next page
// starts at the wrong row.
func pageUnsignedBigintKey() *Case {
	return &Case{
		ID:       "page-unsigned-bigint-key",
		Priority: P1,
		Title:    "Clé BIGINT UNSIGNED au-delà de 2^53 dans le jeton de reprise",
		Tables: []*schema.Table{{
			Name: "MESURE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Uint64()},
				{Name: "valeur", Type: schema.Int32()},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			const base = uint64(1) << 60
			for i := 0; i < 2*p.PageLimit+p.PageLimit/2; i++ {
				emit.Row("MESURE", []any{base + uint64(i), int64(i)}, Kept())
			}
		},
	}
}

// keyedTable is a table paged on its primary key, the key type being what the case is
// about, with a plain payload column.
func keyedTable(name string, keyType schema.Type) *schema.Table {
	return &schema.Table{
		Name: name,
		Columns: []schema.Column{
			{Name: idColumn, Type: keyType},
			{Name: "rang", Type: schema.Int32()},
		},
		PrimaryKey: []string{idColumn},
	}
}

// exoticKeyCase pages a table whose key values do not survive a naive trip through the
// JSON continuation token. keyAt returns the key of the i-th row, in key order or not.
func exoticKeyCase(id, title, table string, keyType schema.Type, keyAt func(p Params, i int) any) *Case {
	return &Case{
		ID:       id,
		Priority: P1,
		Title:    title,
		Tables:   []*schema.Table{keyedTable(table, keyType)},
		Seed: func(p Params, emit Emitter) {
			for i := 0; i < 2*p.PageLimit+p.PageLimit/2; i++ {
				emit.Row(table, []any{keyAt(p, i), int64(i)}, Kept())
			}
		},
	}
}

// pageDecimalKey: neighbors differ at the 17th significant digit, beyond a float64.
func pageDecimalKey() *Case {
	return exoticKeyCase("page-decimal-key", "Clé DECIMAL à 17 chiffres significatifs dans le jeton de reprise",
		"TARIF", schema.Decimal(20, 4),
		func(_ Params, i int) any { return fmt.Sprintf("1234567890123.%04d", i) })
}

// pageDatetimeKey: neighbors one microsecond apart.
func pageDatetimeKey() *Case {
	return exoticKeyCase("page-datetime6-key", "Clé DATETIME(6) à la microseconde dans le jeton de reprise",
		"RELEVE", schema.DateTime(6),
		func(p Params, i int) any {
			return timestampText(p.Dialect, "2024-02-29 23:59:59", 999000+i)
		})
}

// timestampText writes an instant the way its database prints it: MySQL pads the fraction
// to the precision of the column, PostgreSQL drops its trailing zeros. The same instants
// are read back either way; only the spelling of the fraction differs, and a row key must
// read the way the column does.
func timestampText(dialect schema.Dialect, second string, micros int) string {
	fraction := fmt.Sprintf("%06d", micros)
	if dialect == schema.Postgres {
		fraction = strings.TrimRight(fraction, "0")
		if fraction == "" {
			return second
		}
	}
	return second + "." + fraction
}

// pageBinaryKey: raw bytes that are not valid UTF-8 and hold NUL bytes.
func pageBinaryKey() *Case {
	return exoticKeyCase("page-binary16-key", "Clé BINARY(16) non UTF-8 dans le jeton de reprise",
		"JETON", schema.Binary(16),
		func(_ Params, i int) any {
			key := []byte{0xff, 0xfe, 0x00, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			binary.BigEndian.PutUint16(key[14:], uint16(i)) //nolint:gosec // a few hundred rows at most
			return key
		})
}

// pageCaseInsensitiveKey: under the default MySQL collation "k0002" and "K0002" are the
// same key and the order ignores case; an engine comparing keys itself would disagree.
func pageCaseInsensitiveKey() *Case {
	// MySQL compares text without regard to case by default; PostgreSQL does not, and
	// saying so there needs a collation of its own — a case of its own too.
	c := exoticKeyCase("page-case-insensitive-key", "Clé texte sous collation insensible à la casse",
		"LIBELLE", schema.Varchar(20),
		func(_ Params, i int) any {
			if i%2 == 0 {
				return fmt.Sprintf("K%04d", i)
			}
			return fmt.Sprintf("k%04d", i)
		})
	c.Dialects = mysqlOnly
	return c
}

// pageCompositeKey: page boundaries fall inside groups sharing the first key column.
func pageCompositeKey() *Case {
	return &Case{
		ID:       "page-composite-key",
		Priority: P1,
		Title:    "Tri sur une clé composite : frontières de page à l'intérieur d'un groupe",
		Tables: []*schema.Table{{
			Name: "STOCK",
			Columns: []schema.Column{
				{Name: "depot", Type: schema.Int32()},
				{Name: "article", Type: schema.Int32()},
				{Name: "qte", Type: schema.Int32()},
			},
			PrimaryKey: []string{"depot", "article"},
		}},
		Seed: func(p Params, emit Emitter) {
			perDepot := p.PageLimit*3/5 + 1
			for depot := int64(1); depot <= 5; depot++ {
				for article := int64(1); article <= int64(perDepot); article++ {
					emit.Row("STOCK", []any{depot, article, depot * article}, Kept())
				}
			}
		},
	}
}

// pageEmptyAndSingleRow: the smallest tables there are.
func pageEmptyAndSingleRow() *Case {
	return &Case{
		ID:       "page-empty-and-single-row",
		Priority: P1,
		Title:    "Table vide et table d'une seule ligne",
		Tables: []*schema.Table{
			keyedTable("VIDE", schema.Int64()),
			keyedTable("SEULE", schema.Int64()),
		},
		Seed: func(p Params, emit Emitter) {
			emit.Row("SEULE", []any{int64(1), int64(1)}, Kept())
		},
	}
}
