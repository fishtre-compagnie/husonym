// Package sqlident writes names and string values into SQL text, once for each engine.
//
// A name is written as one identifier whatever characters it holds, and a value as one
// string literal. Every statement that carries a schema, table or column name written by
// hand goes through this package, so that each rule exists in a single place.
//
// The package imports goqu and the standard library only, never another package of the
// product, so that any of them can import it.
//
// With goqu, a schema, table or column name is never given as a string: goqu copies a
// string between its quote characters as it is, and reads dots in it as separators. A name
// is given as d.Table(...), d.Col(...) or d.TableCol(...), which write it as one
// identifier. In place of the forms that take a name as a string:
//
//   - a goqu.Record in Insert().Rows(...): Insert(table).Cols(d.Col(a), d.Col(b)).Vals([]interface{}{x, y});
//   - a goqu.Record or a map in Update().Set(...): Update(table).Set(d.Col(a).Set(x)), which
//     takes one column; goqu takes no expression form for several columns;
//   - a goqu.Ex key in Where(...): Where(d.Col(a).Eq(x));
//   - Select("name") and From("name"): Select(d.Col(a)) and From(d.Table(schema, table)).
package sqlident

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
)

// Dialect is an engine whose SQL this package writes.
type Dialect int

const (
	Postgres Dialect = iota + 1
	MySQL
	SQLServer
)

// The driver names of the product, written out here to keep the package free of imports.
const (
	driverPgx       = "pgx"
	driverPostgres  = "postgres"
	driverMySQL     = "mysql"
	driverSQLServer = "sqlserver"
)

// ForDriver maps "pgx", "postgres", "mysql" and "sqlserver" to a Dialect. A caller returns
// the error it gets for another driver name and never ignores it: a Dialect that was not
// obtained here has no meaning, and writing with it panics.
func ForDriver(driver string) (Dialect, error) {
	switch driver {
	case driverPgx, driverPostgres:
		return Postgres, nil
	case driverMySQL:
		return MySQL, nil
	case driverSQLServer:
		return SQLServer, nil
	default:
		return 0, fmt.Errorf("no SQL dialect for driver %q", driver)
	}
}

// Check refuses a name that no engine takes as an identifier: an empty name and a name
// holding a NUL byte. Any other name is accepted.
func Check(name string) error {
	if name == "" {
		return errors.New("a name cannot be empty")
	}
	if strings.ContainsRune(name, 0) {
		return errors.New("a name cannot hold a NUL byte")
	}
	return nil
}

// Quote writes name as one identifier: "…" with " doubled for PostgreSQL, `…` with `
// doubled for MySQL and MariaDB, […] with ] doubled for SQL Server.
func (d Dialect) Quote(name string) string {
	switch d {
	case Postgres:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case MySQL:
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case SQLServer:
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		panic(fmt.Sprintf("sqlident: unknown dialect %d", int(d)))
	}
}

// Qualified writes schema.name, each part quoted; an empty schema gives the name alone.
func (d Dialect) Qualified(schema, name string) string {
	if schema == "" {
		return d.Quote(name)
	}
	return d.Quote(schema) + "." + d.Quote(name)
}

// Literal writes value as a string literal of the dialect.
//
//   - PostgreSQL: '…' with ' doubled (a backslash is an ordinary character, as the product
//     sessions set standard_conforming_strings).
//   - MySQL and MariaDB: '…' with ' doubled when the value holds no backslash, which reads
//     the same under every sql_mode; otherwise the hex form _utf8mb4 0x… of its UTF-8 bytes.
//   - SQL Server: N'…' with ' doubled, and a backslash directly before a line break (LF, CR
//     or CRLF) closed and reopened as N'…\' + N'…', because T-SQL drops a backslash
//     followed by a line break inside a literal.
func (d Dialect) Literal(value string) string {
	switch d {
	case Postgres:
		return quoteApostrophes(value)
	case MySQL:
		if strings.Contains(value, `\`) {
			return "_utf8mb4 0x" + strings.ToUpper(hex.EncodeToString([]byte(value)))
		}
		return quoteApostrophes(value)
	case SQLServer:
		return sqlServerLiteral(value)
	default:
		panic(fmt.Sprintf("sqlident: unknown dialect %d", int(d)))
	}
}

func quoteApostrophes(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// sqlServerLiteral writes value as N'…' with ' doubled; after each backslash directly
// followed by CR or LF it closes the literal and opens the next one.
func sqlServerLiteral(value string) string {
	var b strings.Builder
	b.WriteString("N'")
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '\'':
			b.WriteString("''")
		case c == '\\' && i+1 < len(value) && (value[i+1] == '\n' || value[i+1] == '\r'):
			b.WriteString("\\' + N'")
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("'")
	return b.String()
}

// Table, Col and TableCol give goqu identifiers that goqu writes as one identifier each.
// goqu copies a name between its quote characters without escaping it, so the quote
// character of goqu's dialect is doubled here, and the constructors used do not split a
// name on dots.
//
// The quote character is the one goqu's dialect writes: " for PostgreSQL, ` for MySQL and
// " for SQL Server. This differs from Quote, which writes brackets for SQL Server: Quote
// serves hand-written statements, whereas these identifiers are prepared for goqu's double
// quotes, which SQL Server reads as identifiers under QUOTED_IDENTIFIER ON, the setting
// of the product sessions.
func (d Dialect) Table(schema, table string) exp.IdentifierExpression {
	if schema == "" {
		return goqu.T(d.goquEscape(table))
	}
	return goqu.S(d.goquEscape(schema)).Table(d.goquEscape(table))
}

// Col gives a column identifier; see Table.
func (d Dialect) Col(name string) exp.IdentifierExpression {
	return d.column("", "", name)
}

// TableCol gives a column identifier qualified by a table or alias; see Table.
func (d Dialect) TableCol(table, col string) exp.IdentifierExpression {
	return d.column("", d.goquEscape(table), col)
}

// column builds the identifier. goqu writes a column named * as the star of a select list;
// that one name is handed over as a literal holding the quoted name, so that it is written
// as one identifier like any other.
func (d Dialect) column(schema, table, col string) exp.IdentifierExpression {
	if col == "*" {
		q := d.goquQuote()
		return exp.NewIdentifierExpression(schema, table, exp.NewLiteralExpression(q+"*"+q))
	}
	return exp.NewIdentifierExpression(schema, table, d.goquEscape(col))
}

// goquQuote is the quote character goqu's dialect writes around an identifier.
func (d Dialect) goquQuote() string {
	switch d {
	case Postgres, SQLServer:
		return `"`
	case MySQL:
		return "`"
	default:
		panic(fmt.Sprintf("sqlident: unknown dialect %d", int(d)))
	}
}

func (d Dialect) goquEscape(name string) string {
	q := d.goquQuote()
	return strings.ReplaceAll(name, q, q+q)
}
