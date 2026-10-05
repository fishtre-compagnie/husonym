// Package ddl turns what the catalog of a SQL Server database says of a set of tables into the
// T-SQL that creates them again. It reads nothing and writes nothing: a snapshot goes in, a
// plan comes out.
package ddl

import "strings"

// QuoteIdentifier returns name between brackets, each ] doubled.
func QuoteIdentifier(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// QuoteLiteral returns value as a Unicode literal N'…', each ' doubled.
func QuoteLiteral(value string) string {
	return "N'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// QualifiedName returns [schema].[name].
func QualifiedName(schema, name string) string {
	return QuoteIdentifier(schema) + "." + QuoteIdentifier(name)
}

// quoteIdentifiers returns the names, each quoted, separated by a comma and a space.
func quoteIdentifiers(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = QuoteIdentifier(name)
	}
	return strings.Join(quoted, ", ")
}
