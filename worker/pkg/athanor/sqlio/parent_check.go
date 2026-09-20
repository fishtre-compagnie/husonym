package sqlio

// parent_check.go — mandatory foreign keys to a table the job copies only in part.
//
// Pages are written with foreign key checks off, so that a table goes in one pass.
// Nothing then refuses a row whose parent was left out of the subset: a diamond in the
// schema, a second key to the same parent or a parent filtered by another subset root
// all select such rows. A nullable key is read as NULL by the query itself; a mandatory
// one cannot be cleared, so the row cannot be written. The parent tables are complete by
// then: the workflow syncs them first.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Tx is what a page is written through: statements, and reads of what is already there.
type Tx interface {
	Execer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ParentCheck is one mandatory foreign key to verify before writing.
type ParentCheck struct {
	Columns       []string
	ParentSchema  string
	ParentTable   string
	ParentColumns []string
	// NoParentValue, when set, is the value of a single-column key meaning "no parent":
	// rows holding it are written as they are.
	NoParentValue *string
}

// maxKeysPerLookup bounds one lookup query. A key of n columns costs n+1 parameters, and
// what a database takes in one statement differs by two orders of magnitude between them:
// a fixed bound would go past SQL Server's on a composite key.
func maxKeysPerLookup(dialect Dialect, columns int) int {
	return min(500, dialect.MaxRowsPerInsert(columns+1))
}

type parentCheckWriter struct {
	ctx       context.Context
	tx        Tx
	dialect   Dialect
	inner     RowWriter
	table     string
	checks    []ParentCheck
	skip      bool
	onDiscard func(dropped []int)
}

// NewParentCheckWriter wraps a writer so that rows referencing a missing parent never
// reach it. With skip they are left out and reported through onDiscard, by their index in
// the batch this writer was given; without, the first one fails the write, as the foreign
// key itself would have.
func NewParentCheckWriter(
	ctx context.Context,
	tx Tx,
	dialect Dialect,
	inner RowWriter,
	table string,
	checks []ParentCheck,
	skip bool,
	onDiscard func(dropped []int),
) RowWriter {
	if len(checks) == 0 {
		return inner
	}
	return &parentCheckWriter{
		ctx: ctx, tx: tx, dialect: dialect, inner: inner, table: table,
		checks: checks, skip: skip, onDiscard: onDiscard,
	}
}

func (w *parentCheckWriter) WriteBatch(columns []string, rows [][]any) error {
	for i := range w.checks {
		check := &w.checks[i]
		kept, dropped, err := w.rowsWithParent(check, columns, rows)
		if err != nil {
			return err
		}
		if len(dropped) > 0 {
			if !w.skip {
				return fmt.Errorf("sqlio: %d ligne(s) de %s violent la clé étrangère (%s) vers %s.%s : parent absent de la destination",
					len(dropped), w.table, strings.Join(check.Columns, ", "), check.ParentSchema, check.ParentTable)
			}
			w.onDiscard(dropped)
			rows = kept
		}
	}
	return w.inner.WriteBatch(columns, rows)
}

// rowsWithParent returns the rows whose key references an existing parent row, or holds
// the "no parent" value, and the indexes of the ones it leaves out. The database compares
// the keys itself, with its own collation and type rules: it is sent the distinct keys of
// the batch and answers with the ordinals of the ones it found.
func (w *parentCheckWriter) rowsWithParent(
	check *ParentCheck,
	columns []string,
	rows [][]any,
) (kept [][]any, dropped []int, err error) {
	indexes := make([]int, len(check.Columns))
	for i, name := range check.Columns {
		indexes[i] = -1
		for j, column := range columns {
			if column == name {
				indexes[i] = j
			}
		}
		if indexes[i] < 0 {
			return nil, nil, fmt.Errorf("sqlio: colonne de clé étrangère %q absente du lot écrit dans %s", name, w.table)
		}
	}

	// Distinct keys of the batch, in order of appearance.
	ordinals := map[string]int{}
	var keys [][]any
	rowOrdinal := make([]int, len(rows))
	for r, row := range rows {
		key := make([]any, len(indexes))
		parts := make([]string, len(indexes))
		for i, idx := range indexes {
			key[i] = row[idx]
			parts[i] = keyText(row[idx])
		}
		if check.NoParentValue != nil && len(parts) == 1 && parts[0] == *check.NoParentValue {
			rowOrdinal[r] = -1
			continue
		}
		text := strings.Join(parts, "\x1f")
		ordinal, seen := ordinals[text]
		if !seen {
			ordinal = len(keys)
			ordinals[text] = ordinal
			keys = append(keys, key)
		}
		rowOrdinal[r] = ordinal
	}

	found := make([]bool, len(keys))
	perLookup := maxKeysPerLookup(w.dialect, len(check.Columns))
	for start := 0; start < len(keys); start += perLookup {
		end := min(start+perLookup, len(keys))
		if err := w.lookup(check, keys[start:end], start, found); err != nil {
			return nil, nil, err
		}
	}

	kept = make([][]any, 0, len(rows))
	for r, row := range rows {
		if rowOrdinal[r] < 0 || found[rowOrdinal[r]] {
			kept = append(kept, row)
			continue
		}
		dropped = append(dropped, r)
	}
	return kept, dropped, nil
}

// lookup marks the keys that have a parent row:
//
//	SELECT v.n FROM (SELECT 0 AS n, p.ref0 AS k0 FROM parent p WHERE 1 = 0
//	                 UNION ALL SELECT ?, ? UNION ALL SELECT ?, ? …) v
//	WHERE EXISTS (SELECT 1 FROM parent p WHERE p.ref0 = v.k0 …)
//
// The first branch reads nothing: it is there to give each column of the derived table
// the type and the collation of the parent key it is compared with. Without it PostgreSQL
// resolves a bare parameter to text and refuses the comparison ("operator does not exist:
// bigint = text"), and naming the type instead would mean carrying the schema of the
// destination around and getting it right for every type a key can have. The planner drops
// the branch, so it costs nothing.
func (w *parentCheckWriter) lookup(check *ParentCheck, keys [][]any, offset int, found []bool) error {
	parent := w.dialect.QuoteIdent(check.ParentTable)
	if check.ParentSchema != "" {
		parent = w.dialect.QuoteIdent(check.ParentSchema) + "." + parent
	}

	var b strings.Builder
	b.WriteString("SELECT v.n FROM (SELECT 0 AS n")
	for i, column := range check.ParentColumns {
		fmt.Fprintf(&b, ", p.%s AS k%d", w.dialect.QuoteIdent(column), i)
	}
	fmt.Fprintf(&b, " FROM %s p WHERE 1 = 0", parent)

	args := make([]any, 0, len(keys)*(len(check.Columns)+1))
	placeholder := 1
	for k, key := range keys {
		fmt.Fprintf(&b, " UNION ALL SELECT %s", w.dialect.Placeholder(placeholder))
		placeholder++
		args = append(args, offset+k)
		for _, value := range key {
			fmt.Fprintf(&b, ", %s", w.dialect.Placeholder(placeholder))
			placeholder++
			args = append(args, value)
		}
	}

	fmt.Fprintf(&b, ") v WHERE EXISTS (SELECT 1 FROM %s p WHERE ", parent)
	for i, column := range check.ParentColumns {
		if i > 0 {
			b.WriteString(" AND ")
		}
		fmt.Fprintf(&b, "p.%s = v.k%d", w.dialect.QuoteIdent(column), i)
	}
	b.WriteString(")")

	result, err := w.tx.QueryContext(w.ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("sqlio: recherche des parents de %s dans %s: %w", w.table, parent, err)
	}
	defer result.Close()
	for result.Next() {
		var ordinal int
		if err := result.Scan(&ordinal); err != nil {
			return fmt.Errorf("sqlio: recherche des parents de %s: %w", w.table, err)
		}
		if ordinal >= 0 && ordinal < len(found) {
			found[ordinal] = true
		}
	}
	return result.Err()
}

// keyText is the text a key value is deduplicated and compared to NoParentValue with.
func keyText(value any) string {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return fmt.Sprint(value)
}

var _ RowWriter = (*parentCheckWriter)(nil)
