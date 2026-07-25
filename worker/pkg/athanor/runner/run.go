package runner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/sqlio"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
	te "github.com/Groupe-Hevea/neosync/worker/pkg/benthos/transformer_executor"
)

// Querier est la source de lecture. *database/sql.DB, *sql.Tx et le
// neosync_benthos_sql.SqlDbtx du worker le satisfont tous (QueryContext).
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// WriteConfig porte la politique d'écriture en destination : gestion des conflits
// de clé (do nothing / upsert). Sa valeur zéro = INSERT simple (comportement
// historique). PKColumns n'est requis que pour l'upsert Postgres.
type WriteConfig struct {
	OnConflict sqlio.ConflictAction
	PKColumns  []string
}

// RunTable exécute l'anonymisation d'une table via le moteur Athanor :
// SELECT sur la source → plan compilé depuis les mappings → INSERT sur la
// destination, en flux par batches. src et dst peuvent être deux bases
// différentes (prod → staging).
//
// Le subsetting est géré via `where` (clause SQL sans le mot-clé WHERE) et la
// gestion des conflits de clé via `wc` (do nothing / upsert). Restent à porter,
// par rapport au chemin Benthos : destinations multiples, SGBD hétérogènes
// source/destination, et l'upsert Postgres (nécessite les colonnes PK).
func RunTable(
	ctx context.Context,
	src Querier,
	dst sqlio.Execer,
	dialect sqlio.Dialect,
	mappings []*mgmtv1alpha1.JobMapping,
	schema, table, where string,
	batchSize int,
	wc WriteConfig,
	opts ...te.TransformerExecutorOption,
) error {
	cols, spec, err := SpecForTable(mappings, schema, table, opts...)
	if err != nil {
		return err
	}

	query := buildSelect(dialect, schema, table, cols, where)
	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("runner: lecture de %s.%s: %w", schema, table, err)
	}
	// rows (*sql.Rows) satisfait sqlio.RowReader ; Pipeline le referme.

	w := sqlio.NewSQLWriter(ctx, dst, dialect, schema, table,
		sqlio.WithOnConflict(wc.OnConflict, wc.PKColumns))
	return sqlio.Pipeline(transform.Ctx{Context: ctx}, rows, batchSize, spec, w)
}

// buildSelect construit le SELECT de lecture, identifiants quotés selon le
// dialecte. where est la clause de subsetting (sans le mot-clé WHERE) ; vide = pas
// de filtre. Elle n'est PAS paramétrée : elle provient de la config du job et doit
// être valide telle quelle (contrat Neosync, cf. proto where_clause).
func buildSelect(d sqlio.Dialect, schema, table string, cols []string, where string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.QuoteIdent(c)
	}
	ref := d.QuoteIdent(table)
	if schema != "" {
		ref = d.QuoteIdent(schema) + "." + ref
	}
	q := fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoted, ", "), ref)
	if strings.TrimSpace(where) != "" {
		q += " WHERE " + where
	}
	return q
}
