package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// PostgresRenderer writes PostgreSQL DDL.
//
// A case lives in a schema, not in a database: one server holds them all, which keeps the
// number of connections down and matches how a PostgreSQL user organizes a database.
//
// What MySQL says in a CREATE TABLE and PostgreSQL does not: secondary indexes are their
// own statements, a generated column is always STORED, and there is no equivalent of ON
// UPDATE CURRENT_TIMESTAMP nor of an invisible column. A case asking for one of those is
// refused rather than rendered into something else: it belongs to the databases that have
// it.
type PostgresRenderer struct{}

func (PostgresRenderer) Dialect() Dialect { return Postgres }

func (PostgresRenderer) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (PostgresRenderer) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

// PostgreSQL: "C" compares byte for byte, whatever the collation of the database.
func (PostgresRenderer) ExactCollation() string { return `"C"` }

// A seed writes the values the case declares, into a GENERATED ALWAYS AS IDENTITY column
// as into any other. The clause is accepted on a table that has no such column.
func (PostgresRenderer) InsertOverride() string { return " OVERRIDING SYSTEM VALUE" }

func (PostgresRenderer) BoolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// The driver hands back typed Go values, which would make a Go type decide how a number
// or a date reads: the cast has the server print the value instead, as MySQL always does.
func (r PostgresRenderer) ReadExpr(column string, binary bool) string {
	quoted := r.QuoteIdent(column)
	if binary {
		return "('x''' || encode(" + quoted + ", 'hex') || '''')"
	}
	return "CAST(" + quoted + " AS text)"
}

func (r PostgresRenderer) CreateContainerStatements(container string) []string {
	return []string{
		"DROP SCHEMA IF EXISTS " + r.QuoteIdent(container) + " CASCADE",
		"CREATE SCHEMA " + r.QuoteIdent(container),
	}
}

func (r PostgresRenderer) EnsureContainerStatement(container string) string {
	return "CREATE SCHEMA IF NOT EXISTS " + r.QuoteIdent(container)
}

// PostgreSQL suspends foreign keys through the replication role, which the bench holds as
// a superuser. Unlike the session variable of MySQL it also suspends user triggers, which
// a seed does not have.
func (PostgresRenderer) LoadSessionStatements() []string {
	return []string{"SET session_replication_role = replica"}
}

// PostgreSQL has no server-wide read-only switch a bench can flip: standing up a real
// standby for every pass would cost far more than it proves. Every session opened on the
// database from then on starts read-only, which is what a run opens — and what the
// sessions already loading the seed do not, since the setting is read at connection time.
// Lifting the setting is itself a write, which the session that inherited it can no longer
// make: it first steps out of read-only for itself, which SET is allowed to do and which
// takes effect on the next statement.
func (r PostgresRenderer) ReadOnlyStatements(database string, readOnly bool) []string {
	value := "off"
	if readOnly {
		value = "on"
	}
	return []string{
		"SET SESSION default_transaction_read_only = off",
		"ALTER DATABASE " + r.QuoteIdent(database) + " SET default_transaction_read_only = " + value,
	}
}

func (r PostgresRenderer) Account(user string) string { return r.QuoteIdent(user) }

// Every case is a schema of the same database, which is the one every account connects to.
func (PostgresRenderer) ConnectionDatabase(benchDatabase, _ string) string { return benchDatabase }

func (r PostgresRenderer) AccountStatements(user, password string) []string {
	return []string{
		// What a role still holds refuses its drop, and DROP OWNED BY refuses a role that
		// is not there: both are settled in one statement.
		"DO $husonym$ BEGIN\n" +
			"  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = " + literal(user) + ") THEN\n" +
			"    EXECUTE 'DROP OWNED BY ' || quote_ident(" + literal(user) + ") || ' CASCADE';\n" +
			"    EXECUTE 'DROP ROLE ' || quote_ident(" + literal(user) + ");\n" +
			"  END IF;\n" +
			"END $husonym$",
		"CREATE ROLE " + r.Account(user) + " LOGIN PASSWORD " + literal(password),
	}
}

// literal quotes a value as a SQL string.
func literal(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (r PostgresRenderer) CreateTable(container string, t *Table) ([]string, error) {
	lines := make([]string, 0, len(t.Columns)+1)
	for i := range t.Columns {
		col := &t.Columns[i]
		line, err := r.columnLine(col)
		if err != nil {
			return nil, fmt.Errorf("schema: %s.%s: %w", t.Name, col.Name, err)
		}
		lines = append(lines, line)
	}
	if len(t.PrimaryKey) > 0 {
		lines = append(lines, "PRIMARY KEY ("+r.quoteList(t.PrimaryKey)+")")
	}
	create := fmt.Sprintf("CREATE TABLE %s.%s (\n  %s\n)",
		r.QuoteIdent(container), r.QuoteIdent(t.Name), strings.Join(lines, ",\n  "))
	if t.Options != "" {
		create += " " + t.Options
	}

	stmts := []string{create}
	for _, idx := range t.Indexes {
		kind := "CREATE INDEX"
		if idx.Unique {
			kind = "CREATE UNIQUE INDEX"
		}
		// Index names are unique per schema, not per table as in MySQL: the table name
		// keeps two cases of the same schema from colliding.
		stmts = append(stmts, fmt.Sprintf("%s %s ON %s.%s (%s)",
			kind, r.QuoteIdent(t.Name+"_"+idx.Name), r.QuoteIdent(container), r.QuoteIdent(t.Name),
			r.quoteList(idx.Columns)))
	}
	return stmts, nil
}

func (r PostgresRenderer) columnLine(col *Column) (string, error) {
	if col.OnUpdate != "" {
		return "", fmt.Errorf("ON UPDATE needs a trigger on PostgreSQL: the case belongs to MySQL")
	}
	if col.Invisible {
		return "", fmt.Errorf("PostgreSQL has no invisible column: the case belongs to MySQL")
	}
	typ, err := r.columnType(col.Type)
	if err != nil {
		return "", err
	}
	line := r.QuoteIdent(col.Name) + " " + typ
	if collation := col.Collation[Postgres]; collation != "" {
		line += " COLLATE " + collation
	}
	switch {
	case col.GeneratedAs != "":
		// PostgreSQL 16 only stores generated columns; VIRTUAL arrived in 18.
		line += " GENERATED ALWAYS AS (" + col.GeneratedAs + ") STORED"
	case col.AutoIncrement:
		// BY DEFAULT, not ALWAYS: like AUTO_INCREMENT it accepts a value of its own, which
		// is what a sync writes. ALWAYS is exercised by a case of its own.
		line += " GENERATED BY DEFAULT AS IDENTITY"
	}
	if !col.Nullable {
		line += " NOT NULL"
	}
	if col.Default != "" {
		line += " DEFAULT " + col.Default
	}
	return line, nil
}

func (r PostgresRenderer) AddForeignKeys(container string, t *Table) []string {
	var stmts []string
	for _, fk := range t.ForeignKeys {
		if fk.Virtual {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			r.QuoteIdent(container), r.QuoteIdent(t.Name), r.QuoteIdent(t.Name+"_"+fk.Name),
			r.quoteList(fk.Columns), r.QuoteIdent(container), r.QuoteIdent(fk.RefTable), r.quoteList(fk.RefColumns))
		if fk.OnDelete != "" {
			stmt += " ON DELETE " + fk.OnDelete
		}
		stmts = append(stmts, stmt)
	}
	return stmts
}

func (r PostgresRenderer) quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = r.QuoteIdent(n)
	}
	return strings.Join(quoted, ", ")
}

func (PostgresRenderer) columnType(t Type) (string, error) {
	switch t.Kind {
	case KindInt32:
		return "integer", nil
	case KindInt64:
		return "bigint", nil
	case KindUint64:
		// PostgreSQL has no unsigned integer: numeric holds the whole range of a MySQL
		// BIGINT UNSIGNED, above what a bigint can carry.
		return "numeric(20,0)", nil
	case KindDecimal:
		return fmt.Sprintf("numeric(%d,%d)", t.Precision, t.Scale), nil
	case KindFloat64:
		return "double precision", nil
	case KindBool:
		return "boolean", nil
	case KindVarchar:
		return fmt.Sprintf("varchar(%d)", t.Length), nil
	case KindText:
		return "text", nil
	case KindBinary, KindBlob:
		// bytea has no fixed length: a case relying on the padding of BINARY(n) belongs
		// to MySQL.
		return "bytea", nil
	case KindDate:
		return "date", nil
	case KindDateTime:
		return fmt.Sprintf("timestamp(%d)", t.Length), nil
	case KindJSON:
		return "jsonb", nil
	case KindNative:
		if native, ok := t.Native[Postgres]; ok {
			return native, nil
		}
		return "", fmt.Errorf("native type without a PostgreSQL form")
	default:
		return "", fmt.Errorf("unknown kind %d", t.Kind)
	}
}

var _ Renderer = PostgresRenderer{}
