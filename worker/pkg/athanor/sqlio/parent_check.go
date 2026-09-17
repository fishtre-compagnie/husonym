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

// maxKeysPerLookup bounds one lookup query, well under every database's parameter limit.
const maxKeysPerLookup = 500

type parentCheckWriter struct {
	ctx       context.Context
	tx        Tx
	dialect   Dialect
	inner     RowWriter
	table     string
	checks    []ParentCheck
	skip      bool
	onDiscard func(rows int)
}

// NewParentCheckWriter wraps a writer so that rows referencing a missing parent never
// reach it. With skip they are left out and counted through onDiscard; without, the
// first one fails the write, as the foreign key itself would have.
func NewParentCheckWriter(
	ctx context.Context,
	tx Tx,
	dialect Dialect,
	inner RowWriter,
	table string,
	checks []ParentCheck,
	skip bool,
	onDiscard func(rows int),
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
		kept, err := w.rowsWithParent(check, columns, rows)
		if err != nil {
			return err
		}
		if missing := len(rows) - len(kept); missing > 0 {
			if !w.skip {
				return fmt.Errorf("sqlio: %d ligne(s) de %s violent la clé étrangère (%s) vers %s.%s : parent absent de la destination",
					missing, w.table, strings.Join(check.Columns, ", "), check.ParentSchema, check.ParentTable)
			}
			w.onDiscard(missing)
			rows = kept
		}
	}
	return w.inner.WriteBatch(columns, rows)
}

// rowsWithParent returns the rows whose key references an existing parent row, or holds
// the "no parent" value. The database compares the keys itself, with its own collation
// and type rules: it is sent the distinct keys of the batch and answers with the ordinals
// of the ones it found.
func (w *parentCheckWriter) rowsWithParent(check *ParentCheck, columns []string, rows [][]any) ([][]any, error) {
	indexes := make([]int, len(check.Columns))
	for i, name := range check.Columns {
		indexes[i] = -1
		for j, column := range columns {
			if column == name {
				indexes[i] = j
			}
		}
		if indexes[i] < 0 {
			return nil, fmt.Errorf("sqlio: colonne de clé étrangère %q absente du lot écrit dans %s", name, w.table)
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
	for start := 0; start < len(keys); start += maxKeysPerLookup {
		end := min(start+maxKeysPerLookup, len(keys))
		if err := w.lookup(check, keys[start:end], start, found); err != nil {
			return nil, err
		}
	}

	kept := make([][]any, 0, len(rows))
	for r, row := range rows {
		if rowOrdinal[r] < 0 || found[rowOrdinal[r]] {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// lookup marks the keys that have a parent row:
//
//	SELECT v.n FROM (SELECT ? AS n, ? AS k0 UNION ALL SELECT ? AS n, ? AS k0 …) v
//	WHERE EXISTS (SELECT 1 FROM parent p WHERE p.ref0 = v.k0 …)
func (w *parentCheckWriter) lookup(check *ParentCheck, keys [][]any, offset int, found []bool) error {
	var b strings.Builder
	args := make([]any, 0, len(keys)*(len(check.Columns)+1))
	placeholder := 1
	b.WriteString("SELECT v.n FROM (")
	for k, key := range keys {
		if k > 0 {
			b.WriteString(" UNION ALL ")
		}
		fmt.Fprintf(&b, "SELECT %s AS n", w.dialect.Placeholder(placeholder))
		placeholder++
		args = append(args, offset+k)
		for i, value := range key {
			fmt.Fprintf(&b, ", %s AS k%d", w.dialect.Placeholder(placeholder), i)
			placeholder++
			args = append(args, value)
		}
	}
	parent := w.dialect.QuoteIdent(check.ParentTable)
	if check.ParentSchema != "" {
		parent = w.dialect.QuoteIdent(check.ParentSchema) + "." + parent
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
