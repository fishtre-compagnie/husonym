// Package sqlio branche une source/destination SQL sur le moteur vectorisé
// (RFC §7 — l'I/O par batches). C'est la première pièce du Mouvement 3b : le
// moteur, jusqu'ici alimenté par des batches fabriqués en test, lit désormais de
// vraies lignes et écrit de vrais résultats — en flux, mémoire bornée par batch.
//
// Volontairement DRIVER-AGNOSTIQUE : RowReader est un sous-ensemble des méthodes
// de *database/sql.Rows, donc un *sql.Rows réel (PostgreSQL, MySQL…) le satisfait
// directement, et un faux le satisfait aussi pour les tests — sans base réelle.
//
// Cette pièce ne touche PAS encore l'activité Temporal ni GenerateBenthosConfigs :
// c'est la plomberie isolée, à éprouver avant tout câblage sur le chemin de prod.
package sqlio

import (
	"fmt"

	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/engine"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
)

// RowReader abstrait une source de lignes. *database/sql.Rows le satisfait tel
// quel (mêmes signatures), ce qui rend le branchement sur un vrai SGBD trivial.
type RowReader interface {
	Columns() ([]string, error)
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// RowWriter reçoit les lignes transformées, par lots (INSERT groupé, COPY…).
type RowWriter interface {
	WriteBatch(columns []string, rows [][]any) error
}

// Pipeline lit la source par batches, compile la Spec contre le schéma réel de
// la source, exécute le plan sur chaque batch, puis écrit le résultat. Erreurs
// remontées fidèlement (contrairement au streaming en mémoire pure).
func Pipeline(
	ctx transform.Ctx,
	r RowReader,
	batchSize int,
	spec engine.Spec,
	w RowWriter,
) (err error) {
	if batchSize <= 0 {
		return fmt.Errorf("sqlio: batchSize doit être > 0")
	}
	defer func() {
		if cerr := r.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("sqlio: fermeture de la source: %w", cerr)
		}
	}()

	cols, err := r.Columns()
	if err != nil {
		return fmt.Errorf("sqlio: lecture des colonnes: %w", err)
	}

	// Le schéma réel de la source valide la Spec : un plan qui compile s'exécute.
	plan, err := engine.Compile(cols, spec)
	if err != nil {
		return err
	}

	n := len(cols)
	for {
		// Remplir un batch : jusqu'à batchSize lignes scannées en []any.
		data := make([][]any, 0, batchSize)
		for len(data) < batchSize && r.Next() {
			vals := make([]any, n)
			ptrs := make([]any, n)
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if serr := r.Scan(ptrs...); serr != nil {
				return fmt.Errorf("sqlio: scan d'une ligne: %w", serr)
			}
			data = append(data, vals)
		}
		if rerr := r.Err(); rerr != nil {
			return fmt.Errorf("sqlio: itération de la source: %w", rerr)
		}
		if len(data) == 0 {
			return nil // source épuisée
		}

		// Vue colonnaire du batch.
		b := engine.NewBatch(cols, len(data))
		for i, name := range cols {
			col := b.Cols[name]
			for j := range data {
				col[j] = data[j][i]
			}
		}

		if xerr := plan.Execute(ctx, b); xerr != nil {
			return xerr
		}

		if werr := w.WriteBatch(cols, batchToRows(b)); werr != nil {
			return fmt.Errorf("sqlio: écriture d'un batch: %w", werr)
		}
	}
}

// batchToRows reconvertit un batch colonnaire en lignes, pour les destinations
// orientées lignes (INSERT/COPY).
func batchToRows(b *engine.Batch) [][]any {
	rows := make([][]any, b.N)
	for j := 0; j < b.N; j++ {
		row := make([]any, len(b.Names))
		for i, name := range b.Names {
			row[i] = b.Cols[name][j]
		}
		rows[j] = row
	}
	return rows
}

// NOTE (suite M3b) : le scan en *any renvoie, selon le driver, du []byte pour
// certains types texte. Une couche de correspondance type SGBD ↔ type Go
// (par colonne, depuis le Catalog) sera nécessaire avant le câblage prod.
