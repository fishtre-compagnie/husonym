package benthosbuilder_builders

import (
	"context"
	"fmt"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	rc "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
)

// What the plan of a SQL sync tells of its run, found while computing it: see
// internal/preflight. Only MySQL and PostgreSQL are reported on, the databases the bench
// verifies each finding on.

// preflightDriver reports whether findings are reported for a database.
func preflightDriver(driver string) bool {
	return driver == sqlmanager_shared.MysqlDriver || driver == sqlmanager_shared.PostgresDriver
}

// sourceFindings reports what the plan of each table says of how its rows are read and
// which values it writes, whatever the destination.
func sourceFindings(
	ctx context.Context,
	transformers *transformerConfigs,
	job *mgmtv1alpha1.Job,
	usesAthanor bool,
	subsetByForeignKeys bool,
	runConfigs []*rc.RunConfig,
	constraints *sqlmanager_shared.TableConstraints,
	columns map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	colTransformerMap map[string]map[string]*mgmtv1alpha1.JobMappingTransformer,
	foreignKeys map[string][]*tableplan.ForeignKey,
) ([]*preflight.Finding, error) {
	var findings []*preflight.Finding
	for _, config := range runConfigs {
		// Every pass of a table reads it the same way: the insert pass speaks for them.
		if config.RunType() != rc.RunTypeInsert {
			continue
		}
		table := config.Table()
		keys := uniqueKeysOf(constraints, table)

		if len(config.OrderByColumns()) == 0 {
			findings = append(findings, readInOneStream(table, constraints.PrimaryKeyConstraints[table], keys))
		}
		// "Do nothing" collides only on a key without NULL: a row holding one is written again.
		keyed := len(constraints.PrimaryKeyConstraints[table]) > 0 || hasNotNullKey(keys, columns[table])
		if !keyed && !usesAthanor && retries(job) {
			findings = append(findings, &preflight.Finding{
				Kind:  mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE,
				Level: preflight.Warning,
				Table: table,
				Message: fmt.Sprintf("%s has no key free of NULL: when Benthos retries a write of it that failed part way, "+
					"it writes again the rows already written, which the destination then holds twice", table),
			})
		}

		constant, err := constantColumns(ctx, transformers, colTransformerMap[table])
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if !allIn(key, constant) {
				continue
			}
			findings = append(findings, &preflight.Finding{
				Kind:    mgmtv1alpha1.PreflightFinding_KIND_CONSTANT_ON_UNIQUE,
				Level:   preflight.Warning,
				Table:   table,
				Columns: key,
				Message: fmt.Sprintf("%s: every row is given the same value in %s, which a unique key refuses "+
					"from the second row on", table, strings.Join(key, ", ")),
			})
		}

		for _, fk := range foreignKeys[config.Id()] {
			// A key the subset joins on keeps only the rows whose parent is kept: nothing
			// is cleared, the other rows are left out.
			if !fk.ParentReduced || fk.IsMandatory() || (subsetByForeignKeys && joinsSubset(config, fk)) {
				continue
			}
			var cleared []string
			for i, column := range fk.Columns {
				if i < len(fk.NotNull) && !fk.NotNull[i] {
					cleared = append(cleared, column)
				}
			}
			parent := fk.ParentSchema + "." + fk.ParentTable
			findings = append(findings, &preflight.Finding{
				Kind:    mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET,
				Level:   preflight.Information,
				Table:   table,
				Columns: cleared,
				Message: fmt.Sprintf("%s: a reference in %s to a row of %s the subset leaves out is written as NULL",
					table, strings.Join(cleared, ", "), parent),
			})
		}
	}
	return findings, nil
}

// joinsSubset reports whether the subset of a table joins its parent on a foreign key.
func joinsSubset(config *rc.RunConfig, fk *tableplan.ForeignKey) bool {
	for _, path := range config.SubsetPaths() {
		for _, step := range path.JoinSteps {
			if step.FromKey != config.Table() || step.ForeignKey == nil {
				continue
			}
			if step.ToKey == fk.ParentSchema+"."+fk.ParentTable && slices.Equal(step.ForeignKey.Columns, fk.Columns) {
				return true
			}
		}
	}
	return false
}

// readInOneStream says why a table is read in one stream rather than page by page.
func readInOneStream(table string, primaryKey []string, keys [][]string) *preflight.Finding {
	var why string
	switch {
	case len(primaryKey) > 0:
		why = fmt.Sprintf("the job does not read its primary key (%s)", strings.Join(primaryKey, ", "))
	case len(keys) > 0:
		why = "none of its unique keys is both read by the job and free of NULL"
	default:
		why = "it has no primary key nor unique key"
	}
	return &preflight.Finding{
		Kind:  mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
		Level: preflight.Information,
		Table: table,
		Message: fmt.Sprintf("%s is read in one stream rather than page by page: %s. "+
			"A table sync that fails reads it again from its first row", table, why),
	}
}

// uniqueKeysOf returns the keys refusing two rows with the same values in a table: its
// primary key, unique constraints and unique indexes, nullable columns included, each once.
func uniqueKeysOf(constraints *sqlmanager_shared.TableConstraints, table string) [][]string {
	var keys [][]string
	add := func(key []string) {
		if len(key) == 0 {
			return
		}
		for _, known := range keys {
			if sameColumns(known, key) {
				return
			}
		}
		keys = append(keys, key)
	}
	add(constraints.PrimaryKeyConstraints[table])
	for _, key := range constraints.UniqueConstraints[table] {
		add(key)
	}
	for _, key := range constraints.UniqueIndexes[table] {
		add(key)
	}
	return keys
}

// hasNotNullKey reports whether one of the keys has no nullable column.
func hasNotNullKey(keys [][]string, columns map[string]*sqlmanager_shared.DatabaseSchemaRow) bool {
	for _, key := range keys {
		nullable := false
		for _, column := range key {
			if info, ok := columns[column]; !ok || info.IsNullable {
				nullable = true
			}
		}
		if !nullable {
			return true
		}
	}
	return false
}

func sameColumns(a, b []string) bool {
	return len(a) == len(b) && allIn(a, b)
}

func allIn(columns, set []string) bool {
	for _, column := range columns {
		if !slices.Contains(set, column) {
			return false
		}
	}
	return true
}

// retries reports whether a table sync of the job is attempted more than once: an attempt
// count of zero is no limit.
func retries(job *mgmtv1alpha1.Job) bool {
	policy := job.GetSyncOptions().GetRetryPolicy()
	return policy != nil && policy.MaximumAttempts != nil && *policy.MaximumAttempts != 1
}

// constantColumns returns the columns a transformer gives one same value on every row.
func constantColumns(
	ctx context.Context,
	configs *transformerConfigs,
	transformers map[string]*mgmtv1alpha1.JobMappingTransformer,
) ([]string, error) {
	var constant []string
	for column, transformer := range transformers {
		config, err := configs.of(ctx, transformer)
		if err != nil {
			return nil, err
		}
		if categorical := config.GetGenerateCategoricalConfig(); categorical != nil && len(categoriesOf(categorical)) == 1 {
			constant = append(constant, column)
		}
	}
	return constant, nil
}

// destinationFindings reports what the plan of a table says of what a destination
// receives: values it refuses, or may not hold.
func destinationFindings(
	ctx context.Context,
	configs *transformerConfigs,
	usesAthanor bool,
	connectionID string,
	config *bb_internal.BenthosSourceConfig,
	destinationColumns map[string]*sqlmanager_shared.DatabaseSchemaRow,
	sourceColumns map[string]*sqlmanager_shared.DatabaseSchemaRow,
	transformers map[string]*mgmtv1alpha1.JobMappingTransformer,
) ([]*preflight.Finding, error) {
	if config.RunType != rc.RunTypeInsert {
		return nil, nil
	}
	table := sqlmanager_shared.BuildTable(config.TableSchema, config.TableName)
	var findings []*preflight.Finding
	for _, column := range config.Columns {
		transformer := transformers[column]
		if transformer == nil {
			continue
		}
		transformerConfig, err := configs.of(ctx, transformer)
		if err != nil {
			return nil, err
		}
		// The destination computes a default itself: nothing is written.
		if transformerConfig.GetGenerateDefaultConfig() != nil {
			continue
		}
		destination, ok := destinationColumns[column]
		if !ok {
			continue
		}
		if isGenerated(destination) {
			findings = append(findings, generatedColumnWritten(usesAthanor, connectionID, table, column,
				slices.Contains(config.GeneratedColumns, column)))
			continue
		}
		if destination.CharacterMaximumLength <= 0 {
			continue
		}
		longest, what := longestOutput(transformerConfig, sourceColumns[column])
		if longest <= destination.CharacterMaximumLength {
			continue
		}
		findings = append(findings, &preflight.Finding{
			Kind:         mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG,
			Level:        preflight.Warning,
			ConnectionID: connectionID,
			Table:        table,
			Columns:      []string{column},
			Message: fmt.Sprintf("%s.%s takes %d characters, and the run writes into it %s of up to %d",
				table, column, destination.CharacterMaximumLength, what, longest),
		})
	}
	return findings, nil
}

// generatedColumnWritten reports a column the destination computes itself, given a value
// by the job. Benthos writes every column it is given, and the destination refuses the
// row. Athanor leaves out of its writes the columns generated in the source; one the
// destination alone generates, it writes, and is refused the same way.
func generatedColumnWritten(usesAthanor bool, connectionID, table, column string, generatedInSource bool) *preflight.Finding {
	finding := &preflight.Finding{
		Kind:         mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN,
		Level:        preflight.Blocking,
		ConnectionID: connectionID,
		Table:        table,
		Columns:      []string{column},
	}
	switch {
	case usesAthanor && generatedInSource:
		finding.Level = preflight.Information
		finding.Message = fmt.Sprintf("%s.%s is computed by the destination: Athanor does not write it, "+
			"and its transformer is not applied", table, column)
	case usesAthanor:
		finding.Message = fmt.Sprintf("%s.%s is computed by the destination, which refuses the value Athanor "+
			"would write into it: map it to Generate Default", table, column)
	default:
		finding.Message = fmt.Sprintf("%s.%s is computed by the destination, which refuses the value Benthos "+
			"would write into it: map it to Generate Default", table, column)
	}
	return finding
}

// isGenerated reports whether the database computes a column itself and refuses any value
// for it. Identity columns are not generated: they accept the values of the source.
func isGenerated(info *sqlmanager_shared.DatabaseSchemaRow) bool {
	return !info.UpdateAllowed && info.IdentityGeneration == nil
}

// longestOutput returns the length of the longest value a transformer writes, and what
// writes it, when it is known from its config alone: the source value copied as it is,
// or a generator whose values have a known length whatever the column. Other transformers
// fit their values to the column, or cannot be told: they give 0.
func longestOutput(
	config *mgmtv1alpha1.TransformerConfig,
	source *sqlmanager_shared.DatabaseSchemaRow,
) (length int, what string) {
	switch {
	case config.GetPassthroughConfig() != nil:
		if source == nil {
			return 0, ""
		}
		return source.CharacterMaximumLength, "the values of the source"
	case config.GetGenerateUuidConfig() != nil:
		if hyphens := config.GetGenerateUuidConfig().IncludeHyphens; hyphens != nil && !*hyphens {
			return 32, "uuids"
		}
		return 36, "uuids"
	case config.GetGenerateSha256HashConfig() != nil:
		return 64, "SHA-256 hashes"
	case config.GetGenerateCategoricalConfig() != nil:
		longest := 0
		for _, category := range categoriesOf(config.GetGenerateCategoricalConfig()) {
			longest = max(longest, len([]rune(category)))
		}
		return longest, "categories"
	}
	return 0, ""
}

// categoriesOf returns the distinct values a categorical transformer picks from.
func categoriesOf(config *mgmtv1alpha1.GenerateCategorical) []string {
	categories := "ultimo,proximo,semper" // the default of generate_categorical
	if config.Categories != nil {
		categories = *config.Categories
	}
	var distinct []string
	for _, category := range strings.Split(categories, ",") {
		if !slices.Contains(distinct, category) {
			distinct = append(distinct, category)
		}
	}
	return distinct
}

// transformerConfigs gives the config a transformer of the job runs: a user-defined one
// resolved to the system transformer it configures, asked once for the whole job.
type transformerConfigs struct {
	client   mgmtv1alpha1connect.TransformersServiceClient
	resolved map[string]*mgmtv1alpha1.TransformerConfig
}

func newTransformerConfigs(client mgmtv1alpha1connect.TransformersServiceClient) *transformerConfigs {
	return &transformerConfigs{client: client, resolved: map[string]*mgmtv1alpha1.TransformerConfig{}}
}

func (c *transformerConfigs) of(
	ctx context.Context,
	transformer *mgmtv1alpha1.JobMappingTransformer,
) (*mgmtv1alpha1.TransformerConfig, error) {
	userDefined := transformer.GetConfig().GetUserDefinedTransformerConfig()
	if userDefined == nil {
		return transformer.GetConfig(), nil
	}
	if config, ok := c.resolved[userDefined.GetId()]; ok {
		return config, nil
	}
	resolved, err := convertUserDefinedFunctionConfig(ctx, c.client, transformer)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve a user defined transformer: %w", err)
	}
	c.resolved[userDefined.GetId()] = resolved.GetConfig()
	return resolved.GetConfig(), nil
}
