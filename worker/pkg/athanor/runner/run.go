package runner

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
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
	// DisableForeignKeyChecks writes the page with foreign key checks off, so the table
	// is written in one pass whatever the order of its rows. The dialect must allow it.
	DisableForeignKeyChecks bool
	// SkipForeignKeyViolations leaves out the rows whose mandatory parent is missing;
	// without it the first one fails the page.
	SkipForeignKeyViolations bool
}

// Destination is where a table is written: each page in a transaction of its own.
type Destination interface {
	sqlio.Execer
	sqlio.TxBeginner
}

// TablePage is one page of a table sync, as described by its plan.
type TablePage struct {
	Plan      *tableplan.TablePlan
	Mappings  []*mgmtv1alpha1.JobMapping
	BatchSize int
	Write     WriteConfig
	Deriver   *consistency.Deriver
	// AfterOrderValues are the order column values of the last row of the previous
	// page; nil reads the first page.
	AfterOrderValues []any
	Env              *TransformEnv
	// Keys carries transformed keys from the tables holding them to the foreign keys
	// following them; required only when the plan says so.
	Keys KeyStore
}

// PageResult tells what a page read and whether the table has more pages.
type PageResult struct {
	RowsRead int
	// RowsDiscarded counts the rows left out because a mandatory parent was missing.
	RowsDiscarded int
	// LastOrderValues are the order column values of the last source row read, before
	// any transformation: the next page resumes after them.
	LastOrderValues []any
	HasMore         bool
}

// RunTablePage anonymizes one page of a table with the Athanor engine: it reads the
// page described by the plan (subset included), transforms it and writes it to the
// destination, in batches. src and dst may be two different databases (prod → staging).
func RunTablePage(
	ctx context.Context,
	src Querier,
	dst Destination,
	dialect sqlio.Dialect,
	page *TablePage,
) (*PageResult, error) {
	plan := page.Plan
	mapped, spec, err := SpecForTable(ctx, page.Mappings, plan.Schema, plan.Table, page.Deriver, page.Env)
	if err != nil {
		return nil, err
	}

	// Upsert (do update) needs the conflict target columns. Postgres and SQL Server
	// require them explicitly; we introspect them when the job does not supply them.
	// (MySQL does not need them — ON DUPLICATE KEY fires on any unique key.)
	wc := page.Write
	pkColumns := wc.PKColumns
	if wc.OnConflict == sqlio.ConflictDoUpdate && len(pkColumns) == 0 {
		pkColumns, err = primaryKeyColumns(ctx, src, dialect, plan.Schema, plan.Table)
		if err != nil {
			//nolint:misspell // message produit, rédigé en français
			return nil, fmt.Errorf("runner: introspection des clés primaires de %s.%s: %w", plan.Schema, plan.Table, err)
		}
	}

	translated, err := translatedForeignKeys(plan)
	if err != nil {
		return nil, err
	}
	if (len(translated) > 0 || len(plan.PublishedKeys) > 0) && page.Keys == nil {
		return nil, fmt.Errorf("runner: %s.%s suit ou publie des clés transformées, ce qui demande Redis", plan.Schema, plan.Table)
	}

	query, args, err := pageQuery(plan, dialect, page.AfterOrderValues)
	if err != nil {
		return nil, err
	}
	rows, err := src.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("runner: lecture de %s.%s: %w", plan.Schema, plan.Table, err)
	}
	// rows (*sql.Rows) satisfait sqlio.RowReader ; Pipeline le referme.

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("runner: types des colonnes de %s.%s: %w", plan.Schema, plan.Table, err)
	}
	typeNames := make([]string, len(colTypes))
	for i, ct := range colTypes {
		typeNames[i] = ct.DatabaseTypeName()
	}

	result := &PageResult{}
	publisher := &keyPublisher{ctx: ctx, store: page.Keys, keys: plan.PublishedKeys, sources: map[string][]any{}}
	var orderIdx []int
	observe := func(columns []string, row []any) {
		publisher.observe(columns, row)
		if orderIdx == nil {
			orderIdx = columnIndexes(columns, plan.OrderByColumns)
		}
		result.RowsRead++
		if len(orderIdx) == len(plan.OrderByColumns) {
			last := make([]any, len(orderIdx))
			for i, idx := range orderIdx {
				last[i] = row[idx]
			}
			result.LastOrderValues = last
		}
	}

	// The page is written in one transaction: whole or not at all.
	err = sqlio.InTransaction(ctx, dst, dialect, wc.DisableForeignKeyChecks, func(tx sqlio.Tx) error {
		var w sqlio.RowWriter = sqlio.NewSQLWriter(ctx, tx, dialect, plan.Schema, plan.Table,
			sqlio.WithOnConflict(wc.OnConflict, pkColumns))
		// Rows a writer leaves out are counted, and taken out of the keys the table
		// publishes: a key published for a row that was not written would send its
		// children to a parent the destination never received.
		discard := func(dropped []int) {
			result.RowsDiscarded += len(dropped)
			publisher.dropped(dropped)
		}
		w = sqlio.NewParentCheckWriter(ctx, tx, dialect, w, plan.Schema+"."+plan.Table, parentChecks(plan),
			wc.SkipForeignKeyViolations, discard)
		if len(translated) > 0 {
			w = &keyTranslator{
				ctx: ctx, store: page.Keys, table: plan.Schema + "." + plan.Table, foreignKeys: translated,
				skip: wc.SkipForeignKeyViolations, onDiscard: discard, inner: w,
			}
		}
		if len(plan.PublishedKeys) > 0 {
			publisher.inner = w
			w = publisher
		}
		return sqlio.Pipeline(transform.Ctx{Context: ctx}, rows, page.BatchSize, spec, w,
			sqlio.WithNormalizer(sqlio.NormalizerForColumnTypes(columnsOf(colTypes), typeNames)),
			sqlio.WithRowObserver(observe),
			sqlio.WithWrittenColumns(writtenColumns(mapped, plan.GeneratedColumns)),
		)
	})
	if err != nil {
		// Pipeline closes the rows it was given; they are still open when the
		// transaction could not even start.
		_ = rows.Close()
		return nil, err
	}

	if plan.IsPaged() {
		if len(orderIdx) != len(plan.OrderByColumns) && result.RowsRead > 0 {
			return nil, fmt.Errorf("runner: colonnes de tri %v absentes de la lecture de %s.%s",
				plan.OrderByColumns, plan.Schema, plan.Table)
		}
		result.HasMore = result.RowsRead >= plan.PageLimit
	}
	return result, nil
}

// writtenColumns are the mapped columns the destination accepts a value for. Columns
// mapped to their default are not among the mapped ones; generated columns are left to
// the destination whatever their mapping says.
func writtenColumns(mapped, generated []string) []string {
	written := make([]string, 0, len(mapped))
	for _, column := range mapped {
		if !slices.Contains(generated, column) {
			written = append(written, column)
		}
	}
	return written
}

// parentChecks returns the foreign keys to verify when writing: the mandatory ones whose
// parent table the job copies only in part. A mandatory self-reference is not among them:
// its parent rows belong to the page being written.
func parentChecks(plan *tableplan.TablePlan) []sqlio.ParentCheck {
	var checks []sqlio.ParentCheck
	for _, fk := range plan.ForeignKeys {
		selfReference := fk.ParentSchema == plan.Schema && fk.ParentTable == plan.Table
		if !fk.IsMandatory() || !fk.ParentReduced || selfReference {
			continue
		}
		checks = append(checks, sqlio.ParentCheck{
			Columns:       fk.Columns,
			ParentSchema:  fk.ParentSchema,
			ParentTable:   fk.ParentTable,
			ParentColumns: fk.ParentColumns,
			NoParentValue: fk.NoParentValue,
		})
	}
	return checks
}

// pageQuery returns the query reading a page and its arguments. The first page uses
// the plan query as is; the next ones resume after the last order values read, with
// the lexicographic arguments the paged query expects: for n order columns, the i-th
// OR condition takes the first i values, then the page size (first for SQL Server).
func pageQuery(plan *tableplan.TablePlan, dialect sqlio.Dialect, after []any) (query string, args []any, err error) {
	if after == nil {
		return plan.Query, nil, nil
	}
	if !plan.IsPaged() {
		return "", nil, fmt.Errorf("runner: reprise demandée sur %s.%s, dont le plan n'est pas paginé",
			plan.Schema, plan.Table)
	}
	if len(after) != len(plan.OrderByColumns) {
		return "", nil, fmt.Errorf("runner: %d valeurs de reprise pour %d colonnes de tri",
			len(after), len(plan.OrderByColumns))
	}
	_, isMSSQL := dialect.(sqlio.MSSQLDialect)
	if isMSSQL {
		args = append(args, plan.PageLimit)
	}
	for i := range after {
		args = append(args, after[:i+1]...)
	}
	if !isMSSQL {
		args = append(args, plan.PageLimit)
	}
	return plan.PageQuery, args, nil
}

func columnIndexes(columns, wanted []string) []int {
	idx := make([]int, 0, len(wanted))
	for _, w := range wanted {
		for i, c := range columns {
			if c == w {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

func columnsOf(types []*sql.ColumnType) []string {
	names := make([]string, len(types))
	for i, ct := range types {
		names[i] = ct.Name()
	}
	return names
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
