package sqlio

// writer.go — un RowWriter qui écrit réellement dans une destination SQL, par
// INSERT groupés, via database/sql standard. Driver-agnostique : on abstrait la
// destination par Execer (que *sql.DB et *sql.Tx satisfont) et on isole les
// spécificités de dialecte (placeholders, quoting) derrière Dialect.

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Execer est le sous-ensemble de database/sql suffisant pour écrire.
// *sql.DB et *sql.Tx le satisfont tels quels.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Dialect isole les différences de syntaxe entre SGBD.
type Dialect interface {
	Placeholder(n int) string // n est 1-indexé
	QuoteIdent(s string) string
}

// PostgresDialect : placeholders $1, $2… et identifiants entre guillemets doubles.
type PostgresDialect struct{}

func (PostgresDialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }
func (PostgresDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// MySQLDialect : placeholders ? et identifiants entre accents graves.
type MySQLDialect struct{}

func (MySQLDialect) Placeholder(int) string { return "?" }
func (MySQLDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// SQLWriter écrit des batches dans une table via INSERT multi-lignes.
type SQLWriter struct {
	ctx     context.Context
	db      Execer
	dialect Dialect
	ref     string // référence de table déjà quotée (schema.table)
}

// NewSQLWriter construit un writer. schema peut être vide (table non qualifiée).
func NewSQLWriter(ctx context.Context, db Execer, dialect Dialect, schema, table string) *SQLWriter {
	ref := dialect.QuoteIdent(table)
	if schema != "" {
		ref = dialect.QuoteIdent(schema) + "." + ref
	}
	return &SQLWriter{ctx: ctx, db: db, dialect: dialect, ref: ref}
}

// WriteBatch insère toutes les lignes du lot en une requête INSERT groupée.
func (w *SQLWriter) WriteBatch(columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = w.dialect.QuoteIdent(c)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES ", w.ref, strings.Join(quoted, ", "))

	args := make([]any, 0, len(rows)*len(columns))
	ph := 1
	for r, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("sqlio: ligne %d a %d valeurs, %d colonnes attendues", r, len(row), len(columns))
		}
		if r > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for c := range columns {
			if c > 0 {
				b.WriteString(", ")
			}
			b.WriteString(w.dialect.Placeholder(ph))
			ph++
			args = append(args, row[c])
		}
		b.WriteByte(')')
	}

	if _, err := w.db.ExecContext(w.ctx, b.String(), args...); err != nil {
		return fmt.Errorf("sqlio: INSERT dans %s: %w", w.ref, err)
	}
	return nil
}

var _ RowWriter = (*SQLWriter)(nil)
