// Package ddl turns what the catalog of a SQL Server database says of a set of tables into the
// T-SQL that creates them again. It reads nothing and writes nothing: a snapshot goes in, a
// plan comes out.
package ddl

import (
	"strings"

	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
)

// QuoteIdentifier returns name between brackets, each ] doubled.
func QuoteIdentifier(name string) string {
	return sqlident.SQLServer.Quote(name)
}

// QuoteLiteral returns value as a Unicode literal N'…', each ' doubled and a backslash
// before a line break split so that T-SQL keeps it.
func QuoteLiteral(value string) string {
	return sqlident.SQLServer.Literal(value)
}

// QualifiedName returns [schema].[name].
func QualifiedName(schema, name string) string {
	return sqlident.SQLServer.Qualified(schema, name)
}

// quoteIdentifiers returns the names, each quoted, separated by a comma and a space.
func quoteIdentifiers(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = QuoteIdentifier(name)
	}
	return strings.Join(quoted, ", ")
}
