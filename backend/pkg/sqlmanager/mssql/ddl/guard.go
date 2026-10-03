package ddl

// Each object is created behind a test of its own existence, by its own name and, for what
// belongs to a table, by that table. A statement is therefore safe to run again, and a name
// taken by an object of another kind or of another table lets the statement run and fail.

// tableID is the expression that gives the id of a table, or NULL when it does not exist.
func tableID(schema, table string) string {
	return objectID(schema, table, TypeTable)
}

// objectID is the expression that gives the id of an object of a type, or NULL.
func objectID(schema, name, typeCode string) string {
	return "OBJECT_ID(" + QuoteLiteral(QualifiedName(schema, name)) + ", " + QuoteLiteral(typeCode) + ")"
}

// guarded puts a statement behind its guard: both make one batch.
func guarded(guard, statement string) string {
	return guard + "\n" + statement
}

func schemaMissing(name string) string {
	return "IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = " + QuoteLiteral(name) + ")"
}

func typeMissing(schema, name string) string {
	return "IF TYPE_ID(" + QuoteLiteral(QualifiedName(schema, name)) + ") IS NULL"
}

func objectMissing(schema, name, typeCode string) string {
	return "IF " + objectID(schema, name, typeCode) + " IS NULL"
}

// constraintMissing tests a constraint of a table, whatever its kind: the names of the
// constraints of one table are unique.
func constraintMissing(schema, table, name string) string {
	return "IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = " + QuoteLiteral(name) +
		" AND parent_object_id = " + tableID(schema, table) + ")"
}

func indexMissing(schema, table, name string) string {
	return "IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = " + QuoteLiteral(name) +
		" AND object_id = " + tableID(schema, table) + ")"
}

// The guards below hold a statement that disables an object for as long as it is enabled.

func constraintEnabled(view, schema, table, name string) string {
	return "IF EXISTS (SELECT 1 FROM " + view + " WHERE name = " + QuoteLiteral(name) +
		" AND parent_object_id = " + tableID(schema, table) + " AND is_disabled = 0)"
}

func indexEnabled(schema, table, name string) string {
	return "IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = " + QuoteLiteral(name) +
		" AND object_id = " + tableID(schema, table) + " AND is_disabled = 0)"
}

func triggerEnabled(schema, name string) string {
	return "IF EXISTS (SELECT 1 FROM sys.triggers WHERE object_id = " + objectID(schema, name, TypeTrigger) +
		" AND is_disabled = 0)"
}

// notVersioned holds the statement that turns system versioning on.
func notVersioned(schema, table string) string {
	return "IF EXISTS (SELECT 1 FROM sys.tables WHERE object_id = " + tableID(schema, table) +
		" AND temporal_type = 0)"
}
