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
	"slices"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
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

// Option configure le Pipeline.
type Option func(*config)

type config struct {
	norm     *Normalizer
	observer func(columns []string, row []any)
	written  []string
}

// WithWrittenColumns restricts what reaches the writer to these columns, in this order.
// The others are still read and handed to the transformers, which may need them, but the
// destination computes them itself: generated columns, columns left to their default.
func WithWrittenColumns(columns []string) Option {
	return func(c *config) { c.written = columns }
}

// WithNormalizer surcharge le normaliseur de types (par défaut : mode Auto pour
// toutes les colonnes, ce qui suffit dans la plupart des cas).
func WithNormalizer(n *Normalizer) Option {
	return func(c *config) { c.norm = n }
}

// WithRowObserver is called with every source row, normalized but before any
// transformation. The row must not be modified.
func WithRowObserver(fn func(columns []string, row []any)) Option {
	return func(c *config) { c.observer = fn }
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
	opts ...Option,
) (err error) {
	if batchSize <= 0 {
		return fmt.Errorf("sqlio: batchSize doit être > 0")
	}
	cfg := config{norm: NewNormalizer()} // normalisation Auto par défaut
	for _, o := range opts {
		o(&cfg)
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
	written := cols
	if cfg.written != nil {
		for _, name := range cfg.written {
			if !slices.Contains(cols, name) {
				return fmt.Errorf("sqlio: colonne à écrire %q absente de la lecture", name)
			}
		}
		written = cfg.written
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
			// Normalisation vers types Go canoniques : indispensable pour que la
			// même valeur logique produise la même graine de cohérence quel que
			// soit le driver source (ex. []byte MySQL -> string).
			for i := range vals {
				nv, nerr := cfg.norm.Normalize(cols[i], vals[i])
				if nerr != nil {
					return fmt.Errorf("sqlio: normalisation colonne %q: %w", cols[i], nerr)
				}
				vals[i] = nv
			}
			if cfg.observer != nil {
				cfg.observer(cols, vals)
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

		if werr := w.WriteBatch(written, batchToRows(b, written)); werr != nil {
			return fmt.Errorf("sqlio: écriture d'un batch: %w", werr)
		}
	}
}

// batchToRows reconvertit les colonnes demandées d'un batch colonnaire en lignes, pour les
// destinations orientées lignes (INSERT/COPY).
func batchToRows(b *engine.Batch, columns []string) [][]any {
	rows := make([][]any, b.N)
	for j := 0; j < b.N; j++ {
		row := make([]any, len(columns))
		for i, name := range columns {
			row[i] = b.Cols[name][j]
		}
		rows[j] = row
	}
	return rows
}
