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

	// Upsert (do update) : il faut les colonnes de conflit. Postgres et SQL Server
	// les exigent explicitement ; on les introspecte si le job ne les fournit pas.
	// (MySQL n'en a pas besoin — ON DUPLICATE KEY se déclenche sur toute clé unique.)
	pkColumns := wc.PKColumns
	if wc.OnConflict == sqlio.ConflictDoUpdate && len(pkColumns) == 0 {
		pkColumns, err = primaryKeyColumns(ctx, src, dialect, schema, table)
		if err != nil {
			return fmt.Errorf("runner: introspection des clés primaires de %s.%s: %w", schema, table, err)
		}
	}

	query := buildSelect(dialect, schema, table, cols, where)
	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("runner: lecture de %s.%s: %w", schema, table, err)
	}
	// rows (*sql.Rows) satisfait sqlio.RowReader ; Pipeline le referme.

	w := sqlio.NewSQLWriter(ctx, dst, dialect, schema, table,
		sqlio.WithOnConflict(wc.OnConflict, pkColumns))
	return sqlio.Pipeline(transform.Ctx{Context: ctx}, rows, batchSize, spec, w)
}

// primaryKeyColumns introspecte les colonnes de clé primaire d'une table via
// information_schema — portable sur PostgreSQL, MySQL et SQL Server. La requête
// est paramétrée (placeholders du dialecte). Les colonnes sont renvoyées dans
// l'ordre de la clé. Introspecte la SOURCE : source et destination partageant le
// même schéma (contrainte d'homogénéité actuelle), sa PK vaut pour la destination.
func primaryKeyColumns(ctx context.Context, q Querier, d sqlio.Dialect, schema, table string) ([]string, error) {
	query := fmt.Sprintf(`SELECT kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema = %s AND tc.table_name = %s
ORDER BY kcu.ordinal_position`, d.Placeholder(1), d.Placeholder(2))

	rows, err := q.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
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
