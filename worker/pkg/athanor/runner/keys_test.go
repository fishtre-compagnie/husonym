package runner

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/stretchr/testify/require"
)

type memoryKeyStore map[string]map[string][]byte

func (m memoryKeyStore) Publish(_ context.Context, store string, newBySource map[string][]byte) error {
	if m[store] == nil {
		m[store] = map[string][]byte{}
	}
	for source, encoded := range newBySource {
		m[store][source] = encoded
	}
	return nil
}

func (m memoryKeyStore) Lookup(_ context.Context, store string, sources []string) ([][]byte, error) {
	found := make([][]byte, len(sources))
	for i, source := range sources {
		found[i] = m[store][source]
	}
	return found, nil
}

type capturingWriter struct{ rows [][]any }

func (w *capturingWriter) WriteBatch(_ []string, rows [][]any) error {
	w.rows = append(w.rows, rows...)
	return nil
}

func clientKey(notNull bool) *tableplan.ForeignKey {
	return &tableplan.ForeignKey{
		Columns: []string{"client_id"}, NotNull: []bool{notNull},
		ParentSchema: "shop", ParentTable: "CLIENT", ParentColumns: []string{"id"},
		ParentKeyStores: []string{"store-client-id"},
	}
}

// The parent table publishes each new key under the source one, read before the
// transformers ran; the child table, synced after it, follows.
func Test_keysArePublishedThenFollowed(t *testing.T) {
	ctx := context.Background()
	store := memoryKeyStore{}

	parent := &capturingWriter{}
	publisher := &keyPublisher{
		ctx: ctx, store: store, sources: map[string][]any{}, inner: parent,
		keys: []*tableplan.PublishedKey{{Column: "id", Store: "store-client-id"}},
	}
	columns := []string{"id", "reference"}
	publisher.observe(columns, []any{int64(1), "CL-1"})
	publisher.observe(columns, []any{int64(2), "CL-2"})
	require.NoError(t, publisher.WriteBatch(columns, [][]any{{int64(1000001), "CL-1"}, {int64(1000002), "CL-2"}}))
	require.Len(t, parent.rows, 2)

	child := &capturingWriter{}
	discarded := 0
	translator := &keyTranslator{
		ctx: ctx, store: store, table: "shop.COMMANDE", inner: child, skip: true,
		onDiscard:   func(dropped []int) { discarded += len(dropped) },
		foreignKeys: []*tableplan.ForeignKey{clientKey(true)},
	}
	require.NoError(t, translator.WriteBatch([]string{"id", "client_id"},
		[][]any{{int64(10), int64(2)}, {int64(11), int64(1)}, {int64(12), int64(99)}}))
	// Client 99 was never published: its row was not copied, the order cannot be written.
	require.Equal(t, [][]any{{int64(10), int64(1000002)}, {int64(11), int64(1000001)}}, child.rows)
	require.Equal(t, 1, discarded)
}

func Test_keyTranslator_UnpublishedParent(t *testing.T) {
	ctx := context.Background()
	rows := func() [][]any { return [][]any{{int64(10), int64(99)}, {int64(11), nil}} }

	nullable := &capturingWriter{}
	require.NoError(t, (&keyTranslator{
		ctx: ctx, store: memoryKeyStore{}, table: "shop.COMMANDE", inner: nullable,
		foreignKeys: []*tableplan.ForeignKey{clientKey(false)},
	}).WriteBatch([]string{"id", "client_id"}, rows()))
	require.Equal(t, [][]any{{int64(10), nil}, {int64(11), nil}}, nullable.rows, "a nullable key is cleared")

	err := (&keyTranslator{
		ctx: ctx, store: memoryKeyStore{}, table: "shop.COMMANDE", inner: &capturingWriter{},
		foreignKeys: []*tableplan.ForeignKey{clientKey(true)},
	}).WriteBatch([]string{"id", "client_id"}, [][]any{{int64(10), int64(99)}})
	require.ErrorContains(t, err, "parent non copié", "without skip a mandatory key fails the page")
}

func Test_translatedForeignKeys_SelfReference(t *testing.T) {
	plan := &tableplan.TablePlan{Schema: "shop", Table: "CLIENT", ForeignKeys: []*tableplan.ForeignKey{{
		Columns: []string{"parrain_id"}, NotNull: []bool{false},
		ParentSchema: "shop", ParentTable: "CLIENT", ParentColumns: []string{"id"},
		ParentKeyStores: []string{"store-client-id"},
	}}}
	_, err := translatedForeignKeys(plan)
	require.ErrorContains(t, err, "auto-référencée")

	plan.ForeignKeys[0].ParentKeyStores = []string{""}
	translated, err := translatedForeignKeys(plan)
	require.NoError(t, err)
	require.Empty(t, translated)
}

// droppingWriter leaves out the rows at the given indexes of the batch it receives, the
// way the parent check and the key translator do, and says which ones.
type droppingWriter struct {
	drop      []int
	onDiscard func(dropped []int)
	inner     *capturingWriter
}

func (w *droppingWriter) WriteBatch(columns []string, rows [][]any) error {
	kept := rows[:0:0]
	for r, row := range rows {
		if slices.Contains(w.drop, r) {
			continue
		}
		kept = append(kept, row)
	}
	w.onDiscard(w.drop)
	return w.inner.WriteBatch(columns, kept)
}

// A row the writers under the publisher leave out has no key published: a child following
// it would otherwise reference a parent row the destination never received.
func Test_keysOfDiscardedRowsAreNotPublished(t *testing.T) {
	ctx := context.Background()
	store := memoryKeyStore{}

	written := &capturingWriter{}
	publisher := &keyPublisher{
		ctx: ctx, store: store, sources: map[string][]any{},
		keys: []*tableplan.PublishedKey{{Column: "id", Store: "store-client-id"}},
	}
	// The second row of the batch is the one whose mandatory parent is missing.
	publisher.inner = &droppingWriter{drop: []int{1}, onDiscard: publisher.dropped, inner: written}

	columns := []string{"id", "reference"}
	for _, source := range [][]any{{int64(1), "CL-1"}, {int64(2), "CL-2"}, {int64(3), "CL-3"}} {
		publisher.observe(columns, source)
	}
	require.NoError(t, publisher.WriteBatch(columns, [][]any{
		{int64(1000001), "CL-1"}, {int64(1000002), "CL-2"}, {int64(1000003), "CL-3"},
	}))

	require.Len(t, written.rows, 2)
	require.ElementsMatch(t, []string{"1", "3"}, slices.Sorted(maps.Keys(store["store-client-id"])),
		"only the keys of the rows that were written are published")
}
