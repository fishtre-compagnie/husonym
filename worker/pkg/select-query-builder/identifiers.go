package selectquerybuilder

import (
	"fmt"

	"github.com/doug-martin/goqu/v9/exp"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
)

// checkTable refuses a schema or a table name that no engine takes. A table may have no
// schema: it is then written alone.
func checkTable(schema, table string) error {
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

func checkColumns(columns ...[]string) error {
	for _, names := range columns {
		for _, column := range names {
			if err := sqlident.Check(column); err != nil {
				return fmt.Errorf("column name: %w", err)
			}
		}
	}
	return nil
}

// table gives a table under the alias the query names it by.
func (qb *QueryBuilder) table(schema, table, alias string) exp.AliasedExpression {
	return qb.ident.Table(schema, table).As(qb.ident.Table("", alias))
}

// tableOfKey gives the table a table key names, under an alias.
//
// The join steps of a subset path carry their tables as keys only. A key is cut on its
// dots the way goqu cuts a name it is given as a string, which is how this builder has
// always read them: two parts are a schema and a table, three parts are three identifiers,
// and anything else is one name. Each part is then written as one identifier.
func (qb *QueryBuilder) tableOfKey(key, alias string) (exp.AliasedExpression, error) {
	parts := exp.ParseIdentifier(key)
	// goqu keeps the last part of a key aside when it is a star.
	last, ok := parts.GetCol().(string)
	if !ok {
		last = "*"
	}
	var table exp.IdentifierExpression
	switch {
	case parts.GetSchema() != "":
		if err := sqlident.Check(last); err != nil {
			return nil, fmt.Errorf("table name: %w", err)
		}
		table = qb.ident.Table(parts.GetSchema(), parts.GetTable()).Col(qb.ident.Col(last).GetCol())
	case parts.GetTable() != "":
		if err := checkTable(parts.GetTable(), last); err != nil {
			return nil, err
		}
		table = qb.ident.Table(parts.GetTable(), last)
	default:
		if err := checkTable("", key); err != nil {
			return nil, err
		}
		table = qb.ident.Table("", key)
	}
	return table.As(qb.ident.Table("", alias)), nil
}
