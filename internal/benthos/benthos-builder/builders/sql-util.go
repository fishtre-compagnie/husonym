package benthosbuilder_builders

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	rc "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"golang.org/x/sync/errgroup"
)

const (
	jobmappingSubsetErrMsg     = "unable to continue: job mappings contain schemas, tables, or columns that were not found in the source connection"
	haltOnSchemaAdditionErrMsg = "unable to continue: HaltOnNewColumnAddition: job mappings are missing columns for the mapped tables found in the source connection"
	haltOnColumnRemovalErrMsg  = "unable to continue: HaltOnColumnRemoval: source database is missing columns for the mapped tables found in the job mappings"
)

type sqlSourceTableOptions struct {
	WhereClause *string
}

type tableMapping struct {
	Schema   string
	Table    string
	Mappings []*mgmtv1alpha1.JobMapping
}

var errSourceShowsNoMappedColumn = errors.New(
	"the source shows none of the columns the job maps: check that the connection points to the right database and can read its tables",
)

// checkSourceShowsTheJob refuses a source in which none of the job's mapped columns remain. That
// is not a source that lost columns but the wrong database, or a user without the rights to see
// its tables: removing the mappings of the missing columns would empty the job.
func checkSourceShowsTheJob(mappings, found []*mgmtv1alpha1.JobMapping) error {
	if len(mappings) > 0 && len(found) == 0 {
		return errSourceShowsNoMappedColumn
	}
	return nil
}

// removeMappingsNotFoundInSource splits the mappings between those whose column the source still
// has and those whose column it no longer has.
func removeMappingsNotFoundInSource(
	mappings []*mgmtv1alpha1.JobMapping,
	groupedSchemas map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
) (kept, removed []*mgmtv1alpha1.JobMapping) {
	kept = make([]*mgmtv1alpha1.JobMapping, 0, len(mappings))
	for _, mapping := range mappings {
		key := sqlmanager_shared.BuildTable(mapping.Schema, mapping.Table)
		if _, ok := groupedSchemas[key][mapping.Column]; ok {
			kept = append(kept, mapping)
			continue
		}
		removed = append(removed, mapping)
	}
	return kept, removed
}

// checks that the source database has all the columns that are mapped in the job mappings
func isSourceMissingColumnsFoundInMappings(
	groupedSchemas map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	mappings []*mgmtv1alpha1.JobMapping,
) ([]string, bool) {
	missingColumns := []string{}
	tableColMappings := getUniqueColMappingsMap(mappings)

	for schemaTable, cols := range tableColMappings {
		tableCols := groupedSchemas[schemaTable]
		for col := range cols {
			if _, ok := tableCols[col]; !ok {
				missingColumns = append(missingColumns, fmt.Sprintf("%s.%s", schemaTable, col))
			}
		}
	}
	return missingColumns, len(missingColumns) != 0
}

// Builds a map of <schema.table>->column
func getUniqueColMappingsMap(
	mappings []*mgmtv1alpha1.JobMapping,
) map[string]map[string]struct{} {
	tableColMappings := map[string]map[string]struct{}{}
	for _, mapping := range mappings {
		key := husonym_benthos.BuildBenthosTable(mapping.Schema, mapping.Table)
		if _, ok := tableColMappings[key]; ok {
			tableColMappings[key][mapping.Column] = struct{}{}
		} else {
			tableColMappings[key] = map[string]struct{}{
				mapping.Column: {},
			}
		}
	}
	return tableColMappings
}

// Based on the source schema, we check each mapped table for newly added columns that are not present in the mappings,
// but are present in the source. If so, halt because this means PII may be leaked.
func shouldHaltOnSchemaAddition(
	groupedSchemas map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	mappings []*mgmtv1alpha1.JobMapping,
) ([]string, bool) {
	tableColMappings := getUniqueColMappingsMap(mappings)
	newColumns := []string{}
	for table, cols := range groupedSchemas {
		mappingCols, exists := tableColMappings[table]
		if !exists {
			// table not mapped in job mappings, skip
			continue
		}
		for col := range cols {
			if _, exists := mappingCols[col]; !exists {
				newColumns = append(newColumns, fmt.Sprintf("%s.%s", table, col))
			}
		}
	}
	return newColumns, len(newColumns) != 0
}

// formatMappingColumns names mappings the way shouldHaltOnSchemaAddition names new columns, so
// the two messages about the same columns read alike. Sorted, because the mappings are built by
// walking maps: without it the same run logs the same columns in a different order every time,
// and nobody can diff two runs.
func formatMappingColumns(mappings []*mgmtv1alpha1.JobMapping) []string {
	columns := make([]string, 0, len(mappings))
	for _, m := range mappings {
		columns = append(
			columns,
			fmt.Sprintf("%s.%s", sqlmanager_shared.BuildTable(m.GetSchema(), m.GetTable()), m.GetColumn()),
		)
	}
	slices.Sort(columns)
	return columns
}

// autoMapNewColumns (AutoMap & Review) replaces, among the mappings added for new columns, each
// passthrough by the transformer the PII detection suggests, in the config it starts with in the
// catalogue — the mapping the UI would propose for the column.
//
// The passthrough stays when nothing is suggested, and when the column carries a primary key, a
// foreign key or a unique constraint, or is referenced by one: a transformer there could break
// the constraint and fail the run, while the strategy never stops one. Choosing a transformer
// that keeps a constraint is another matter. Generated columns keep their GenerateDefault.
//
// An option the run could not honor is turned off rather than written to the job: see
// withinReach.
//
// It returns the mappings, and the names of the columns it anonymized and of those it left in
// passthrough, for the run's log.
func autoMapNewColumns(
	mappings []*mgmtv1alpha1.JobMapping,
	columnInfo map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	constraints *sqlmanager_shared.TableConstraints,
	hasConsistencyKey bool,
) (out []*mgmtv1alpha1.JobMapping, anonymized, passedThrough []string) {
	constrained := constrainedColumns(constraints)
	out = make([]*mgmtv1alpha1.JobMapping, 0, len(mappings))
	for _, m := range mappings {
		if m.GetTransformer().GetConfig().GetPassthroughConfig() == nil {
			out = append(out, m)
			continue
		}
		table := sqlmanager_shared.BuildTable(m.GetSchema(), m.GetTable())
		name := fmt.Sprintf("%s.%s", table, m.GetColumn())

		var dataType string
		if info := columnInfo[table][m.GetColumn()]; info != nil {
			dataType = info.DataType
		}
		source, category, suggested := job_util.SuggestedTransformer(m.GetColumn(), dataType)
		if _, ok := constrained[table][m.GetColumn()]; ok || !suggested {
			out = append(out, m)
			passedThrough = append(passedThrough, name)
			continue
		}
		// The base catalogue: a run has no license to check, and the suggestions are base
		// transformers anyway.
		config, ok := catalog.DefaultConfig(source, false)
		if !ok {
			out = append(out, m)
			passedThrough = append(passedThrough, name)
			continue
		}
		out = append(out, &mgmtv1alpha1.JobMapping{
			Schema:      m.GetSchema(),
			Table:       m.GetTable(),
			Column:      m.GetColumn(),
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: withinReach(config, hasConsistencyKey)},
		})
		anonymized = append(anonymized, fmt.Sprintf("%s (%s)", name, category))
	}
	slices.Sort(anonymized)
	slices.Sort(passedThrough)
	return out, anonymized, passedThrough
}

// withinReach turns off the options of a config that need something the deployment does not
// have. It is the strategy's own rule applied to the options: a mapping written to the job is a
// mapping the runs after this one will keep, so it must be one they can carry out.
//
// TransformPhoneNumber keeps the format of a number by permuting its digits under a derived key
// (preserve_format), which the catalogue turns on by default. Without a key the run would stop
// on the very column AutoMap had just mapped, and every run after it, until somebody edited the
// mapping by hand. The number is anonymized without keeping its format instead — the column does
// not leave in clear, and turning the option back on is a click once the key is set.
func withinReach(
	config *mgmtv1alpha1.TransformerConfig,
	hasConsistencyKey bool,
) *mgmtv1alpha1.TransformerConfig {
	if hasConsistencyKey {
		return config
	}
	if phone := config.GetTransformPhoneNumberConfig(); phone.GetPreserveFormat() {
		phone.PreserveFormat = gotypeutil.ToPtr(false)
	}
	return config
}

// constrainedColumns are the columns, by schema.table, that a primary key, a foreign key or a
// unique constraint or index covers, on either side of a foreign key.
func constrainedColumns(constraints *sqlmanager_shared.TableConstraints) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	add := func(table string, columns ...string) {
		if out[table] == nil {
			out[table] = map[string]struct{}{}
		}
		for _, column := range columns {
			out[table][column] = struct{}{}
		}
	}
	if constraints == nil {
		return out
	}
	for table, columns := range constraints.PrimaryKeyConstraints {
		add(table, columns...)
	}
	for table, fks := range constraints.ForeignKeyConstraints {
		for _, fk := range fks {
			add(table, fk.Columns...)
			if fk.ForeignKey != nil {
				add(fk.ForeignKey.Table, fk.ForeignKey.Columns...)
			}
		}
	}
	for table, sets := range constraints.UniqueConstraints {
		for _, columns := range sets {
			add(table, columns...)
		}
	}
	for table, sets := range constraints.UniqueIndexes {
		for _, columns := range sets {
			add(table, columns...)
		}
	}
	return out
}

// sourceColumnsOf lists every column of the tables the mappings cover, with its type as the source
// reports it: what the backend compares from one run to the next to tell that a column changed
// type.
func sourceColumnsOf(
	mappings []*mgmtv1alpha1.JobMapping,
	columnInfo map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
) []*mgmtv1alpha1.JobSourceColumn {
	tables := map[string]struct{ schema, table string }{}
	for _, m := range mappings {
		tables[sqlmanager_shared.BuildTable(m.GetSchema(), m.GetTable())] = struct{ schema, table string }{m.GetSchema(), m.GetTable()}
	}
	out := []*mgmtv1alpha1.JobSourceColumn{}
	for key, t := range tables {
		for column, info := range columnInfo[key] {
			var dataType string
			if info != nil {
				dataType = info.DataType
			}
			out = append(out, &mgmtv1alpha1.JobSourceColumn{
				Column:   &mgmtv1alpha1.JobColumn{Schema: t.schema, Table: t.table, Column: column},
				DataType: dataType,
			})
		}
	}
	// Read out of maps, so in no order: these rows replace the job's source columns on every
	// run, and an order that changes for nothing makes two runs look different.
	slices.SortFunc(out, func(a, b *mgmtv1alpha1.JobSourceColumn) int {
		return cmp.Or(
			cmp.Compare(a.GetColumn().GetSchema(), b.GetColumn().GetSchema()),
			cmp.Compare(a.GetColumn().GetTable(), b.GetColumn().GetTable()),
			cmp.Compare(a.GetColumn().GetColumn(), b.GetColumn().GetColumn()),
		)
	})
	return out
}

func getMapValuesCount[K comparable, V any](m map[K][]V) int {
	count := 0
	for _, v := range m {
		count += len(v)
	}
	return count
}

func buildPlainColumns(mappings []*mgmtv1alpha1.JobMapping) []string {
	columns := make([]string, len(mappings))
	for idx := range mappings {
		columns[idx] = mappings[idx].Column
	}
	return columns
}

func buildTableSubsetMap(
	tableOpts map[string]*sqlSourceTableOptions,
	tableMap map[string]*tableMapping,
) map[string]string {
	tableSubsetMap := map[string]string{}
	for table, opts := range tableOpts {
		if _, ok := tableMap[table]; !ok {
			continue
		}
		if opts != nil && opts.WhereClause != nil && *opts.WhereClause != "" {
			tableSubsetMap[table] = *opts.WhereClause
		}
	}
	return tableSubsetMap
}

func groupSqlJobSourceOptionsByTable(
	sqlSourceOpts *job_util.SqlJobSourceOpts,
) map[string]*sqlSourceTableOptions {
	groupedMappings := map[string]*sqlSourceTableOptions{}
	for _, schemaOpt := range sqlSourceOpts.SchemaOpt {
		for tidx := range schemaOpt.Tables {
			tableOpt := schemaOpt.Tables[tidx]
			key := husonym_benthos.BuildBenthosTable(schemaOpt.Schema, tableOpt.Table)
			groupedMappings[key] = &sqlSourceTableOptions{
				WhereClause: tableOpt.WhereClause,
			}
		}
	}
	return groupedMappings
}

func mergeVirtualForeignKeys(
	dbForeignKeys map[string][]*sqlmanager_shared.ForeignConstraint,
	virtualForeignKeys []*mgmtv1alpha1.VirtualForeignConstraint,
	colInfoMap map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
) (map[string][]*sqlmanager_shared.ForeignConstraint, error) {
	fks := map[string][]*sqlmanager_shared.ForeignConstraint{}

	for table, fk := range dbForeignKeys {
		fks[table] = fk
	}

	for _, fk := range virtualForeignKeys {
		tn := sqlmanager_shared.BuildTable(fk.Schema, fk.Table)
		fkTable := sqlmanager_shared.BuildTable(fk.GetForeignKey().Schema, fk.GetForeignKey().Table)
		notNullable := []bool{}
		for _, c := range fk.GetColumns() {
			colMap, ok := colInfoMap[tn]
			if !ok {
				return nil, fmt.Errorf("virtual foreign key source table not found: %s", tn)
			}
			colInfo, ok := colMap[c]
			if !ok {
				return nil, fmt.Errorf("virtual foreign key source column not found: %s.%s", tn, c)
			}
			notNullable = append(notNullable, !colInfo.IsNullable)
		}
		fks[tn] = append(fks[tn], &sqlmanager_shared.ForeignConstraint{
			Columns:     fk.GetColumns(),
			NotNullable: notNullable,
			ForeignKey: &sqlmanager_shared.ForeignKey{
				Table:   fkTable,
				Columns: fk.GetForeignKey().GetColumns(),
			},
		})
	}

	return fks, nil
}

func groupMappingsByTable(
	mappings []*mgmtv1alpha1.JobMapping,
) []*tableMapping {
	groupedMappings := map[string][]*mgmtv1alpha1.JobMapping{}

	for _, mapping := range mappings {
		key := husonym_benthos.BuildBenthosTable(mapping.Schema, mapping.Table)
		groupedMappings[key] = append(groupedMappings[key], mapping)
	}

	output := make([]*tableMapping, 0, len(groupedMappings))
	for key, mappings := range groupedMappings {
		schema, table := sqlmanager_shared.SplitTableKey(key)
		output = append(output, &tableMapping{
			Schema:   schema,
			Table:    table,
			Mappings: mappings,
		})
	}
	return output
}

func getTableMappingsMap(groupedMappings []*tableMapping) map[string]*tableMapping {
	groupedTableMapping := map[string]*tableMapping{}
	for _, tm := range groupedMappings {
		groupedTableMapping[husonym_benthos.BuildBenthosTable(tm.Schema, tm.Table)] = tm
	}
	return groupedTableMapping
}

func getColumnTransformerMap(
	tableMappingMap map[string]*tableMapping,
) map[string]map[string]*mgmtv1alpha1.JobMappingTransformer {
	colTransformerMap := map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{} // schema.table ->  column -> transformer
	for table, mapping := range tableMappingMap {
		colTransformerMap[table] = map[string]*mgmtv1alpha1.JobMappingTransformer{}
		for _, m := range mapping.Mappings {
			colTransformerMap[table][m.Column] = m.Transformer
		}
	}
	return colTransformerMap
}

func getTableColMapFromMappings(mappings []*tableMapping) map[string][]string {
	tableColMap := map[string][]string{}
	for _, m := range mappings {
		cols := []string{}
		for _, c := range m.Mappings {
			cols = append(cols, c.Column)
		}
		tn := sqlmanager_shared.BuildTable(m.Schema, m.Table)
		tableColMap[tn] = cols
	}
	return tableColMap
}

func filterForeignKeysMap(
	colTransformerMap map[string]map[string]*mgmtv1alpha1.JobMappingTransformer,
	foreignKeysMap map[string][]*sqlmanager_shared.ForeignConstraint,
) map[string][]*sqlmanager_shared.ForeignConstraint {
	newFkMap := make(map[string][]*sqlmanager_shared.ForeignConstraint)

	for table, fks := range foreignKeysMap {
		cols, ok := colTransformerMap[table]
		if !ok {
			continue
		}
		for _, fk := range fks {
			newFk := &sqlmanager_shared.ForeignConstraint{
				ForeignKey: &sqlmanager_shared.ForeignKey{
					Table: fk.ForeignKey.Table,
				},
			}
			for i, c := range fk.Columns {
				t, ok := cols[c]
				if !fk.NotNullable[i] && (!ok || isNullJobMappingTransformer(t)) {
					continue
				}

				newFk.Columns = append(newFk.Columns, c)
				newFk.NotNullable = append(newFk.NotNullable, fk.NotNullable[i])
				newFk.ForeignKey.Columns = append(
					newFk.ForeignKey.Columns,
					fk.ForeignKey.Columns[i],
				)
			}

			if len(newFk.Columns) > 0 {
				newFkMap[table] = append(newFkMap[table], newFk)
			}
		}
	}
	return newFkMap
}

func isNullJobMappingTransformer(t *mgmtv1alpha1.JobMappingTransformer) bool {
	switch t.GetConfig().GetConfig().(type) {
	case *mgmtv1alpha1.TransformerConfig_Nullconfig:
		return true
	default:
		return false
	}
}

func isDefaultJobMappingTransformer(t *mgmtv1alpha1.JobMappingTransformer) bool {
	switch t.GetConfig().GetConfig().(type) {
	case *mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig:
		return true
	default:
		return false
	}
}

// map of table primary key cols to foreign key cols
func getPrimaryKeyDependencyMap(
	tableDependencies map[string][]*sqlmanager_shared.ForeignConstraint,
) map[string]map[string][]*bb_internal.ReferenceKey {
	tc := map[string]map[string][]*bb_internal.ReferenceKey{} // schema.table -> column -> ForeignKey
	for table, constraints := range tableDependencies {
		for _, c := range constraints {
			_, ok := tc[c.ForeignKey.Table]
			if !ok {
				tc[c.ForeignKey.Table] = map[string][]*bb_internal.ReferenceKey{}
			}
			for idx, col := range c.ForeignKey.Columns {
				tc[c.ForeignKey.Table][col] = append(
					tc[c.ForeignKey.Table][col],
					&bb_internal.ReferenceKey{
						Table:  table,
						Column: c.Columns[idx],
					},
				)
			}
		}
	}
	return tc
}

func findTopForeignKeySource(
	tableName, col string,
	tableDependencies map[string][]*sqlmanager_shared.ForeignConstraint,
) *bb_internal.ReferenceKey {
	// Add the foreign key dependencies of the current table
	if foreignKeys, ok := tableDependencies[tableName]; ok {
		for _, fk := range foreignKeys {
			for idx, c := range fk.Columns {
				if c == col {
					// Recursively add dependent tables and their foreign keys
					return findTopForeignKeySource(
						fk.ForeignKey.Table,
						fk.ForeignKey.Columns[idx],
						tableDependencies,
					)
				}
			}
		}
	}
	return &bb_internal.ReferenceKey{
		Table:  tableName,
		Column: col,
	}
}

// builds schema.table -> FK column ->  PK schema table column
// find top level primary key column if foreign keys are nested
func buildForeignKeySourceMap(
	tableDeps map[string][]*sqlmanager_shared.ForeignConstraint,
) map[string]map[string]*bb_internal.ReferenceKey {
	outputMap := map[string]map[string]*bb_internal.ReferenceKey{}
	for tableName, constraints := range tableDeps {
		if _, ok := outputMap[tableName]; !ok {
			outputMap[tableName] = map[string]*bb_internal.ReferenceKey{}
		}
		for _, con := range constraints {
			for _, col := range con.Columns {
				fk := findTopForeignKeySource(tableName, col, tableDeps)
				outputMap[tableName][col] = fk
			}
		}
	}
	return outputMap
}

func getTransformedFksMap(
	tabledependencies map[string][]*sqlmanager_shared.ForeignConstraint,
	colTransformerMap map[string]map[string]*mgmtv1alpha1.JobMappingTransformer,
) map[string]map[string][]*bb_internal.ReferenceKey {
	foreignKeyToSourceMap := buildForeignKeySourceMap(tabledependencies)
	// filter this list by table constraints that has transformer
	transformedForeignKeyToSourceMap := map[string]map[string][]*bb_internal.ReferenceKey{} // schema.table -> column -> foreignKey
	for table, constraints := range foreignKeyToSourceMap {
		_, ok := transformedForeignKeyToSourceMap[table]
		if !ok {
			transformedForeignKeyToSourceMap[table] = map[string][]*bb_internal.ReferenceKey{}
		}
		for col, tc := range constraints {
			// only add constraint if foreign key has transformer
			transformer, transformerOk := colTransformerMap[tc.Table][tc.Column]
			if transformerOk && shouldProcessStrict(transformer) {
				transformedForeignKeyToSourceMap[table][col] = append(
					transformedForeignKeyToSourceMap[table][col],
					tc,
				)
			}
		}
	}
	return transformedForeignKeyToSourceMap
}

func getColumnDefaultProperties(
	slogger *slog.Logger,
	driver string,
	cols []string,
	colInfo map[string]*sqlmanager_shared.DatabaseSchemaRow,
	colTransformers map[string]*mgmtv1alpha1.JobMappingTransformer,
) (map[string]*husonym_benthos.ColumnDefaultProperties, error) {
	colDefaults := map[string]*husonym_benthos.ColumnDefaultProperties{}
	for _, cName := range cols {
		info, ok := colInfo[cName]
		if !ok {
			return nil, fmt.Errorf("column default type missing. column: %s", cName)
		}
		needsOverride, needsReset, err := sqlmanager.GetColumnOverrideAndResetProperties(
			driver,
			info,
		)
		if err != nil {
			slogger.Error(
				"unable to determine SQL column default flags",
				"error",
				err,
				"column",
				cName,
			)
			return nil, err
		}

		jmTransformer, ok := colTransformers[cName]
		if !ok {
			return nil, fmt.Errorf("transformer missing for column: %s", cName)
		}

		var hasDefaultTransformer bool
		if jmTransformer != nil && isDefaultJobMappingTransformer(jmTransformer) {
			hasDefaultTransformer = true
		}
		if !needsReset && !needsOverride && !hasDefaultTransformer {
			continue
		}
		colDefaults[cName] = &husonym_benthos.ColumnDefaultProperties{
			NeedsReset:            needsReset,
			NeedsOverride:         needsOverride,
			HasDefaultTransformer: hasDefaultTransformer,
		}
	}
	return colDefaults, nil
}

type destinationOptions struct {
	OnConflictDoNothing      bool
	OnConflictDoUpdate       bool
	Truncate                 bool
	TruncateCascade          bool
	SkipForeignKeyViolations bool
	MaxInFlight              uint32
	BatchCount               int
	BatchPeriod              string
}

func getDestinationOptions(
	destOpts *mgmtv1alpha1.JobDestinationOptions,
) (*destinationOptions, error) {
	if destOpts.GetConfig() == nil {
		return &destinationOptions{}, nil
	}
	switch config := destOpts.GetConfig().(type) {
	case *mgmtv1alpha1.JobDestinationOptions_PostgresOptions:
		if config.PostgresOptions == nil {
			return &destinationOptions{}, nil
		}
		batchingConfig, err := getParsedBatchingConfig(config.PostgresOptions)
		if err != nil {
			return nil, err
		}
		onConflictDoNothing := false
		onConflictDoUpdate := false
		if config.PostgresOptions.GetOnConflict().GetNothing() != nil {
			onConflictDoNothing = true
		} else if config.PostgresOptions.GetOnConflict().GetUpdate() != nil {
			onConflictDoUpdate = true
		}
		if onConflictDoNothing && onConflictDoUpdate {
			return nil, fmt.Errorf("cannot have both on conflict do nothing and on conflict do update")
		}
		return &destinationOptions{
			OnConflictDoNothing:      onConflictDoNothing,
			OnConflictDoUpdate:       onConflictDoUpdate,
			Truncate:                 config.PostgresOptions.GetTruncateTable().GetTruncateBeforeInsert(),
			TruncateCascade:          config.PostgresOptions.GetTruncateTable().GetCascade(),
			SkipForeignKeyViolations: config.PostgresOptions.GetSkipForeignKeyViolations(),
			MaxInFlight:              batchingConfig.MaxInFlight,
			BatchCount:               batchingConfig.BatchCount,
			BatchPeriod:              batchingConfig.BatchPeriod,
		}, nil
	case *mgmtv1alpha1.JobDestinationOptions_MysqlOptions:
		if config.MysqlOptions == nil {
			return &destinationOptions{}, nil
		}
		batchingConfig, err := getParsedBatchingConfig(config.MysqlOptions)
		if err != nil {
			return nil, err
		}
		onConflictDoNothing := false
		onConflictDoUpdate := false
		if config.MysqlOptions.GetOnConflict().GetNothing() != nil {
			onConflictDoNothing = true
		} else if config.MysqlOptions.GetOnConflict().GetUpdate() != nil {
			onConflictDoUpdate = true
		}
		if onConflictDoNothing && onConflictDoUpdate {
			return nil, fmt.Errorf("cannot have both on conflict do nothing and on conflict do update")
		}
		return &destinationOptions{
			OnConflictDoNothing:      onConflictDoNothing,
			OnConflictDoUpdate:       onConflictDoUpdate,
			Truncate:                 config.MysqlOptions.GetTruncateTable().GetTruncateBeforeInsert(),
			SkipForeignKeyViolations: config.MysqlOptions.GetSkipForeignKeyViolations(),
			MaxInFlight:              batchingConfig.MaxInFlight,
			BatchCount:               batchingConfig.BatchCount,
			BatchPeriod:              batchingConfig.BatchPeriod,
		}, nil
	case *mgmtv1alpha1.JobDestinationOptions_MssqlOptions:
		if config.MssqlOptions == nil {
			return &destinationOptions{}, nil
		}
		batchingConfig, err := getParsedBatchingConfig(config.MssqlOptions)
		if err != nil {
			return nil, err
		}
		return &destinationOptions{
			SkipForeignKeyViolations: config.MssqlOptions.GetSkipForeignKeyViolations(),
			MaxInFlight:              batchingConfig.MaxInFlight,
			BatchCount:               batchingConfig.BatchCount,
			BatchPeriod:              batchingConfig.BatchPeriod,
		}, nil
	default:
		return &destinationOptions{}, nil
	}
}

type batchingConfig struct {
	MaxInFlight uint32
	BatchPeriod string
	BatchCount  int
}
type batchDestinationOption interface {
	GetMaxInFlight() uint32
	GetBatch() *mgmtv1alpha1.BatchConfig
}

func getParsedBatchingConfig(destOpt batchDestinationOption) (batchingConfig, error) {
	output := batchingConfig{
		MaxInFlight: 10,
		BatchPeriod: "5s",
		BatchCount:  100,
	}
	if destOpt == nil {
		return output, nil
	}
	if destOpt.GetMaxInFlight() > 0 {
		output.MaxInFlight = destOpt.GetMaxInFlight()
	}

	batchConfig := destOpt.GetBatch()
	if batchConfig != nil {
		output.BatchCount = int(batchConfig.GetCount())

		if batchConfig.GetPeriod() != "" {
			_, err := time.ParseDuration(batchConfig.GetPeriod())
			if err != nil {
				return batchingConfig{}, fmt.Errorf(
					"unable to parse batch period for s3 destination config: %w",
					err,
				)
			}
		}
		output.BatchPeriod = batchConfig.GetPeriod()
	}

	if output.BatchCount == 0 && output.BatchPeriod == "" {
		return batchingConfig{}, fmt.Errorf(
			"must have at least one batch policy configured. Cannot disable both period and count",
		)
	}
	return output, nil
}

// Based on the source schema and the provided mappings, we find the missing columns (if any) and generate passthrough job mappings for them automatically
func getAdditionalPassthroughJobMappings(
	groupedSchemas map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	mappings []*mgmtv1alpha1.JobMapping,
	getTableFromKey func(key string) (schema, table string, err error),
	logger *slog.Logger,
) ([]*mgmtv1alpha1.JobMapping, error) {
	output := []*mgmtv1alpha1.JobMapping{}

	tableColMappings := getUniqueColMappingsMap(mappings)

	for schematable, cols := range groupedSchemas {
		mappedCols, ok := tableColMappings[schematable]
		if !ok {
			// todo: we may want to generate mappings for this entire table? However this may be dead code as we get the grouped schemas based on the mappings
			logger.Warn(
				"table found in schema data that is not present in job mappings",
				"table",
				schematable,
			)
			continue
		}
		if len(cols) == len(mappedCols) {
			continue
		}
		for col, info := range cols {
			if _, ok := mappedCols[col]; !ok {
				schema, table, err := getTableFromKey(schematable)
				if err != nil {
					return nil, err
				}
				// we found a column that is not present in the mappings, let's create a mapping for it
				if info.GeneratedType != nil {
					output = append(output, &mgmtv1alpha1.JobMapping{
						Schema: schema,
						Table:  table,
						Column: col,
						Transformer: &mgmtv1alpha1.JobMappingTransformer{
							Config: &mgmtv1alpha1.TransformerConfig{
								Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
									GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
								},
							},
						},
					})
				} else {
					output = append(output, &mgmtv1alpha1.JobMapping{
						Schema: schema,
						Table:  table,
						Column: col,
						Transformer: &mgmtv1alpha1.JobMappingTransformer{
							Config: &mgmtv1alpha1.TransformerConfig{
								Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
									PassthroughConfig: &mgmtv1alpha1.Passthrough{},
								},
							},
						},
					})
				}
			}
		}
	}

	// Map iteration gives a different order every time, and these mappings are written to the
	// job: without this, two runs that find the same new columns leave the job's mappings in a
	// different order, and its history reads as a change where nothing changed.
	sortMappings(output)
	return output, nil
}

// sortMappings orders mappings by schema, table then column.
func sortMappings(mappings []*mgmtv1alpha1.JobMapping) {
	slices.SortFunc(mappings, func(a, b *mgmtv1alpha1.JobMapping) int {
		return cmp.Or(
			cmp.Compare(a.GetSchema(), b.GetSchema()),
			cmp.Compare(a.GetTable(), b.GetTable()),
			cmp.Compare(a.GetColumn(), b.GetColumn()),
		)
	})
}

func shouldOverrideColumnDefault(
	columnDefaults map[string]*husonym_benthos.ColumnDefaultProperties,
) bool {
	for _, cd := range columnDefaults {
		if cd != nil && !cd.HasDefaultTransformer && cd.NeedsOverride {
			return true
		}
	}
	return false
}

func getSqlBatchProcessors(
	driver string,
	columns []string,
	columnDataTypes map[string]string,
	columnDefaultProperties map[string]*husonym_benthos.ColumnDefaultProperties,
) (*husonym_benthos.BatchProcessor, error) {
	switch driver {
	case sqlmanager_shared.PostgresDriver:
		return &husonym_benthos.BatchProcessor{
			HusonymToPgx: &husonym_benthos.HusonymToPgxConfig{
				Columns:                 columns,
				ColumnDataTypes:         columnDataTypes,
				ColumnDefaultProperties: columnDefaultProperties,
			},
		}, nil
	case sqlmanager_shared.MysqlDriver:
		return &husonym_benthos.BatchProcessor{
			HusonymToMysql: &husonym_benthos.HusonymToMysqlConfig{
				Columns:                 columns,
				ColumnDataTypes:         columnDataTypes,
				ColumnDefaultProperties: columnDefaultProperties,
			},
		}, nil
	case sqlmanager_shared.MssqlDriver:
		return &husonym_benthos.BatchProcessor{
			HusonymToMssql: &husonym_benthos.HusonymToMssqlConfig{
				Columns:                 columns,
				ColumnDataTypes:         columnDataTypes,
				ColumnDefaultProperties: columnDefaultProperties,
			},
		}, nil
	default:
		return nil, fmt.Errorf(
			"unsupported driver %q when attempting to get sql batch processors",
			driver,
		)
	}
}

func getTableDeferrableMap(
	ctx context.Context,
	db *sqlmanager.SqlConnection,
	connection *mgmtv1alpha1.Connection,
	schemaTablesMap map[string][]string,
) (map[string]bool, error) {
	tableDeferrableMap := map[string]bool{}
	var mutx sync.Mutex
	if connection.ConnectionConfig.GetPgConfig() != nil {
		errgrp, errctx := errgroup.WithContext(ctx)
		for schema, tables := range schemaTablesMap {
			schema, tables := schema, tables // capture range variables
			errgrp.Go(func() error {
				constraints, err := db.Db().GetTableConstraintsByTables(errctx, schema, tables)
				if err != nil {
					return err
				}
				for table, cons := range constraints {
					hasDeferrableConstraint := false
					for _, fk := range cons.ForeignKeyConstraints {
						if !fk.Deferrable {
							continue
						}
						mutx.Lock()
						tableDeferrableMap[table] = true
						mutx.Unlock()
						hasDeferrableConstraint = true
						break
					}
					if !hasDeferrableConstraint {
						for _, constraint := range cons.NonForeignKeyConstraints {
							if constraint.Deferrable {
								mutx.Lock()
								tableDeferrableMap[table] = true
								mutx.Unlock()
								break
							}
						}
					}
				}
				return nil
			})
		}
		if err := errgrp.Wait(); err != nil {
			return nil, err
		}
	}
	return tableDeferrableMap, nil
}

// withoutNullableColumns keeps, per table, the unique keys whose columns are all NOT
// NULL: only those can order the pages of a table sync. A unique index accepts any
// number of NULLs, which keyset pagination ("col > last value") never reads past.
func withoutNullableColumns(
	uniqueKeys map[string][][]string,
	columnInfo map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
) map[string][][]string {
	filtered := make(map[string][][]string, len(uniqueKeys))
	for table, keys := range uniqueKeys {
		for _, key := range keys {
			nullable := slices.ContainsFunc(key, func(column string) bool {
				info, ok := columnInfo[table][column]
				return !ok || info.IsNullable
			})
			if !nullable {
				filtered[table] = append(filtered[table], key)
			}
		}
	}
	return filtered
}

// planForeignKeys describes, per run config, the foreign keys of its table for the
// engine-neutral plan: which parents the job copies only in part, which value of a
// mandatory key means "no parent", and where the new values of a transformed parent key
// are published (keyStore, "" for a column copied as it is).
func planForeignKeys(
	driver string,
	runConfigs []*rc.RunConfig,
	subsetByForeignKeyConstraints bool,
	columnInfo map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	declaredForeignKeys map[string][]*sqlmanager_shared.ForeignConstraint,
	keyStore func(table, column string) string,
) map[string][]*tableplan.ForeignKey {
	reduced := make(map[string]bool, len(runConfigs))
	for _, config := range runConfigs {
		if subsetByForeignKeyConstraints {
			reduced[config.Table()] = len(config.SubsetPaths()) > 0
		} else {
			reduced[config.Table()] = config.WhereClause() != nil && *config.WhereClause() != ""
		}
	}

	byConfig := make(map[string][]*tableplan.ForeignKey, len(runConfigs))
	for _, config := range runConfigs {
		for _, fk := range config.ForeignKeys() {
			// A key the job writes only in part is never enforced: filterForeignKeysMap
			// took out the columns a null transformer writes NULL, and a key holding a
			// NULL references nothing (MATCH SIMPLE). What is left of such a key reads as
			// mandatory — every column it kept refuses NULL — so the engine would delete,
			// or refuse, rows the database accepts. It is left out of the plan.
			if reducedKey(declaredForeignKeys[config.Table()], fk) {
				continue
			}
			planned := &tableplan.ForeignKey{
				Columns:       fk.Columns,
				NotNull:       fk.NotNullable,
				ParentSchema:  fk.ReferenceSchema,
				ParentTable:   fk.ReferenceTable,
				ParentColumns: fk.ReferenceColumns,
				ParentReduced: reduced[fk.ReferenceSchema+"."+fk.ReferenceTable],
			}
			parentKey := fk.ReferenceSchema + "." + fk.ReferenceTable
			for _, parentColumn := range fk.ReferenceColumns {
				planned.ParentKeyStores = append(planned.ParentKeyStores, keyStore(parentKey, parentColumn))
			}
			// parent_id NOT NULL DEFAULT 0: the default stands for "no parent".
			if len(fk.Columns) == 1 && planned.IsMandatory() {
				if info, ok := columnInfo[config.Table()][fk.Columns[0]]; ok {
					if value, ok := noParentValue(driver, info.ColumnDefault); ok {
						planned.NoParentValue = &value
					}
				}
			}
			byConfig[config.Id()] = append(byConfig[config.Id()], planned)
		}
	}
	return byConfig
}

// reducedKey reports whether a key of a run config is what is left of a declared key
// after the columns written NULL were taken out of it. A key declared with the very
// columns the run config holds is not reduced, whatever else the table declares.
func reducedKey(declared []*sqlmanager_shared.ForeignConstraint, fk *rc.ForeignKey) bool {
	parent := fk.ReferenceSchema + "." + fk.ReferenceTable
	toParent := make([]*sqlmanager_shared.ForeignConstraint, 0, len(declared))
	for _, candidate := range declared {
		if candidate.ForeignKey == nil || candidate.ForeignKey.Table != parent {
			continue
		}
		if slices.Equal(candidate.Columns, fk.Columns) {
			return false
		}
		toParent = append(toParent, candidate)
	}
	for _, candidate := range toParent {
		if len(candidate.Columns) > len(fk.Columns) && holdsAll(candidate.Columns, fk.Columns) {
			return true
		}
	}
	return false
}

func holdsAll(columns, wanted []string) bool {
	for _, column := range wanted {
		if !slices.Contains(columns, column) {
			return false
		}
	}
	return true
}

// noParentDefaultCast matches the type a PostgreSQL default is reported with: the default
// of a text column reads 'XX'::text, and of a domain 'XX'::public.code.
var noParentDefaultCast = regexp.MustCompile(`::\s*[A-Za-z_][A-Za-z0-9_. ]*(\[\])?\s*$`)

// noParentValue reads, from the default of a mandatory single-column key, the value that
// means "no parent" — the one rows holding it reference nothing on purpose with.
//
// Only a value is a sentinel. A default the database computes (nextval, a function call,
// CURRENT_TIMESTAMP) names no row of the parent table, and what it produces is not known
// here: such a default gives no sentinel rather than a wrong one. A wrong one is not
// harmless — the rows holding it are the ones the check deletes.
//
// Each database reports a default in its own way: MySQL gives the value itself, PostgreSQL
// the expression it parsed back ('XX'::text), SQL Server the same in parentheses (('XX')).
func noParentValue(driver, columnDefault string) (string, bool) {
	literal := strings.TrimSpace(columnDefault)
	if literal == "" {
		return "", false
	}
	if driver == sqlmanager_shared.MysqlDriver {
		// MySQL reports the value as it is, unquoted. Only a computed default is an
		// expression, and it is the one case it puts in parentheses.
		if strings.HasPrefix(literal, "(") {
			return "", false
		}
		return literal, true
	}
	// SQL Server wraps a default in parentheses, a literal in two: (('XX')), ((0)).
	for strings.HasPrefix(literal, "(") && strings.HasSuffix(literal, ")") {
		literal = strings.TrimSpace(literal[1 : len(literal)-1])
	}
	literal = strings.TrimSpace(noParentDefaultCast.ReplaceAllString(literal, ""))
	if strings.HasPrefix(literal, "'") && strings.HasSuffix(literal, "'") && len(literal) >= 2 {
		return strings.ReplaceAll(literal[1:len(literal)-1], "''", "'"), true
	}
	if noParentNumeric.MatchString(literal) {
		return literal, true
	}
	return "", false
}

// noParentNumeric matches a number written out, the other shape a key's default takes.
var noParentNumeric = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)

// generatedColumns returns, in name order, the columns of a table the database computes
// itself and refuses any value for. Identity columns are not among them: they accept the
// values of the source.
func generatedColumns(columns map[string]*sqlmanager_shared.DatabaseSchemaRow) []string {
	var generated []string
	for name, info := range columns {
		if isGenerated(info) {
			generated = append(generated, name)
		}
	}
	slices.Sort(generated)
	return generated
}

// unmappedColumns counts columns in a sentence, singular or plural.
func unmappedColumns(n int) string {
	if n == 1 {
		return "1 unmapped column"
	}
	return fmt.Sprintf("%d unmapped columns", n)
}
