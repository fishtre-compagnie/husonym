// Package gen creates the databases of the bench cases and fills the source from the
// seed of each case, recording the expectation of every row as it goes.
//
// Seeds are plain deterministic code: the same case, page limit and scale always produce
// the same rows, so two runs of the bench compare the same work.
package gen

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/oracle"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

const (
	insertBatchRows = 1000
	// maxParams is the number of bound parameters MySQL and PostgreSQL take in one
	// statement.
	maxParams = 65535
)

// CreateSchema drops and recreates the schema of a case, with its tables and declared
// foreign keys, empty. It prepares a destination, and the source before it is loaded.
func CreateSchema(ctx context.Context, db *sql.DB, r schema.Renderer, c *cases.Case) error {
	container := c.Schema()
	stmts := r.CreateContainerStatements(container)
	for _, stmt := range c.SchemaSetup {
		stmts = append(stmts, renderStatement(r, container, stmt))
	}
	for _, t := range c.Tables {
		create, err := r.CreateTable(container, t)
		if err != nil {
			return err
		}
		stmts = append(stmts, create...)
	}
	for _, t := range c.Tables {
		stmts = append(stmts, r.AddForeignKeys(container, t)...)
	}
	for _, stmt := range stmts {
		// Every statement the bench sends goes through the same filling-in, so that a
		// column of a type the case declares can name the schema holding it.
		stmt = renderStatement(r, container, stmt)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("gen: %s: %w\n%s", c.ID, err, stmt)
		}
	}
	return nil
}

// PrepareDestination creates the empty schema of a case on a destination server, then
// applies what the case adds to a destination (triggers…).
func PrepareDestination(ctx context.Context, db *sql.DB, r schema.Renderer, c *cases.Case) error {
	if err := CreateSchema(ctx, db, r, c); err != nil {
		return err
	}
	for _, stmt := range c.DestinationSetup {
		stmt = renderStatement(r, c.Schema(), stmt)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("gen: %s: destination setup: %w\n%s", c.ID, err, stmt)
		}
	}
	return nil
}

// LoadSource creates the database of a case on the source server, fills it from the
// seed and stores the expectation. It returns the number of rows loaded per table.
//
// Rows are written with foreign key checks off: a seed may hold orphans on purpose, the
// way legacy databases do.
func LoadSource(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	c *cases.Case,
	params cases.Params,
) (map[string]int, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := CreateSchema(ctx, db, r, c); err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("gen: %s: %w", c.ID, err)
	}
	defer conn.Close()
	// The connection is closed for good once loaded, so its session settings die with it.
	defer func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }()
	for _, stmt := range r.LoadSessionStatements() {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("gen: %s: %w\n%s", c.ID, err, stmt)
		}
	}
	// sql_mode is what MySQL once let legacy values in with; no other database has it.
	if c.SourceSQLMode != "" && r.Dialect() == schema.MySQL {
		if _, err := conn.ExecContext(ctx, "SET SESSION sql_mode = ?", c.SourceSQLMode); err != nil {
			return nil, fmt.Errorf("gen: %s: %w", c.ID, err)
		}
	}

	l := &loader{
		ctx: ctx, conn: conn, renderer: r, c: c,
		expect:  oracle.NewWriter(c.ID),
		pending: map[string][][]any{},
		counts:  map[string]int{},
	}
	c.Seed(params, l)
	for table := range l.pending {
		l.flush(table)
	}
	if l.err != nil {
		return nil, fmt.Errorf("gen: %s: %w", c.ID, l.err)
	}
	if err := l.expect.Store(ctx, db, r, c.ColumnRules()); err != nil {
		return nil, err
	}
	return l.counts, nil
}

// loader is the Emitter writing a seed to the source. Emitters cannot fail, so it keeps
// the first error and ignores every following row.
type loader struct {
	ctx      context.Context
	conn     *sql.Conn
	renderer schema.Renderer
	c        *cases.Case
	expect   *oracle.Writer
	pending  map[string][][]any
	counts   map[string]int
	err      error
}

func (l *loader) Row(table string, values []any, expect cases.RowExpect) {
	if l.err != nil {
		return
	}
	t := l.c.Table(table)
	if t == nil {
		l.err = fmt.Errorf("seed writes to unknown table %q", table)
		return
	}
	if len(values) != len(t.Columns) {
		l.err = fmt.Errorf("seed gives %d values to %s, which has %d columns", len(values), table, len(t.Columns))
		return
	}
	for _, column := range expect.NullColumns {
		if col := t.Column(column); col == nil || !col.Nullable {
			l.err = fmt.Errorf("seed expects NULL in %s.%s, which is not a nullable column", table, column)
			return
		}
	}
	key, err := RowKey(l.renderer, l.c, t, values)
	if err != nil {
		l.err = err
		return
	}
	if err := l.expect.Add(table, key, expect); err != nil {
		l.err = err
		return
	}
	l.pending[table] = append(l.pending[table], values)
	l.counts[table]++
	if len(l.pending[table]) >= maxRowsPerInsert(len(t.Columns)) {
		l.flush(table)
	}
}

func (l *loader) flush(table string) {
	rows := l.pending[table]
	if l.err != nil || len(rows) == 0 {
		return
	}
	t := l.c.Table(table)
	writable := t.WritableColumns()
	quoted := make([]string, len(writable))
	for i, idx := range writable {
		quoted[i] = l.renderer.QuoteIdent(t.Columns[idx].Name)
	}
	tuples := make([]string, len(rows))
	args := make([]any, 0, len(rows)*len(writable))
	for r, row := range rows {
		marks := make([]string, len(writable))
		for i, idx := range writable {
			marks[i] = l.renderer.Placeholder(len(args) + 1)
			args = append(args, row[idx])
		}
		tuples[r] = "(" + strings.Join(marks, ",") + ")"
	}
	//nolint:gosec // identifiers come from the case definitions and are quoted
	query := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES %s",
		l.renderer.QuoteIdent(l.c.Schema()), l.renderer.QuoteIdent(table), strings.Join(quoted, ", "),
		strings.Join(tuples, ","))
	if _, err := l.conn.ExecContext(l.ctx, query, args...); err != nil {
		l.err = fmt.Errorf("insert into %s: %w", table, err)
	}
	l.pending[table] = rows[:0]
}

func maxRowsPerInsert(columns int) int {
	return min(insertBatchRows, maxParams/columns)
}

// identifierPlaceholder matches {q:name}, an identifier a case leaves to the renderer to
// quote so that one statement serves every database.
var identifierPlaceholder = regexp.MustCompile(`\{q:([^}]*)\}`)

// renderStatement fills in the schema of the case and the identifiers a statement leaves
// to the renderer to quote.
func renderStatement(r schema.Renderer, container, stmt string) string {
	return RenderIdentifiers(r, strings.ReplaceAll(stmt, "{db}", r.QuoteIdent(container)))
}

// RenderIdentifiers replaces every {q:name} of a statement by the quoted identifier.
func RenderIdentifiers(r schema.Renderer, stmt string) string {
	return identifierPlaceholder.ReplaceAllStringFunc(stmt, func(match string) string {
		return r.QuoteIdent(identifierPlaceholder.FindStringSubmatch(match)[1])
	})
}

// RowKey returns the oracle key of a row given in column order, from the identity
// columns of its table.
func RowKey(r schema.Renderer, c *cases.Case, t *schema.Table, values []any) (string, error) {
	identity := c.IdentityColumns(t.Name)
	parts := make([]string, 0, len(identity))
	for _, name := range identity {
		for i := range t.Columns {
			if t.Columns[i].Name != name {
				continue
			}
			text, err := oracle.CanonicalValue(r, values[i], t.Columns[i].IsBinary())
			if err != nil {
				return "", fmt.Errorf("%s.%s: %w", t.Name, name, err)
			}
			parts = append(parts, text)
		}
	}
	return oracle.RowKey(parts), nil
}
