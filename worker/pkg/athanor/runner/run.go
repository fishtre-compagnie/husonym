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

// RunTable exécute l'anonymisation d'une table via le moteur Athanor :
// SELECT sur la source → plan compilé depuis les mappings → INSERT sur la
// destination, en flux par batches. src et dst peuvent être deux bases
// différentes (prod → staging).
//
// Portée volontairement bornée pour ce premier câblage : table complète, sans
// subsetting (WHERE) ni upsert/onConflict. Ces éléments — déjà gérés par le
// chemin Benthos — seront ajoutés au fur et à mesure de la migration par route.
func RunTable(
	ctx context.Context,
	src Querier,
	dst sqlio.Execer,
	dialect sqlio.Dialect,
	mappings []*mgmtv1alpha1.JobMapping,
	schema, table string,
	batchSize int,
	opts ...te.TransformerExecutorOption,
) error {
	cols, spec, err := SpecForTable(mappings, schema, table, opts...)
	if err != nil {
		return err
	}

	query := buildSelect(dialect, schema, table, cols)
	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("runner: lecture de %s.%s: %w", schema, table, err)
	}
	// rows (*sql.Rows) satisfait sqlio.RowReader ; Pipeline le referme.

	w := sqlio.NewSQLWriter(ctx, dst, dialect, schema, table)
	return sqlio.Pipeline(transform.Ctx{Context: ctx}, rows, batchSize, spec, w)
}

// buildSelect construit le SELECT de lecture, identifiants quotés selon le dialecte.
func buildSelect(d sqlio.Dialect, schema, table string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.QuoteIdent(c)
	}
	ref := d.QuoteIdent(table)
	if schema != "" {
		ref = d.QuoteIdent(schema) + "." + ref
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoted, ", "), ref)
}
