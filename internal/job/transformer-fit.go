package job

import (
	"fmt"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
)

// columnTraits are what decides which transformers a column takes.
type columnTraits struct {
	dataType   mgmtv1alpha1.TransformerDataType
	nullable   bool
	hasDefault bool
	generated  bool
	// identity is how the database numbers the column: "a" (always) or "d" (by default) in
	// PostgreSQL, "auto_increment" in MySQL, "IDENTITY…" in SQL Server; empty otherwise.
	identity   string
	foreignKey bool
}

// traitsOf reads what decides the transformers a column takes from its row in the schema, and
// from whether a foreign key, real or virtual, starts from it.
func traitsOf(row *sqlmanager_shared.DatabaseSchemaRow, foreignKey bool) columnTraits {
	identity := ""
	if row.IdentityGeneration != nil {
		identity = *row.IdentityGeneration
	}
	return columnTraits{
		dataType:   TransformerDataTypeOf(row.DataType),
		nullable:   row.IsNullable,
		hasDefault: row.ColumnDefault != "" || identity != "",
		generated:  row.GeneratedType != nil && *row.GeneratedType != "",
		identity:   identity,
		foreignKey: foreignKey,
	}
}

// transformerFits says whether a system transformer can be mapped on a column in a job of a
// type. It is the rule the UI applies to the list of transformers it offers for a column
// (shouldIncludeSystem, in frontend/apps/web/components/jobs/SchemaTable/transformer-handler.ts):
// what a person is offered and what the API accepts have to be the same set.
func transformerFits(
	transformer *mgmtv1alpha1.SystemTransformer,
	column columnTraits,
	jobType mgmtv1alpha1.SupportedJobType,
) bool {
	source := transformer.GetSource()
	if !slices.Contains(transformer.GetSupportedJobTypes(), jobType) {
		return false
	}
	// A generated column is computed by the database: nothing may be written into it.
	if column.generated {
		return source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT
	}
	if column.identity == "auto_increment" || strings.HasPrefix(column.identity, "IDENTITY") {
		return slices.Contains([]mgmtv1alpha1.TransformerSource{
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_SCRAMBLE_IDENTITY,
		}, source)
	}
	// Generated always, in PostgreSQL: no value may be given but the database's own.
	if column.identity == "a" {
		return source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT ||
			source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH
	}
	if source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT {
		return column.hasDefault
	}
	if column.foreignKey {
		return slices.Contains(foreignKeyTransformers(column, jobType), source)
	}
	if column.nullable {
		if source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_NULL {
			return true
		}
		if !slices.Contains(transformer.GetDataTypes(), mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL) {
			return false
		}
	}
	return acceptsDataType(transformer.GetDataTypes(), column.dataType)
}

// foreignKeyTransformers are the transformers a foreign key column takes: its value has to
// match a row of the table it references, so it is kept, or computed by code that knows how.
func foreignKeyTransformers(column columnTraits, jobType mgmtv1alpha1.SupportedJobType) []mgmtv1alpha1.TransformerSource {
	var allowed []mgmtv1alpha1.TransformerSource
	switch jobType {
	case mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_UNSPECIFIED, mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_SYNC:
		allowed = append(allowed,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT,
		)
	case mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_GENERATE:
		allowed = append(allowed, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_JAVASCRIPT)
	}
	if column.nullable {
		allowed = append(allowed, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_NULL)
	}
	if column.hasDefault {
		allowed = append(allowed, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT)
	}
	return allowed
}

// fittingTransformers names, sorted, the system transformers a column takes.
func fittingTransformers(column columnTraits, jobType mgmtv1alpha1.SupportedJobType) []string {
	var names []string
	for _, transformer := range catalog.Transformers(true) {
		if transformerFits(transformer, column, jobType) {
			names = append(names, transformerName(transformer.GetSource()))
		}
	}
	slices.Sort(names)
	return names
}

// transformerName names a transformer source the way the API's clients spell it: generate_email.
func transformerName(source mgmtv1alpha1.TransformerSource) string {
	return strings.ToLower(strings.TrimPrefix(source.String(), "TRANSFORMER_SOURCE_"))
}

// SupportedJobTypeOf is the type of job a source makes, as transformers declare the jobs they
// support: generate for a generating source, sync for any other; unspecified without a source.
func SupportedJobTypeOf(source *mgmtv1alpha1.JobSource) mgmtv1alpha1.SupportedJobType {
	options := source.GetOptions()
	switch {
	case options == nil:
		return mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_UNSPECIFIED
	case options.GetGenerate() != nil, options.GetAiGenerate() != nil:
		return mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_GENERATE
	default:
		return mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_SYNC
	}
}

// WithJobType has the validator hold each mapping's transformer to the columns it fits, for a
// job of that type. Without it, the transformers are not checked: the type of job decides.
func WithJobType(jobType mgmtv1alpha1.SupportedJobType) Option {
	return func(jmv *JobMappingsValidator) {
		jmv.jobType = jobType
	}
}

// ValidateTransformers reports each mapping whose system transformer does not fit its column:
// a value the database refuses, a foreign key sent nowhere, a type the transformer does not
// take. A user-defined transformer, and a column missing from the source, are left to the
// other checks.
func (j *JobMappingsValidator) ValidateTransformers(
	tableColumnMap map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	foreignKeys map[string][]*sqlmanager_shared.ForeignConstraint,
	virtualForeignKeys []*mgmtv1alpha1.VirtualForeignConstraint,
) {
	if j.jobType == mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_UNSPECIFIED {
		return
	}
	isForeignKey := map[string]map[string]bool{}
	mark := func(table, column string) {
		if isForeignKey[table] == nil {
			isForeignKey[table] = map[string]bool{}
		}
		isForeignKey[table][column] = true
	}
	for table, constraints := range foreignKeys {
		for _, constraint := range constraints {
			for _, column := range constraint.Columns {
				mark(table, column)
			}
		}
	}
	for _, vfk := range virtualForeignKeys {
		for _, column := range vfk.GetColumns() {
			mark(sqlmanager_shared.BuildTable(vfk.GetSchema(), vfk.GetTable()), column)
		}
	}

	transformers := catalog.BySource(true)
	for table, mappings := range j.jobMappings {
		for column, mapping := range mappings {
			row, ok := tableColumnMap[table][column]
			if !ok {
				continue
			}
			source, ok := catalog.SourceOf(mapping.GetTransformer().GetConfig())
			if !ok {
				continue
			}
			traits := traitsOf(row, isForeignKey[table][column])
			if transformerFits(transformers[source], traits, j.jobType) {
				continue
			}
			j.addColumnError(table, column, fmt.Sprintf(
				"%s does not fit this column; it takes %s",
				transformerName(source), strings.Join(fittingTransformers(traits, j.jobType), ", "),
			), mgmtv1alpha1.ColumnError_COLUMN_ERROR_CODE_TRANSFORMER_NOT_ALLOWED)
		}
	}
}
