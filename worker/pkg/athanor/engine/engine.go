package engine

// engine.go — l'exécution proprement dite. Execute applique un plan compilé à un
// batch : d'abord les value bindings colonne par colonne (boucle vectorisée),
// puis les RowTransformers dans l'ordre topologique décidé à la compilation.
// Run enchaîne les batches d'une source en flux (mémoire bornée par le batch).

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// Execute applique le plan au batch, en place.
func (p *Plan) Execute(ctx transform.Ctx, b *Batch) error {
	// 1) Value bindings : une boucle serrée par colonne. C'est le point où la
	//    vectorisation par colonnes remplace le ligne-à-ligne : un transformer,
	//    une colonne entière, sans détour par un système de messages dynamiques.
	for _, vb := range p.values {
		col := b.Cols[vb.Column]
		for i := 0; i < b.N; i++ {
			out, err := vb.T.TransformValue(ctx, col[i])
			if err != nil {
				return fmt.Errorf("engine: colonne %q ligne %d: %w", vb.Column, i, err)
			}
			col[i] = out
		}
	}

	// 2) Row transforms : multi-colonnes, ligne par ligne (inhérent à la portée),
	//    dans l'ordre garanti par le graphe de dépendances.
	for _, rt := range p.rows {
		for i := 0; i < b.N; i++ {
			if err := rt.TransformRow(ctx, b.Row(i)); err != nil {
				return fmt.Errorf("engine: transformer ligne %T, ligne %d: %w", rt, i, err)
			}
		}
	}
	return nil
}

// BatchSource fournit des batches en flux. Renvoie (batch, true) tant qu'il reste
// des données, puis (nil, false). C'est le contrat minimal du streaming : le
// moteur ne garde qu'un batch à la fois en mémoire (RFC §7.3).
type BatchSource func() (*Batch, bool)

// Sink consomme un batch transformé (écriture destination, collecte de preview…).
type Sink func(*Batch) error

// Run exécute le plan sur tous les batches de la source, en flux. La mémoire
// reste bornée par la taille d'un batch, quel que soit le volume total.
func Run(ctx transform.Ctx, src BatchSource, p *Plan, out Sink) error {
	for {
		b, ok := src()
		if !ok {
			return nil
		}
		if err := p.Execute(ctx, b); err != nil {
			return err
		}
		if out != nil {
			if err := out(b); err != nil {
				return fmt.Errorf("engine: sink: %w", err)
			}
		}
	}
}
