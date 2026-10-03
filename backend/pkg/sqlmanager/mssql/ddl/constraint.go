package ddl

import (
	"cmp"
	"slices"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// alterTable opens a statement that adds a constraint to a table.
func alterTable(table *Table) string {
	return "ALTER TABLE " + QualifiedName(table.Schema, table.Name)
}

// addKey writes a primary key or a unique constraint from the index that backs it.
func addKey(table *Table, index *Index) *sqlmanager_shared.AlterTableStatement {
	kind, constraintType := "UNIQUE", sqlmanager_shared.UniqueConstraintType
	if index.IsPrimaryKey {
		kind, constraintType = "PRIMARY KEY", sqlmanager_shared.PrimaryConstraintType
	}
	return &sqlmanager_shared.AlterTableStatement{
		ConstraintType: constraintType,
		Statement: guarded(
			constraintMissing(table.Schema, table.Name, index.Name),
			alterTable(table)+" ADD CONSTRAINT "+QuoteIdentifier(index.Name)+" "+kind+" "+
				clustering(index)+" "+keyColumns(index)+indexOptions(index),
		),
	}
}

// trust tells whether the rows a table holds are checked against a constraint added to it.
func trust(notTrusted bool) string {
	if notTrusted {
		return " WITH NOCHECK"
	}
	return " WITH CHECK"
}

// disableConstraint disables a check or a foreign key for as long as it is enabled. view is the
// catalog view that tells its state.
func disableConstraint(
	table *Table,
	view, name string,
	constraintType sqlmanager_shared.ConstraintType,
) *sqlmanager_shared.AlterTableStatement {
	return &sqlmanager_shared.AlterTableStatement{
		ConstraintType: constraintType,
		Statement: guarded(
			constraintEnabled(view, table.Schema, table.Name, name),
			alterTable(table)+" NOCHECK CONSTRAINT "+QuoteIdentifier(name),
		),
	}
}

// addChecks writes the check constraints of a table by name, each followed by the statement
// that disables it when the source's is disabled.
func addChecks(table *Table) []*sqlmanager_shared.AlterTableStatement {
	checks := slices.Clone(table.Checks)
	slices.SortFunc(checks, func(a, b *CheckConstraint) int { return cmp.Compare(a.Name, b.Name) })

	statements := []*sqlmanager_shared.AlterTableStatement{}
	for _, check := range checks {
		replication := ""
		if check.IsNotForReplication {
			replication = "NOT FOR REPLICATION "
		}
		statements = append(statements, &sqlmanager_shared.AlterTableStatement{
			ConstraintType: sqlmanager_shared.CheckConstraintType,
			Statement: guarded(
				constraintMissing(table.Schema, table.Name, check.Name),
				alterTable(table)+trust(check.IsNotTrusted)+" ADD CONSTRAINT "+QuoteIdentifier(check.Name)+
					" CHECK "+replication+check.Definition,
			),
		})
		if check.IsDisabled {
			statements = append(statements, disableConstraint(
				table, "sys.check_constraints", check.Name, sqlmanager_shared.CheckConstraintType,
			))
		}
	}
	return statements
}

var referentialActions = map[int]string{
	ActionCascade:    "CASCADE",
	ActionSetNull:    "SET NULL",
	ActionSetDefault: "SET DEFAULT",
}

// addForeignKey writes a foreign key, followed by the statement that disables it when the
// source's is disabled.
func addForeignKey(table *Table, key *ForeignKey) []*sqlmanager_shared.AlterTableStatement {
	columns := make([]string, len(key.Columns))
	referenced := make([]string, len(key.Columns))
	for i, column := range key.Columns {
		columns[i] = column.Name
		referenced[i] = column.ReferencedName
	}
	statement := alterTable(table) + trust(key.IsNotTrusted) + " ADD CONSTRAINT " + QuoteIdentifier(key.Name) +
		" FOREIGN KEY (" + quoteIdentifiers(columns) + ") REFERENCES " +
		QualifiedName(key.ReferencedSchema, key.ReferencedTable) + " (" + quoteIdentifiers(referenced) + ")"
	if action, ok := referentialActions[key.DeleteAction]; ok {
		statement += " ON DELETE " + action
	}
	if action, ok := referentialActions[key.UpdateAction]; ok {
		statement += " ON UPDATE " + action
	}
	if key.IsNotForReplication {
		statement += " NOT FOR REPLICATION"
	}

	statements := []*sqlmanager_shared.AlterTableStatement{{
		ConstraintType: sqlmanager_shared.ForeignConstraintType,
		Statement:      guarded(constraintMissing(table.Schema, table.Name, key.Name), statement),
	}}
	if key.IsDisabled {
		statements = append(statements, disableConstraint(
			table, "sys.foreign_keys", key.Name, sqlmanager_shared.ForeignConstraintType,
		))
	}
	return statements
}
