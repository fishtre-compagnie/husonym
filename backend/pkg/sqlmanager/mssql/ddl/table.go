package ddl

import (
	"fmt"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// createTable writes a table with its columns and its period: no constraint but the defaults,
// of which a column has one at most.
func createTable(table *Table) string {
	lines := make([]string, 0, len(table.Columns)+1)
	for _, column := range table.Columns {
		lines = append(lines, "    "+columnDefinition(column))
	}
	if table.PeriodStartColumn != "" {
		lines = append(lines, fmt.Sprintf(
			"    PERIOD FOR SYSTEM_TIME (%s, %s)",
			QuoteIdentifier(table.PeriodStartColumn), QuoteIdentifier(table.PeriodEndColumn),
		))
	}
	name := QualifiedName(table.Schema, table.Name)
	return guarded(
		objectMissing(table.Schema, table.Name, TypeTable),
		"CREATE TABLE "+name+" (\n"+strings.Join(lines, ",\n")+"\n)",
	)
}

// columnDefinition writes a column, its parts in the order of the CREATE TABLE grammar.
func columnDefinition(column *Column) string {
	var b strings.Builder
	b.WriteString(QuoteIdentifier(column.Name))
	if column.IsComputed {
		b.WriteString(" AS " + column.ComputedDefinition)
		if column.IsPersisted {
			b.WriteString(" PERSISTED")
			if !column.IsNullable {
				b.WriteString(" NOT NULL")
			}
		}
		return b.String()
	}

	b.WriteString(" " + columnType(column))
	if column.IsColumnSet {
		b.WriteString(" COLUMN_SET FOR ALL_SPARSE_COLUMNS")
	}
	if writesCollation(column) {
		b.WriteString(" COLLATE " + column.Collation)
	}
	if column.IsSparse {
		b.WriteString(" SPARSE")
	}
	if column.IsMasked {
		// The catalog keeps the function the way it was written inside its literal, quotes
		// doubled: it goes back between quotes as it is.
		b.WriteString(" MASKED WITH (FUNCTION = N'" + column.MaskingFunction + "')")
	}
	if column.HasDefault {
		b.WriteString(" CONSTRAINT " + QuoteIdentifier(column.DefaultName) + " DEFAULT " + column.DefaultDefinition)
	}
	if column.IsIdentity {
		b.WriteString(" IDENTITY(" + column.IdentitySeed + "," + column.IdentityIncrement + ")")
		if column.IdentityNotForReplication {
			b.WriteString(" NOT FOR REPLICATION")
		}
	}
	switch column.GeneratedAlways {
	case GeneratedRowStart:
		b.WriteString(" GENERATED ALWAYS AS ROW START")
	case GeneratedRowEnd:
		b.WriteString(" GENERATED ALWAYS AS ROW END")
	}
	if column.GeneratedAlways != GeneratedNot && column.IsHidden {
		b.WriteString(" HIDDEN")
	}
	if column.IsNullable {
		b.WriteString(" NULL")
	} else {
		b.WriteString(" NOT NULL")
	}
	if column.IsRowGuidCol {
		b.WriteString(" ROWGUIDCOL")
	}
	return b.String()
}

// takesCollation tells a column whose definition may carry a COLLATE clause. An alias type takes
// none: its columns follow the database. A computed column is written by its expression.
func takesCollation(column *Column) bool {
	return column.Collation != "" && !column.IsUserDefinedType && !column.IsComputed
}

// writesCollation tells a column whose definition carries its COLLATE clause: one that takes
// the clause, and whose collation name can be written.
func writesCollation(column *Column) bool {
	return takesCollation(column) && isCollationName(column.Collation)
}

// isCollationName tells a name made of ASCII letters, digits and underscores, the shape of
// every collation name. SQL Server takes no quoted form after COLLATE, so no other name can be
// written there.
func isCollationName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// versioning turns system versioning on, on the history table the source names.
func versioning(table *Table) string {
	retention := "INFINITE"
	if table.RetentionPeriod > 0 {
		retention = fmt.Sprintf("%d %s", table.RetentionPeriod, table.RetentionUnit)
		if table.RetentionPeriod > 1 {
			retention += "S"
		}
	}
	return guarded(
		notVersioned(table.Schema, table.Name),
		fmt.Sprintf(
			"ALTER TABLE %s SET (SYSTEM_VERSIONING = ON (HISTORY_TABLE = %s, HISTORY_RETENTION_PERIOD = %s))",
			QualifiedName(table.Schema, table.Name),
			QualifiedName(table.HistorySchema, table.HistoryName),
			retention,
		),
	)
}

// aliasType writes an alias type from its base type and the numbers a column or a sequence of
// that type carries.
func aliasType(schema, name, baseType string, maxLength, precision, scale int, nullable bool) *sqlmanager_shared.DataType {
	base := systemType(baseType, maxLength, precision, scale)
	nullability := "NOT NULL"
	if nullable {
		nullability = "NULL"
	}
	return &sqlmanager_shared.DataType{
		Schema: schema,
		Name:   name,
		Definition: guarded(
			typeMissing(schema, name),
			"CREATE TYPE "+QualifiedName(schema, name)+" FROM "+base+" "+nullability,
		),
	}
}

// createSchema writes a schema: CREATE SCHEMA opens its batch, so it runs in one of its own.
func createSchema(name string) string {
	return guarded(
		schemaMissing(name),
		"EXEC ("+QuoteLiteral("CREATE SCHEMA "+QuoteIdentifier(name))+")",
	)
}
