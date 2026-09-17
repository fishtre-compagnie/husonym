package runner

// keys.go — foreign keys following a transformed parent key.
//
// When a transformer changes a column other tables reference (usually a primary key), the
// foreign keys to it are left in passthrough by the user and must end up holding the new
// value. The table holding the key publishes, row by row, its new value under the source
// one; the tables referencing it, synced after it, translate their foreign keys through
// what was published. A key that was never published belongs to a parent row that was
// not copied: keeping the source value could point at another parent that happens to
// receive the same new key, so the reference is treated as missing.

import (
	"context"
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/internal/typedvalue"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

// KeyStore holds, for the time of a run, the new values of transformed keys.
type KeyStore interface {
	// Publish records new values (encoded by typedvalue) under their source values.
	Publish(ctx context.Context, store string, newBySource map[string][]byte) error
	// Lookup returns the new value of each source value, nil when none was published.
	Lookup(ctx context.Context, store string, sources []string) ([][]byte, error)
}

// sourceText is the text a source key value is published and looked up under.
func sourceText(value any) string {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return fmt.Sprint(value)
}

func columnIndex(columns []string, name string) int {
	for i, column := range columns {
		if column == name {
			return i
		}
	}
	return -1
}

// keyPublisher publishes the new values of the referenced columns of a table once the
// rows holding them are written. Source values come from the row observer, in read
// order; it must therefore be the outermost writer, the one no row is dropped before.
type keyPublisher struct {
	ctx     context.Context
	store   KeyStore
	keys    []*tableplan.PublishedKey
	sources map[string][]any // column → source values read and not yet written
	inner   sqlio.RowWriter
}

func (p *keyPublisher) observe(columns []string, row []any) {
	for _, key := range p.keys {
		if idx := columnIndex(columns, key.Column); idx >= 0 {
			p.sources[key.Column] = append(p.sources[key.Column], row[idx])
		}
	}
}

func (p *keyPublisher) WriteBatch(columns []string, rows [][]any) error {
	published := make(map[string]map[string][]byte, len(p.keys))
	for _, key := range p.keys {
		idx := columnIndex(columns, key.Column)
		if idx < 0 || len(p.sources[key.Column]) < len(rows) {
			return fmt.Errorf("runner: clé publiée %q absente des lignes écrites", key.Column)
		}
		pairs := make(map[string][]byte, len(rows))
		for r, row := range rows {
			source := p.sources[key.Column][r]
			if source == nil {
				continue
			}
			encoded, err := typedvalue.Marshal(row[idx])
			if err != nil {
				return fmt.Errorf("runner: clé %q: %w", key.Column, err)
			}
			pairs[sourceText(source)] = encoded
		}
		p.sources[key.Column] = p.sources[key.Column][len(rows):]
		published[key.Store] = pairs
	}
	if err := p.inner.WriteBatch(columns, rows); err != nil {
		return err
	}
	for store, pairs := range published {
		if len(pairs) == 0 {
			continue
		}
		if err := p.store.Publish(p.ctx, store, pairs); err != nil {
			return fmt.Errorf("runner: publication des clés transformées: %w", err)
		}
	}
	return nil
}

// keyTranslator replaces, in the rows about to be written, the foreign key values whose
// parent key is transformed by the new value of that key.
type keyTranslator struct {
	ctx         context.Context
	store       KeyStore
	table       string
	foreignKeys []*tableplan.ForeignKey
	skip        bool
	onDiscard   func(rows int)
	inner       sqlio.RowWriter
}

func (t *keyTranslator) WriteBatch(columns []string, rows [][]any) error {
	for _, fk := range t.foreignKeys {
		var err error
		if rows, err = t.translate(fk, columns, rows); err != nil {
			return err
		}
	}
	return t.inner.WriteBatch(columns, rows)
}

func (t *keyTranslator) translate(fk *tableplan.ForeignKey, columns []string, rows [][]any) ([][]any, error) {
	parentMissing := make([]bool, len(rows))
	for i, column := range fk.Columns {
		store := fk.ParentKeyStores[i]
		if store == "" {
			continue
		}
		idx := columnIndex(columns, column)
		if idx < 0 {
			return nil, fmt.Errorf("runner: colonne de clé étrangère %q absente des lignes écrites dans %s", column, t.table)
		}
		var sources []string
		seen := map[string]int{}
		for _, row := range rows {
			if row[idx] == nil || t.isNoParent(fk, row[idx]) {
				continue
			}
			text := sourceText(row[idx])
			if _, ok := seen[text]; !ok {
				seen[text] = len(sources)
				sources = append(sources, text)
			}
		}
		if len(sources) == 0 {
			continue
		}
		found, err := t.store.Lookup(t.ctx, store, sources)
		if err != nil {
			return nil, fmt.Errorf("runner: lecture des clés transformées de %s.%s: %w", fk.ParentSchema, fk.ParentTable, err)
		}
		for r, row := range rows {
			if row[idx] == nil || t.isNoParent(fk, row[idx]) {
				continue
			}
			encoded := found[seen[sourceText(row[idx])]]
			if encoded == nil {
				parentMissing[r] = true
				continue
			}
			if row[idx], err = typedvalue.Unmarshal(encoded); err != nil {
				return nil, fmt.Errorf("runner: clé transformée de %s.%s: %w", fk.ParentSchema, fk.ParentTable, err)
			}
		}
	}

	kept := rows[:0:0]
	discarded := 0
	for r, row := range rows {
		switch {
		case !parentMissing[r]:
		case !fk.IsMandatory():
			for i, column := range fk.Columns {
				if !fk.NotNull[i] {
					row[columnIndex(columns, column)] = nil
				}
			}
		case t.skip:
			discarded++
			continue
		default:
			return nil, fmt.Errorf("runner: une ligne de %s viole la clé étrangère (%s) vers %s.%s : parent non copié",
				t.table, strings.Join(fk.Columns, ", "), fk.ParentSchema, fk.ParentTable)
		}
		kept = append(kept, row)
	}
	if discarded > 0 {
		t.onDiscard(discarded)
	}
	return kept, nil
}

func (t *keyTranslator) isNoParent(fk *tableplan.ForeignKey, value any) bool {
	return fk.NoParentValue != nil && len(fk.Columns) == 1 && sourceText(value) == *fk.NoParentValue
}

// translatedForeignKeys returns the foreign keys of the plan that follow a transformed
// parent key. A self-reference among them cannot be written in one pass: the new key of a
// parent row read later is not known yet.
func translatedForeignKeys(plan *tableplan.TablePlan) ([]*tableplan.ForeignKey, error) {
	var translated []*tableplan.ForeignKey
	for _, fk := range plan.ForeignKeys {
		follows := false
		for _, store := range fk.ParentKeyStores {
			follows = follows || store != ""
		}
		if !follows {
			continue
		}
		if fk.ParentSchema == plan.Schema && fk.ParentTable == plan.Table {
			return nil, fmt.Errorf("runner: %s.%s : clé auto-référencée (%s) vers une clé transformée, non prise en charge par Athanor",
				plan.Schema, plan.Table, strings.Join(fk.Columns, ", "))
		}
		translated = append(translated, fk)
	}
	return translated, nil
}

var (
	_ sqlio.RowWriter = (*keyPublisher)(nil)
	_ sqlio.RowWriter = (*keyTranslator)(nil)
)
