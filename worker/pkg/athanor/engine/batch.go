// Package engine est le cœur d'exécution vectorisé du nouveau moteur (RFC §7).
//
// L'unité de travail est le BATCH colonnaire : un lot de lignes stocké par
// colonnes, traité en boucles serrées. C'est le remplaçant du traitement
// ligne-à-ligne de Benthos. Le compilateur (plan.go) transforme une description
// déclarative en plan exécutable ; Execute (engine.go) applique ce plan à un batch.
//
// Périmètre de cet incrément (Mouvement 3, cœur) : colonnes en []any, car les
// transformers actuels opèrent sur `any` (M1). Les colonnes typées + kernels
// vectorisés natifs (le gain supplémentaire mesuré au spike) viendront pour les
// transformers natifs — sans changer ce contrat.
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
