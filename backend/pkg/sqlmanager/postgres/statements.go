package sqlmanager_postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/doug-martin/goqu/v9"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	schemamanager_shared "github.com/fishtre-compagnie/husonym/internal/schema-manager/shared"
)

// pg writes the names and the string values of the statements of this package.
const pg = sqlident.Postgres

// checkTableName refuses a schema or a table name that no engine takes. A table may have no
// schema: it is then written alone.
func checkTableName(schema, table string) error {
	if schema != "" {
		if err := sqlident.Check(schema); err != nil {
			return fmt.Errorf("schema name: %w", err)
		}
	}
	if err := sqlident.Check(table); err != nil {
		return fmt.Errorf("table name: %w", err)
	}
	return nil
}

func checkNames(kind string, names ...string) error {
	for _, name := range names {
		if err := sqlident.Check(name); err != nil {
			return fmt.Errorf("%s name: %w", kind, err)
		}
	}
	return nil
}

// Finds any schemas referenced in datatypes that don't exist in tables and returns the statements to create them
func getSchemaCreationStatementsFromDataTypes(
	tables []*sqlmanager_shared.SchemaTable,
	datatypes *sqlmanager_shared.SchemaTableDataTypeResponse,
) []string {
	schemaStmts := []string{}
	schemaSet := map[string]struct{}{}
	for _, table := range tables {
		schemaSet[table.Schema] = struct{}{}
	}

	// Check each datatype schema against the table schemas
	for _, composite := range datatypes.Composites {
		if _, exists := schemaSet[composite.Schema]; !exists {
			schemaStmts = append(schemaStmts, buildCreateSchemaStatement(composite.Schema))
			schemaSet[composite.Schema] = struct{}{}
		}
	}

	for _, enum := range datatypes.Enums {
		if _, exists := schemaSet[enum.Schema]; !exists {
			schemaStmts = append(schemaStmts, buildCreateSchemaStatement(enum.Schema))
			schemaSet[enum.Schema] = struct{}{}
		}
	}

	for _, domain := range datatypes.Domains {
		if _, exists := schemaSet[domain.Schema]; !exists {
			schemaStmts = append(schemaStmts, buildCreateSchemaStatement(domain.Schema))
			schemaSet[domain.Schema] = struct{}{}
		}
	}
	return schemaStmts
}

func buildCreateSchemaStatement(schema string) string {
	return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", pg.Quote(schema))
}

// dollarQuoteTag gives the delimiter of a dollar-quoted body: $$ when the body does not hold
// it, and otherwise a tagged delimiter that the body does not hold.
func dollarQuoteTag(body string) string {
	if !strings.Contains(body, "$$") {
		return "$$"
	}
	tag := "$husonym$"
	for i := 1; strings.Contains(body, tag); i++ {
		tag = fmt.Sprintf("$husonym%d$", i)
	}
	return tag
}

// doBlock writes a DO statement around a body, closed by a delimiter the body does not hold.
func doBlock(body string) string {
	tag := dollarQuoteTag(body)
	return "DO " + tag + body + tag + ";"
}

func wrapPgIdempotentIndex(
	schema,
	constraintname,
	alterStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind in ('i', 'I')
		AND c.relname = %s
		AND n.nspname = %s
	) THEN
		%s
	END IF;
END `, pg.Literal(constraintname), pg.Literal(schema), addSuffixIfNotExist(alterStatement, ";")))
}

func wrapPgIdempotentConstraint(
	schema, table,
	constraintName,
	alterStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = %s
		AND connamespace = (SELECT oid FROM pg_namespace WHERE nspname = %s)
		AND conrelid = (
			SELECT oid
			FROM pg_class
			WHERE relname = %s
			AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = %s)
		)
	) THEN
		%s
	END IF;
END `, pg.Literal(constraintName), pg.Literal(schema), pg.Literal(table), pg.Literal(schema),
		addSuffixIfNotExist(alterStatement, ";")))
}

func wrapPgIdempotentSequence(
	schema,
	sequenceName,
	createStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relkind = 'S'
        AND c.relname = %s
        AND n.nspname = %s
    ) THEN
        %s
    END IF;
END `, pg.Literal(sequenceName), pg.Literal(schema), addSuffixIfNotExist(createStatement, ";")))
}

func wrapPgIdempotentTrigger(
	schema,
	tableName,
	triggerName,
	createStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger t
        JOIN pg_class c ON c.oid = t.tgrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE t.tgname = %s
        AND c.relname = %s
        AND n.nspname = %s
    ) THEN
        %s
    END IF;
END `, pg.Literal(triggerName), pg.Literal(tableName), pg.Literal(schema),
		addSuffixIfNotExist(createStatement, ";")))
}

func wrapPgIdempotentFunction(
	schema,
	functionName,
	functionSignature,
	createStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE p.proname = %s
        AND n.nspname = %s
        AND pg_catalog.pg_get_function_identity_arguments(p.oid) = %s
    ) THEN
        %s
    END IF;
END `, pg.Literal(functionName), pg.Literal(schema), pg.Literal(functionSignature),
		addSuffixIfNotExist(createStatement, ";")))
}

func wrapPgIdempotentDataType(
	schema,
	dataTypeName,
	createStatement string,
) string {
	return doBlock(fmt.Sprintf(`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type t
        JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE t.typname = %s
        AND n.nspname = %s
    ) THEN
        %s
    END IF;
END `, pg.Literal(dataTypeName), pg.Literal(schema), addSuffixIfNotExist(createStatement, ";")))
}

// wrapPgIdempotentExtension writes the version as an identifier, a form CREATE EXTENSION
// takes for it.
func wrapPgIdempotentExtension(
	schema sql.NullString,
	extensionName,
	version string,
) string {
	if schema.Valid && strings.EqualFold(schema.String, "public") {
		return fmt.Sprintf(
			`CREATE EXTENSION IF NOT EXISTS %s VERSION %s;`,
			pg.Quote(extensionName),
			pg.Quote(version),
		)
	}
	return fmt.Sprintf(
		`CREATE EXTENSION IF NOT EXISTS %s VERSION %s SCHEMA %s;`,
		pg.Quote(extensionName),
		pg.Quote(version),
		pg.Quote(schema.String),
	)
}

//nolint:unparam
func addSuffixIfNotExist(input, suffix string) string {
	if !strings.HasSuffix(input, suffix) {
		return fmt.Sprintf("%s%s", input, suffix)
	}
	return input
}

func buildAlterStatementByForeignKeyConstraint(
	constraint *pg_queries.GetForeignKeyConstraintsBySchemasRow,
) (string, error) {
	if constraint == nil {
		return "", errors.New("unable to build alter statement as constraint is nil")
	}
	if err := checkTableName(constraint.ReferencingSchema, constraint.ReferencingTable); err != nil {
		return "", err
	}
	if err := checkTableName(constraint.ReferencedSchema, constraint.ReferencedTable); err != nil {
		return "", err
	}
	if err := checkNames("constraint", constraint.ConstraintName); err != nil {
		return "", err
	}
	if err := checkNames("column", constraint.ReferencingColumns...); err != nil {
		return "", err
	}
	if err := checkNames("column", constraint.ReferencedColumns...); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s);",
		pg.Qualified(constraint.ReferencingSchema, constraint.ReferencingTable),
		pg.Quote(constraint.ConstraintName),
		strings.Join(EscapePgColumns(constraint.ReferencingColumns), ", "),
		pg.Qualified(constraint.ReferencedSchema, constraint.ReferencedTable),
		strings.Join(EscapePgColumns(constraint.ReferencedColumns), ", "),
	), nil
}

func buildAlterStatementByConstraint(
	constraint *pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow,
) (string, error) {
	if constraint == nil {
		return "", errors.New("unable to build alter statement as constraint is nil")
	}
	if err := checkTableName(constraint.SchemaName, constraint.TableName); err != nil {
		return "", err
	}
	if err := checkNames("constraint", constraint.ConstraintName); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s %s;",
		pg.Qualified(constraint.SchemaName, constraint.TableName),
		pg.Quote(constraint.ConstraintName),
		constraint.ConstraintDefinition,
	), nil
}

func BuildAddColumnStatement(column *sqlmanager_shared.TableColumn) string {
	col := buildTableCol(&buildTableColRequest{
		ColumnName:         column.Name,
		ColumnDefault:      column.ColumnDefault,
		DataType:           column.DataType,
		IsNullable:         column.IsNullable,
		GeneratedType:      *column.GeneratedType,
		SequenceDefinition: column.SequenceDefinition,
	})
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", pg.Qualified(column.Schema, column.Table), col)
}

func BuildRenameColumnStatement(column *schemamanager_shared.ColumnDiff) string {
	return fmt.Sprintf(
		"ALTER TABLE %s RENAME COLUMN %s TO %s;",
		pg.Qualified(column.Column.Schema, column.Column.Table),
		pg.Quote(column.RenameColumn.OldName),
		pg.Quote(column.Column.Name),
	)
}

func BuildAlterColumnStatement(column *schemamanager_shared.ColumnDiff) []string {
	statements := []string{}
	pieces := []string{}

	base := "ALTER COLUMN " + pg.Quote(column.Column.Name)
	for _, action := range column.Actions {
		switch action {
		case schemamanager_shared.SetDatatype:
			pieces = append(
				pieces,
				fmt.Sprintf(
					"%s TYPE %s USING %s::%s",
					base,
					column.Column.DataType,
					pg.Quote(column.Column.Name),
					column.Column.DataType,
				),
			)
		case schemamanager_shared.DropNotNull:
			pieces = append(pieces, fmt.Sprintf("%s DROP NOT NULL", base))
		case schemamanager_shared.SetNotNull:
			pieces = append(pieces, fmt.Sprintf("%s SET NOT NULL", base))
		case schemamanager_shared.DropDefault:
			pieces = append(pieces, fmt.Sprintf("%s DROP DEFAULT", base))
		case schemamanager_shared.SetDefault:
			if column.Column.GeneratedType != nil && *column.Column.GeneratedType == "s" {
				// generated columns can't be updated. need to drop then recreate
				dropStmt := BuildDropColumnStatement(
					column.Column.Schema,
					column.Column.Table,
					column.Column.Name,
				)
				createStmt := BuildAddColumnStatement(column.Column)
				statements = append(statements, dropStmt, createStmt)
			} else {
				pieces = append(pieces, fmt.Sprintf("%s SET DEFAULT %s", base, column.Column.ColumnDefault))
			}
		case schemamanager_shared.DropIdentity:
			pieces = append(pieces, fmt.Sprintf("%s DROP IDENTITY IF EXISTS", base))
		case schemamanager_shared.SetComment:
			statements = append(
				statements,
				BuildUpdateCommentStatement(
					column.Column.Schema,
					column.Column.Table,
					column.Column.Name,
					column.Column.Comment,
				),
			)
		}
	}

	if len(pieces) > 0 {
		alterStatement := fmt.Sprintf(
			"ALTER TABLE %s %s;",
			pg.Qualified(column.Column.Schema, column.Column.Table),
			strings.Join(pieces, ", "),
		)
		statements = append(statements, alterStatement)
	}

	return statements
}

func BuildDropColumnStatement(schema, table, column string) string {
	// cascade is used to drop the column and all the constraints, views, and indexes that depend on it
	return fmt.Sprintf(
		"ALTER TABLE %s DROP COLUMN IF EXISTS %s CASCADE;",
		pg.Qualified(schema, table),
		pg.Quote(column),
	)
}

func BuildDropConstraintStatement(schema, table, constraintName string) string {
	// cascade is used to drop the constraint and any dependent objects (other constraints, indexes, triggers, etc)
	return fmt.Sprintf(
		"ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s CASCADE;",
		pg.Qualified(schema, table),
		pg.Quote(constraintName),
	)
}

func BuildDropTriggerStatement(schema, table, triggerName string) string {
	return fmt.Sprintf(
		"DROP TRIGGER IF EXISTS %s ON %s;",
		pg.Quote(triggerName),
		pg.Qualified(schema, table),
	)
}

func BuildDropFunctionStatement(schema, functionName string) string {
	return fmt.Sprintf("DROP FUNCTION IF EXISTS %s;", pg.Qualified(schema, functionName))
}

func BuildUpdateFunctionStatement(schema, functionName, createStatement string) string {
	if strings.Contains(strings.ToUpper(createStatement), "CREATE FUNCTION") &&
		!strings.Contains(strings.ToUpper(createStatement), "CREATE OR REPLACE FUNCTION") {
		createStatement = strings.Replace(
			strings.ToUpper(createStatement),
			"CREATE FUNCTION",
			"CREATE OR REPLACE FUNCTION",
			1,
		)
	}
	return createStatement
}

func BuildDropDatatypesStatement(schema, enumName string) string {
	return fmt.Sprintf("DROP TYPE IF EXISTS %s;", pg.Qualified(schema, enumName))
}

// BuildUpdateEnumStatements writes the labels as it receives them: they are values.
func BuildUpdateEnumStatements(
	schema, enumName string,
	newValues []string,
	changedValues map[string]string,
) []string {
	enum := pg.Qualified(schema, enumName)
	statements := []string{}
	for _, value := range newValues {
		statements = append(
			statements,
			fmt.Sprintf("ALTER TYPE %s ADD VALUE IF NOT EXISTS '%s';", enum, value),
		)
	}
	for value, newVal := range changedValues {
		statements = append(
			statements,
			fmt.Sprintf("ALTER TYPE %s RENAME VALUE '%s' TO '%s';", enum, value, newVal),
		)
	}
	return statements
}

func BuildUpdateCompositeStatements(
	schema, compositeName string,
	changedAttributesDatatype, changedAttributesName, newAttributes map[string]string,
	removedAttributes []string,
) []string {
	composite := pg.Qualified(schema, compositeName)
	statements := []string{}
	for attribute, newDatatype := range changedAttributesDatatype {
		statements = append(
			statements,
			fmt.Sprintf(
				"ALTER TYPE %s ALTER ATTRIBUTE %s SET DATA TYPE '%s';",
				composite,
				pg.Quote(attribute),
				newDatatype,
			),
		)
	}
	for oldName, newName := range changedAttributesName {
		statements = append(
			statements,
			fmt.Sprintf(
				"ALTER TYPE %s RENAME ATTRIBUTE %s TO  %s;",
				composite,
				pg.Quote(oldName),
				pg.Quote(newName),
			),
		)
	}
	for attribute, datatype := range newAttributes {
		statements = append(
			statements,
			fmt.Sprintf(
				"ALTER TYPE %s ADD ATTRIBUTE %s %s;",
				composite,
				pg.Quote(attribute),
				datatype,
			),
		)
	}
	for _, attribute := range removedAttributes {
		statements = append(
			statements,
			fmt.Sprintf(
				"ALTER TYPE %s DROP ATTRIBUTE IF EXISTS %s;",
				composite,
				pg.Quote(attribute),
			),
		)
	}
	return statements
}

func BuildDropDomainStatement(schema, domainName string) string {
	return fmt.Sprintf("DROP DOMAIN IF EXISTS %s;", pg.Qualified(schema, domainName))
}

func BuildDomainConstraintStatements(
	schema, domainName string,
	newConstraints map[string]string,
	removedConstraints []string,
) []string {
	domain := pg.Qualified(schema, domainName)
	statements := []string{}
	// The removed constraints first: one whose definition changed is removed, and added anew
	// under the same name.
	for _, constraint := range slices.Sorted(slices.Values(removedConstraints)) {
		statements = append(
			statements,
			fmt.Sprintf("ALTER DOMAIN %s DROP CONSTRAINT IF EXISTS %s;", domain, pg.Quote(constraint)),
		)
	}
	for _, constraint := range slices.Sorted(maps.Keys(newConstraints)) {
		statements = append(
			statements,
			fmt.Sprintf(
				"ALTER DOMAIN %s ADD CONSTRAINT %s %s;",
				domain,
				pg.Quote(constraint),
				newConstraints[constraint],
			),
		)
	}
	return statements
}

func BuildUpdateDomainDefaultStatement(schema, domainName, defaultString string) string {
	return fmt.Sprintf(
		"ALTER DOMAIN %s SET DEFAULT %s;",
		pg.Qualified(schema, domainName),
		defaultString,
	)
}

func BuildDropDomainDefaultStatement(schema, domainName string) string {
	return fmt.Sprintf("ALTER DOMAIN %s DROP DEFAULT;", pg.Qualified(schema, domainName))
}

func BuildUpdateDomainNotNullStatement(schema, domainName string, isNullable bool) string {
	action := "SET"
	if isNullable {
		action = "DROP"
	}
	return fmt.Sprintf("ALTER DOMAIN %s %s NOT NULL;", pg.Qualified(schema, domainName), action)
}

type buildTableColRequest struct {
	ColumnName         string
	ColumnDefault      string
	DataType           string
	IsNullable         bool
	GeneratedType      string
	SequenceDefinition *string
	Sequence           *SequenceConfiguration
}

type SequenceConfiguration struct {
	IncrementBy int64
	MinValue    int64
	MaxValue    int64
	StartValue  int64
	CacheValue  int64
	CycleOption bool
}

func (s *SequenceConfiguration) ToGeneratedDefaultIdentity() string {
	return fmt.Sprintf("GENERATED BY DEFAULT AS IDENTITY ( %s )", s.identitySequenceConfiguration())
}
func (s *SequenceConfiguration) ToGeneratedAlwaysIdentity() string {
	return fmt.Sprintf("GENERATED ALWAYS AS IDENTITY ( %s )", s.identitySequenceConfiguration())
}

func (s *SequenceConfiguration) identitySequenceConfiguration() string {
	return fmt.Sprintf("INCREMENT BY %d MINVALUE %d MAXVALUE %d START %d CACHE %d %s",
		s.IncrementBy, s.MinValue, s.MaxValue, s.StartValue, s.CacheValue, s.toCycelText(),
	)
}

func (s *SequenceConfiguration) toCycelText() string {
	if s.CycleOption {
		return "CYCLE"
	}
	return "NO CYCLE"
}

func BuildSequencOwnerStatement(seq *pg_queries.GetSequencesOwnedByTablesRow) string {
	return fmt.Sprintf(
		"ALTER SEQUENCE %s OWNED BY %s.%s;",
		pg.Qualified(seq.SequenceSchema, seq.SequenceName),
		pg.Qualified(seq.TableSchema, seq.TableName),
		pg.Quote(seq.ColumnName),
	)
}

func buildTableCol(record *buildTableColRequest) string {
	pieces := []string{
		EscapePgColumn(record.ColumnName),
		record.DataType,
		buildNullableText(record.IsNullable),
	}

	if record.SequenceDefinition != nil && *record.SequenceDefinition != "" {
		pieces = append(pieces, *record.SequenceDefinition)
	} else if record.ColumnDefault != "" {
		if record.GeneratedType == "s" {
			pieces = append(pieces, fmt.Sprintf("GENERATED ALWAYS AS (%s) STORED", record.ColumnDefault))
		} else if record.ColumnDefault != "NULL" {
			pieces = append(pieces, "DEFAULT", record.ColumnDefault)
		}
	}
	return strings.Join(pieces, " ")
}

func buildSequenceDefinition(identityType string, seqConfig *SequenceConfiguration) string {
	var seqStr string
	switch identityType {
	case "d":
		seqStr = seqConfig.ToGeneratedDefaultIdentity()
	case "a":
		seqStr = seqConfig.ToGeneratedAlwaysIdentity()
	}
	return seqStr
}

func BuildUpdateCommentStatement(schema, table, column string, comment *string) string {
	target := pg.Qualified(schema, table) + "." + pg.Quote(column)
	if comment == nil || *comment == "" {
		return fmt.Sprintf("COMMENT ON COLUMN %s IS NULL;", target)
	}
	return fmt.Sprintf("COMMENT ON COLUMN %s IS %s;", target, pg.Literal(*comment))
}

func buildNullableText(isNullable bool) string {
	if isNullable {
		return "NULL"
	}
	return "NOT NULL"
}

func getGoquDialect() goqu.DialectWrapper {
	return goqu.Dialect(sqlmanager_shared.GoquPostgresDriver)
}

func BuildPgTruncateStatement(
	tables []*sqlmanager_shared.SchemaTable,
) (string, error) {
	builder := getGoquDialect()
	gTables := []any{}
	for _, t := range tables {
		if err := checkTableName(t.Schema, t.Table); err != nil {
			return "", err
		}
		gTables = append(gTables, pg.Table(t.Schema, t.Table))
	}
	stmt, _, err := builder.From(gTables...).Truncate().Identity("RESTART").ToSQL()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s;", stmt), nil
}

func BuildPgTruncateCascadeStatement(
	schema string,
	table string,
) (string, error) {
	if err := checkTableName(schema, table); err != nil {
		return "", err
	}
	builder := getGoquDialect()
	sqltable := pg.Table(schema, table)
	stmt, _, err := builder.From(sqltable).Truncate().Cascade().Identity("RESTART").ToSQL()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s;", stmt), nil
}

// buildTableRowCountSql counts the rows of a table. The where clause is SQL written by the
// user, and is carried as it is.
func buildTableRowCountSql(schema, table string, whereClause *string) (string, error) {
	if err := checkTableName(schema, table); err != nil {
		return "", err
	}
	query := getGoquDialect().From(pg.Table(schema, table)).Select(goqu.COUNT("*"))
	if whereClause != nil && *whereClause != "" {
		query = query.Where(goqu.L(*whereClause))
	}
	compiledSql, _, err := query.ToSQL()
	if err != nil {
		return "", err
	}
	return compiledSql, nil
}

func EscapePgColumns(cols []string) []string {
	outcols := make([]string, len(cols))
	for idx := range cols {
		outcols[idx] = EscapePgColumn(cols[idx])
	}
	return outcols
}

// EscapePgColumn quotes an identifier: a double quote inside it is doubled, as PostgreSQL
// reads it. Go's %q would escape it with a backslash, and a backslash with another one.
func EscapePgColumn(col string) string {
	return pg.Quote(col)
}

// BuildPgIdentityColumnResetCurrentSql sets the sequence of a column to the highest value
// the column holds. pg_get_serial_sequence takes the table as the text of a qualified name,
// read as an identifier is, and the column as its plain name.
func BuildPgIdentityColumnResetCurrentSql(
	schema, table, column string,
) string {
	qualified := pg.Qualified(schema, table)
	return fmt.Sprintf(
		"SELECT setval(pg_get_serial_sequence(%s, %s), COALESCE((SELECT MAX(%s) FROM %s), 1));",
		pg.Literal(qualified),
		pg.Literal(column),
		pg.Quote(column),
		qualified,
	)
}

// BuildPgInsertIdentityAlwaysSql places OVERRIDING SYSTEM VALUE where the column list of an
// INSERT statement ends: at the first ") VALUES (" that stands outside a quoted identifier
// and outside a string literal. A quote character doubled inside either closes it and opens
// it again at once, so the text between is still read as inside.
//
// A statement without a column list (rows without a column: DEFAULT VALUES) gives no value
// to override, and is returned as it is.
func BuildPgInsertIdentityAlwaysSql(
	insertQuery string,
) string {
	const columnListEnd = ") VALUES ("
	var quote byte
	for i := 0; i < len(insertQuery); i++ {
		c := insertQuery[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case strings.HasPrefix(insertQuery[i:], columnListEnd):
			return insertQuery[:i] + ") OVERRIDING SYSTEM VALUE VALUES(" + insertQuery[i+len(columnListEnd):]
		}
	}
	return insertQuery
}

func BuildPgResetSequenceSql(schema, sequenceName string) string {
	return fmt.Sprintf("ALTER SEQUENCE %s RESTART;", pg.Qualified(schema, sequenceName))
}

func GetPostgresColumnOverrideAndResetProperties(
	columnInfo *sqlmanager_shared.DatabaseSchemaRow,
) (needsOverride, needsReset bool) {
	needsOverride = false
	needsReset = false

	// check if the column is an idenitity type
	if columnInfo.IdentityGeneration != nil && *columnInfo.IdentityGeneration != "" {
		switch *columnInfo.IdentityGeneration {
		case "a": // ALWAYS
			needsOverride = true
			needsReset = true
		case "d": // DEFAULT
			needsReset = true
		}
		return
	}

	// check if column default is sequence
	if columnInfo.ColumnDefault != "" &&
		gotypeutil.CaseInsensitiveContains(columnInfo.ColumnDefault, "nextVal") {
		needsReset = true
		return
	}

	return
}
