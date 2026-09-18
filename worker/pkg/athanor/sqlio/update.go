package sqlio

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// UpdateWriter writes columns into rows already in the destination, identified by their
// key: the pass that fills in, once every table has published its new keys, the foreign
// keys a single pass could not know. It receives the key columns and the columns to write
// together; a row whose written values are all NULL is left alone, the insert pass having
// left them NULL already.
type UpdateWriter struct {
	ctx     context.Context
	db      Execer
	dialect Dialect
	table   string
	keys    []string
}

// NewUpdateWriter writes into schema.table the rows it receives, by their key columns.
func NewUpdateWriter(ctx context.Context, db Execer, dialect Dialect, schema, table string, keys []string) *UpdateWriter {
	return &UpdateWriter{
		ctx: ctx, db: db, dialect: dialect,
		table: dialect.QuoteIdent(schema) + "." + dialect.QuoteIdent(table),
		keys:  keys,
	}
}

func (w *UpdateWriter) WriteBatch(columns []string, rows [][]any) error {
	var set, where []int
	for i, column := range columns {
		if slices.Contains(w.keys, column) {
			where = append(where, i)
		} else {
			set = append(set, i)
		}
	}
	if len(where) != len(w.keys) || len(set) == 0 {
		return fmt.Errorf("sqlio: mise à jour de %s : clé %v ou colonnes à écrire absentes de %v", w.table, w.keys, columns)
	}
	assignments := make([]string, len(set))
	for n, i := range set {
		assignments[n] = w.dialect.QuoteIdent(columns[i]) + " = " + w.dialect.Placeholder(n+1)
	}
	conditions := make([]string, len(where))
	for n, i := range where {
		conditions[n] = w.dialect.QuoteIdent(columns[i]) + " = " + w.dialect.Placeholder(len(set)+n+1)
	}
	statement := "UPDATE " + w.table + " SET " + strings.Join(assignments, ", ") +
		" WHERE " + strings.Join(conditions, " AND ")

	args := make([]any, 0, len(columns))
	for _, row := range rows {
		args = args[:0]
		empty := true
		for _, i := range set {
			args = append(args, row[i])
			empty = empty && row[i] == nil
		}
		if empty {
			continue
		}
		for _, i := range where {
			args = append(args, row[i])
		}
		if _, err := w.db.ExecContext(w.ctx, statement, args...); err != nil {
			return fmt.Errorf("sqlio: UPDATE de %s: %w", w.table, err)
		}
	}
	return nil
}

var _ RowWriter = (*UpdateWriter)(nil)
