// Package engine is the vectorized execution core of the new engine (RFC §7).
//
// The unit of work is the columnar BATCH: a chunk of rows stored column by column
// and processed in tight loops. It replaces Benthos' row-at-a-time processing. The
// compiler (plan.go) turns a declarative description into an executable plan;
// Execute (engine.go) applies that plan to a batch.
//
// Scope of this increment (Movement 3, core): columns as []any, because today's
// transformers operate on `any` (M1). Typed columns plus native vectorized kernels
// — the extra gain measured during the spike — will come with native transformers,
// without changing this contract.
package engine

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// Batch : un lot de lignes en représentation colonnaire.
type Batch struct {
	Names []string         // ordre des colonnes (stable, pour l'itération)
	Cols  map[string][]any // colonne -> valeurs (toutes de longueur N)
	N     int              // nombre de lignes
}

// NewBatch construit un batch vide de capacité n pour les colonnes données.
func NewBatch(names []string, n int) *Batch {
	cols := make(map[string][]any, len(names))
	for _, name := range names {
		cols[name] = make([]any, n)
	}
	return &Batch{Names: names, Cols: cols, N: n}
}

// Row renvoie une vue transform.Row sur la i-ème ligne du batch (sans copie).
func (b *Batch) Row(i int) transform.Row { return batchRow{b: b, i: i} }

// batchRow adapte une ligne d'un Batch à l'interface transform.Row.
type batchRow struct {
	b *Batch
	i int
}

func (r batchRow) Get(col string) (any, bool) {
	c, ok := r.b.Cols[col]
	if !ok {
		return nil, false
	}
	return c[r.i], true
}

func (r batchRow) Set(col string, v any) error {
	c, ok := r.b.Cols[col]
	if !ok {
		return fmt.Errorf("engine: colonne inconnue à l'écriture: %q", col)
	}
	c[r.i] = v
	return nil
}

func (r batchRow) Str(col string) string {
	v, _ := r.Get(col)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

var _ transform.Row = batchRow{}
