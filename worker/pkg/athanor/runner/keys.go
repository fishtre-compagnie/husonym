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
	"slices"
	"strings"

	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
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
//
// A row the writers under it leave out is not published: its key would send the tables
// referencing it to a parent row the destination never received.
type keyPublisher struct {
	ctx     context.Context
	store   KeyStore
	keys    []*tableplan.PublishedKey
	sources map[string][]any // column → source values read and not yet written
	inner   sqlio.RowWriter
	// surviving holds, while a batch is being written, the indexes of the rows no writer
	// has left out yet.
	surviving []int
}

// dropped takes out of the surviving rows the ones a writer left out. Their indexes are
// those of the batch that writer received, which is what is left of the batch at that
// point of the chain; they come in ascending order.
func (p *keyPublisher) dropped(dropped []int) {
	if p.surviving == nil || len(dropped) == 0 {
		return
	}
	kept := p.surviving[:0:0]
	next := 0
	for i, row := range p.surviving {
		if next < len(dropped) && dropped[next] == i {
			next++
			continue
		}
		kept = append(kept, row)
	}
	p.surviving = kept
}

func (p *keyPublisher) observe(columns []string, row []any) {
	for _, key := range p.keys {
		if idx := columnIndex(columns, key.Column); idx >= 0 {
			p.sources[key.Column] = append(p.sources[key.Column], row[idx])
		}
	}
}

func (p *keyPublisher) WriteBatch(columns []string, rows [][]any) error {
	// The new value of each row, kept per row: which of them is published is only known
	// once the writers under this one have had their say.
	byRow := make(map[string][]*publishedValue, len(p.keys))
	for _, key := range p.keys {
		idx := columnIndex(columns, key.Column)
		if idx < 0 || len(p.sources[key.Column]) < len(rows) {
			return fmt.Errorf("runner: clé publiée %q absente des lignes écrites", key.Column)
		}
		values := make([]*publishedValue, len(rows))
		for r, row := range rows {
			source := p.sources[key.Column][r]
			if source == nil {
				continue
			}
			encoded, err := typedvalue.Marshal(row[idx])
			if err != nil {
				return fmt.Errorf("runner: clé %q: %w", key.Column, err)
			}
			values[r] = &publishedValue{source: sourceText(source), encoded: encoded}
		}
		p.sources[key.Column] = p.sources[key.Column][len(rows):]
		byRow[key.Store] = values
	}

	p.surviving = make([]int, len(rows))
	for i := range p.surviving {
		p.surviving[i] = i
	}
	defer func() { p.surviving = nil }()
	if err := p.inner.WriteBatch(columns, rows); err != nil {
		return err
	}

	for store, values := range byRow {
		pairs := make(map[string][]byte, len(p.surviving))
		for _, r := range p.surviving {
			if value := values[r]; value != nil {
				pairs[value.source] = value.encoded
			}
		}
		if len(pairs) == 0 {
			continue
		}
		if err := p.store.Publish(p.ctx, store, pairs); err != nil {
			return fmt.Errorf("runner: publication des clés transformées: %w", err)
		}
	}
	return nil
}

// publishedValue is the new value of a published key, under the source value it replaces.
type publishedValue struct {
	source  string
	encoded []byte
}

// keyTranslator replaces, in the rows about to be written, the foreign key values whose
// parent key is transformed by the new value of that key.
type keyTranslator struct {
	ctx         context.Context
	store       KeyStore
	table       string
	foreignKeys []*tableplan.ForeignKey
	// deferred are written NULL: an update pass fills them in (see followingForeignKeys).
	deferred  []*tableplan.ForeignKey
	onDiscard func(dropped []int)
	inner     sqlio.RowWriter
}

func (t *keyTranslator) WriteBatch(columns []string, rows [][]any) error {
	for _, fk := range t.deferred {
		for _, column := range fk.Columns {
			idx := columnIndex(columns, column)
			if idx < 0 {
				return fmt.Errorf("runner: colonne de clé étrangère %q absente des lignes écrites dans %s", column, t.table)
			}
			for _, row := range rows {
				row[idx] = nil
			}
		}
	}
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
	var dropped []int
	for r, row := range rows {
		switch {
		case !parentMissing[r]:
		case !fk.IsMandatory():
			for i, column := range fk.Columns {
				if !fk.NotNull[i] {
					row[columnIndex(columns, column)] = nil
				}
			}
		default:
			// Le parent n'a pas été copié : la clé est obligatoire, donc la ligne ne peut
			// pas être écrite. Elle est écartée plutôt que de faire échouer la page —
			// voir sqlio/parent_check.go, qui porte le même raisonnement.
			dropped = append(dropped, r)
			continue
		}
		kept = append(kept, row)
	}
	if len(dropped) > 0 {
		t.onDiscard(dropped)
	}
	return kept, nil
}

func (t *keyTranslator) isNoParent(fk *tableplan.ForeignKey, value any) bool {
	return fk.NoParentValue != nil && len(fk.Columns) == 1 && sourceText(value) == *fk.NoParentValue
}

// followingForeignKeys returns the foreign keys of the plan that follow a transformed parent
// key, split by the pass that writes them.
//
// translated are written by this pass, through the new keys already published. deferred
// are the ones an insert pass leaves to an update pass: in a circular dependency or a
// self-reference, the parent row may be written later, and its new key is not known yet.
// The insert pass writes them NULL; the update pass, which runs once the parent table is
// written, fills them in.
func followingForeignKeys(plan *tableplan.TablePlan) (translated, deferred []*tableplan.ForeignKey, err error) {
	for _, fk := range plan.ForeignKeys {
		if !followsTransformedKey(fk) {
			continue
		}
		written := true
		for _, column := range fk.Columns {
			written = written && slices.Contains(plan.Columns, column)
		}
		switch {
		case written && plan.RunType == runconfigs.RunTypeInsert && fk.ParentSchema == plan.Schema && fk.ParentTable == plan.Table:
			// Only a key refusing NULL is written by the insert pass of its own table.
			return nil, nil, fmt.Errorf("runner: %s.%s : clé auto-référencée NOT NULL (%s) vers une clé transformée : "+
				"aucune passe ne peut connaître la nouvelle clé d'un parent écrit plus tard",
				plan.Schema, plan.Table, strings.Join(fk.Columns, ", "))
		case written:
			translated = append(translated, fk)
		case plan.RunType == runconfigs.RunTypeInsert:
			if fk.IsMandatory() || slices.Contains(fk.NotNull, true) {
				return nil, nil, fmt.Errorf("runner: %s.%s : clé étrangère (%s) vers une clé transformée, en partie NOT NULL, "+
					"dans un cycle : aucune passe ne peut connaître la nouvelle clé d'un parent écrit plus tard",
					plan.Schema, plan.Table, strings.Join(fk.Columns, ", "))
			}
			deferred = append(deferred, fk)
		}
	}
	return translated, deferred, nil
}

// UpdateFollowsTransformedKey reports whether an update pass writes a foreign key following
// a transformed key: the only update pass Athanor has to run, since it writes every other
// column in the insert pass, foreign keys suspended.
func UpdateFollowsTransformedKey(plan *tableplan.TablePlan) bool {
	translated, _, err := followingForeignKeys(plan)
	return err == nil && plan.RunType == runconfigs.RunTypeUpdate && len(translated) > 0
}

func followsTransformedKey(fk *tableplan.ForeignKey) bool {
	for _, store := range fk.ParentKeyStores {
		if store != "" {
			return true
		}
	}
	return false
}

// ownKey describes the key of the table as a foreign key to itself, whose stores are the
// ones the table publishes its own new keys in: translating it gives the key a row holds in
// the destination. A row whose key was never published was not written.
func ownKey(plan *tableplan.TablePlan) *tableplan.ForeignKey {
	key := &tableplan.ForeignKey{
		Columns: plan.PrimaryKey, ParentSchema: plan.Schema, ParentTable: plan.Table, ParentColumns: plan.PrimaryKey,
	}
	for _, column := range plan.PrimaryKey {
		store := ""
		for _, published := range plan.PublishedKeys {
			if published.Column == column {
				store = published.Store
			}
		}
		key.NotNull = append(key.NotNull, true)
		key.ParentKeyStores = append(key.ParentKeyStores, store)
	}
	return key
}

var (
	_ sqlio.RowWriter = (*keyPublisher)(nil)
	_ sqlio.RowWriter = (*keyTranslator)(nil)
)
