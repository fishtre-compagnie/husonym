package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	schemamanager_shared "github.com/fishtre-compagnie/husonym/internal/schema-manager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// oddName is a name with a character that the statements of the manager must carry: how it
// is written as an identifier, as a string literal, and its identifier inside a string literal
// (without the apostrophes around it).
type oddName struct {
	name, ident, lit, identInLit string
}

var oddNames = []oddName{
	{name: `we"ird`, ident: `"we""ird"`, lit: `'we"ird'`, identInLit: `"we""ird"`},
	{name: `o'clock`, ident: `"o'clock"`, lit: `'o''clock'`, identInLit: `"o''clock"`},
	{name: `two$$dollars`, ident: `"two$$dollars"`, lit: `'two$$dollars'`, identInLit: `"two$$dollars"`},
}

// eachOddOrder runs a case three times, so that each of the three names takes each place.
func eachOddOrder(t *testing.T, run func(t *testing.T, a, b, c oddName)) {
	t.Helper()
	for i := range oddNames {
		a, b, c := oddNames[i], oddNames[(i+1)%3], oddNames[(i+2)%3]
		t.Run(fmt.Sprintf("order %d", i), func(t *testing.T) { run(t, a, b, c) })
	}
}

var doBlockPattern = regexp.MustCompile(`(?s)^DO (\$[a-z0-9]*\$)(.*)(\$[a-z0-9]*\$);$`)

// requireDoBlock checks that a statement is a DO block opened and closed by the delimiter
// wanted, and that its body does not hold this delimiter.
func requireDoBlock(t *testing.T, wantDelimiter, statement string) {
	t.Helper()
	require.True(t, strings.HasPrefix(statement, "DO "+wantDelimiter+"\n"), statement)
	require.True(t, strings.HasSuffix(statement, "END "+wantDelimiter+";"), statement)
	body := strings.TrimSuffix(strings.TrimPrefix(statement, "DO "+wantDelimiter), wantDelimiter+";")
	require.NotContains(t, body, wantDelimiter)
	require.Regexp(t, doBlockPattern, statement)
}

// delimiterFor is the delimiter of a block whose body holds these names.
func delimiterFor(names ...oddName) string {
	for _, n := range names {
		if strings.Contains(n.name, "$$") {
			return "$husonym$"
		}
	}
	return "$$"
}

func Test_dollarQuoteTag(t *testing.T) {
	require.Equal(t, "$$", dollarQuoteTag("\nBEGIN\n\tPERFORM 1;\nEND "))
	require.Equal(t, "$$", dollarQuoteTag("\nBEGIN\n\tPERFORM '$husonym$';\nEND "))
	require.Equal(t, "$husonym$", dollarQuoteTag("\nBEGIN\n\tPERFORM 'two$$dollars';\nEND "))
	require.Equal(t, "$husonym1$", dollarQuoteTag("\nBEGIN\n\tPERFORM '$$', '$husonym$';\nEND "))
	require.Equal(t, "$husonym2$", dollarQuoteTag("\nBEGIN\n\tPERFORM '$$', '$husonym$', '$husonym1$';\nEND "))
}

// The delimiter occurs once in the body followed by it, at its end: a body that ends in the
// start of the delimiter does not make the server see another one.
func Test_dollarQuoteTag_OccursOnceAtTheEnd(t *testing.T) {
	for _, c := range []struct{ name, body, tag string }{
		{"ends in a dollar", "BEGIN PERFORM 1; END;--$", "$husonym$"},
		{"ends in the start of the first tag", "BEGIN PERFORM '$$'; END;--$husonym", "$husonym1$"},
		{"ends in the start of the first tag, no two dollars", "BEGIN PERFORM 1; END;--$husonym", "$$"},
		{"holds two dollars and ends in a dollar", "BEGIN PERFORM '$$'; END;--$", "$husonym$"},
		{"holds the first tag", "BEGIN PERFORM '$$', '$husonym$'; END;", "$husonym1$"},
		{"holds the first tag and ends in a dollar", "BEGIN PERFORM '$$', '$husonym$'; END;--$", "$husonym1$"},
		{"holds the first tag and ends in its start", "BEGIN PERFORM '$$', '$husonym$'; END;--$husonym1", "$husonym2$"},
		{"ordinary body", "\nBEGIN\n\tPERFORM 1;\nEND ", "$$"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tag := dollarQuoteTag(c.body)
			require.Equal(t, c.tag, tag)
			require.Equal(t, len(c.body), strings.Index(c.body+tag, tag))
			require.Equal(t, "DO "+tag+c.body+tag+";", doBlock(c.body))
		})
	}
}

func Test_OddNames_SchemaCreation(t *testing.T) {
	for _, n := range oddNames {
		require.Equal(t, "CREATE SCHEMA IF NOT EXISTS "+n.ident+";", buildCreateSchemaStatement(n.name))
	}

	statements := getSchemaCreationStatementsFromDataTypes(
		[]*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "t"}},
		&sqlmanager_shared.SchemaTableDataTypeResponse{
			Composites: []*sqlmanager_shared.DataType{{Schema: `we"ird`}},
			Enums:      []*sqlmanager_shared.DataType{{Schema: `o'clock`}},
			Domains:    []*sqlmanager_shared.DataType{{Schema: `two$$dollars`}},
		},
	)
	require.Equal(t, []string{
		`CREATE SCHEMA IF NOT EXISTS "we""ird";`,
		`CREATE SCHEMA IF NOT EXISTS "o'clock";`,
		`CREATE SCHEMA IF NOT EXISTS "two$$dollars";`,
	}, statements)
}

func Test_OddNames_IdempotentIndex(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, index, table oddName) {
		definition := fmt.Sprintf("CREATE INDEX %s ON %s.%s USING btree (id)", index.ident, schema.ident, table.ident)
		delimiter := "$husonym$" // the three names are in the body
		statement := wrapPgIdempotentIndex(schema.name, index.name, definition)
		require.Equal(t, "DO "+delimiter+`
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind in ('i', 'I')
		AND c.relname = `+index.lit+`
		AND n.nspname = `+schema.lit+`
	) THEN
		`+definition+`;
	END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

func Test_OddNames_IdempotentConstraint(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, constraint oddName) {
		alter := fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s PRIMARY KEY (id);", schema.ident, table.ident, constraint.ident)
		delimiter := "$husonym$"
		statement := wrapPgIdempotentConstraint(schema.name, table.name, constraint.name, alter)
		require.Equal(t, "DO "+delimiter+`
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = `+constraint.lit+`
		AND connamespace = (SELECT oid FROM pg_namespace WHERE nspname = `+schema.lit+`)
		AND conrelid = (
			SELECT oid
			FROM pg_class
			WHERE relname = `+table.lit+`
			AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = `+schema.lit+`)
		)
	) THEN
		`+alter+`
	END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

func Test_OddNames_IdempotentSequence(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, sequence, _ oddName) {
		definition := fmt.Sprintf("CREATE SEQUENCE %s.%s START 1", schema.ident, sequence.ident)
		delimiter := delimiterFor(schema, sequence)
		statement := wrapPgIdempotentSequence(schema.name, sequence.name, definition)
		require.Equal(t, "DO "+delimiter+`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relkind = 'S'
        AND c.relname = `+sequence.lit+`
        AND n.nspname = `+schema.lit+`
    ) THEN
        `+definition+`;
    END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

func Test_OddNames_IdempotentTrigger(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, trigger oddName) {
		definition := fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE INSERT ON %s.%s FOR EACH ROW EXECUTE FUNCTION f()",
			trigger.ident, schema.ident, table.ident,
		)
		delimiter := "$husonym$"
		statement := wrapPgIdempotentTrigger(schema.name, table.name, trigger.name, definition)
		require.Equal(t, "DO "+delimiter+`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger t
        JOIN pg_class c ON c.oid = t.tgrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE t.tgname = `+trigger.lit+`
        AND c.relname = `+table.lit+`
        AND n.nspname = `+schema.lit+`
    ) THEN
        `+definition+`;
    END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

func Test_OddNames_IdempotentFunction(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, function, argument oddName) {
		signature := argument.ident + " integer"
		definition := fmt.Sprintf(
			"CREATE OR REPLACE FUNCTION %s.%s(%s)\n RETURNS integer\n LANGUAGE sql\nAS $function$ SELECT 1 $function$\n",
			schema.ident, function.ident, signature,
		)
		delimiter := "$husonym$"
		statement := wrapPgIdempotentFunction(schema.name, function.name, signature, definition)
		require.Equal(t, "DO "+delimiter+`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE p.proname = `+function.lit+`
        AND n.nspname = `+schema.lit+`
        AND pg_catalog.pg_get_function_identity_arguments(p.oid) = '`+argument.identInLit+` integer'
    ) THEN
        `+definition+`;
    END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

// The body of a function may be delimited by $$, and may hold the tagged delimiter too: the
// block that carries it is closed by a delimiter that neither is.
func Test_IdempotentFunction_BodyHoldingDelimiters(t *testing.T) {
	definition := "CREATE FUNCTION app.f() RETURNS text LANGUAGE sql AS $$ SELECT '$husonym$' $$"
	statement := wrapPgIdempotentFunction("app", "f", "", definition)
	requireDoBlock(t, "$husonym1$", statement)
	require.Contains(t, statement, definition+";")
}

func Test_OddNames_IdempotentDataType(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, datatype, _ oddName) {
		definition := fmt.Sprintf("CREATE TYPE %s.%s AS ENUM ('sad', 'ok')", schema.ident, datatype.ident)
		delimiter := delimiterFor(schema, datatype)
		statement := wrapPgIdempotentDataType(schema.name, datatype.name, definition)
		require.Equal(t, "DO "+delimiter+`
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type t
        JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE t.typname = `+datatype.lit+`
        AND n.nspname = `+schema.lit+`
    ) THEN
        `+definition+`;
    END IF;
END `+delimiter+";", statement)
		requireDoBlock(t, delimiter, statement)
	})
}

func Test_OddNames_IdempotentExtension(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, extension, version oddName) {
		require.Equal(t,
			fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s VERSION %s SCHEMA %s;", extension.ident, version.ident, schema.ident),
			wrapPgIdempotentExtension(sql.NullString{String: schema.name, Valid: true}, extension.name, version.name),
		)
		require.Equal(t,
			fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s VERSION %s;", extension.ident, version.ident),
			wrapPgIdempotentExtension(sql.NullString{String: "public", Valid: true}, extension.name, version.name),
		)
	})
}

func Test_OddNames_AlterStatementByForeignKeyConstraint(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, a, b, c oddName) {
		statement, err := buildAlterStatementByForeignKeyConstraint(&pg_queries.GetForeignKeyConstraintsBySchemasRow{
			ConstraintName: c.name, ReferencingSchema: a.name, ReferencingTable: b.name,
			ReferencingColumns: []string{c.name, a.name},
			ReferencedSchema:   b.name, ReferencedTable: c.name,
			ReferencedColumns: []string{a.name, b.name},
		})
		require.NoError(t, err)
		require.Equal(t,
			fmt.Sprintf(
				"ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s, %s) REFERENCES %s.%s (%s, %s);",
				a.ident, b.ident, c.ident, c.ident, a.ident, b.ident, c.ident, a.ident, b.ident,
			),
			statement,
		)
	})
}

func Test_OddNames_AlterStatementByConstraint(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, constraint oddName) {
		statement, err := buildAlterStatementByConstraint(&pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow{
			SchemaName: schema.name, TableName: table.name, ConstraintName: constraint.name,
			ConstraintDefinition: "PRIMARY KEY (id)",
		})
		require.NoError(t, err)
		require.Equal(t,
			fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s PRIMARY KEY (id);", schema.ident, table.ident, constraint.ident),
			statement,
		)
	})
}

func oddColumn(schema, table, column oddName) *sqlmanager_shared.TableColumn {
	generated := ""
	return &sqlmanager_shared.TableColumn{
		Schema: schema.name, Table: table.name, Name: column.name,
		DataType: "integer", IsNullable: true, ColumnDefault: "42", GeneratedType: &generated,
	}
}

func Test_OddNames_Columns(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, column oddName) {
		tableIdent := schema.ident + "." + table.ident

		require.Equal(t,
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s integer NULL DEFAULT 42;", tableIdent, column.ident),
			BuildAddColumnStatement(oddColumn(schema, table, column)),
		)
		require.Equal(t,
			fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableIdent, schema.ident, column.ident),
			BuildRenameColumnStatement(&schemamanager_shared.ColumnDiff{
				Column:       oddColumn(schema, table, column),
				RenameColumn: &schemamanager_shared.ColumnRename{OldName: schema.name},
			}),
		)
		require.Equal(t,
			fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s CASCADE;", tableIdent, column.ident),
			BuildDropColumnStatement(schema.name, table.name, column.name),
		)

		commented := oddColumn(schema, table, column)
		comment := "it's " + column.name
		commented.Comment = &comment
		require.Equal(t,
			[]string{
				fmt.Sprintf("COMMENT ON COLUMN %s.%s IS 'it''s %s';", tableIdent, column.ident, column.lit[1:len(column.lit)-1]),
				fmt.Sprintf(
					"ALTER TABLE %[1]s ALTER COLUMN %[2]s TYPE integer USING %[2]s::integer, ALTER COLUMN %[2]s DROP NOT NULL, "+
						"ALTER COLUMN %[2]s SET NOT NULL, ALTER COLUMN %[2]s DROP DEFAULT, ALTER COLUMN %[2]s SET DEFAULT 42, "+
						"ALTER COLUMN %[2]s DROP IDENTITY IF EXISTS;",
					tableIdent, column.ident,
				),
			},
			BuildAlterColumnStatement(&schemamanager_shared.ColumnDiff{
				Column: commented,
				Actions: []schemamanager_shared.ColumnAction{
					schemamanager_shared.SetDatatype, schemamanager_shared.DropNotNull, schemamanager_shared.SetNotNull,
					schemamanager_shared.DropDefault, schemamanager_shared.SetDefault, schemamanager_shared.DropIdentity,
					schemamanager_shared.SetComment,
				},
			}),
		)
	})
}

// The text of a comment is a value: it is written as one string literal.
func Test_OddNames_Comment(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, column oddName) {
		target := schema.ident + "." + table.ident + "." + column.ident
		require.Equal(t,
			"COMMENT ON COLUMN "+target+" IS NULL;",
			BuildUpdateCommentStatement(schema.name, table.name, column.name, nil),
		)
		comment := `the "best" o'clock, two$$dollars and back\slash`
		require.Equal(t,
			"COMMENT ON COLUMN "+target+` IS 'the "best" o''clock, two$$dollars and back\slash';`,
			BuildUpdateCommentStatement(schema.name, table.name, column.name, &comment),
		)
	})
}

func Test_OddNames_Drops(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, object oddName) {
		require.Equal(t,
			fmt.Sprintf("ALTER TABLE %s.%s DROP CONSTRAINT IF EXISTS %s CASCADE;", schema.ident, table.ident, object.ident),
			BuildDropConstraintStatement(schema.name, table.name, object.name),
		)
		require.Equal(t,
			fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s.%s;", object.ident, schema.ident, table.ident),
			BuildDropTriggerStatement(schema.name, table.name, object.name),
		)
		require.Equal(t,
			fmt.Sprintf("DROP FUNCTION IF EXISTS %s.%s;", schema.ident, object.ident),
			BuildDropFunctionStatement(schema.name, object.name),
		)
		require.Equal(t,
			fmt.Sprintf("DROP TYPE IF EXISTS %s.%s;", schema.ident, object.ident),
			BuildDropDatatypesStatement(schema.name, object.name),
		)
		require.Equal(t,
			fmt.Sprintf("DROP DOMAIN IF EXISTS %s.%s;", schema.ident, object.ident),
			BuildDropDomainStatement(schema.name, object.name),
		)
	})
}

func Test_OddNames_Types(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, datatype, attribute oddName) {
		typeIdent := schema.ident + "." + datatype.ident

		require.Equal(t,
			[]string{
				"ALTER TYPE " + typeIdent + " ADD VALUE IF NOT EXISTS 'happy';",
				"ALTER TYPE " + typeIdent + " RENAME VALUE 'sad' TO 'blue';",
			},
			BuildUpdateEnumStatements(schema.name, datatype.name, []string{"happy"}, map[string]string{"sad": "blue"}),
		)

		require.Equal(t,
			[]string{
				fmt.Sprintf("ALTER TYPE %s ALTER ATTRIBUTE %s SET DATA TYPE 'text';", typeIdent, attribute.ident),
				fmt.Sprintf("ALTER TYPE %s RENAME ATTRIBUTE %s TO  %s;", typeIdent, attribute.ident, schema.ident),
				fmt.Sprintf("ALTER TYPE %s ADD ATTRIBUTE %s integer;", typeIdent, attribute.ident),
				fmt.Sprintf("ALTER TYPE %s DROP ATTRIBUTE IF EXISTS %s;", typeIdent, attribute.ident),
			},
			BuildUpdateCompositeStatements(
				schema.name, datatype.name,
				map[string]string{attribute.name: "text"},
				map[string]string{attribute.name: schema.name},
				map[string]string{attribute.name: "integer"},
				[]string{attribute.name},
			),
		)
	})
}

func Test_OddNames_Domains(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, domain, constraint oddName) {
		domainIdent := schema.ident + "." + domain.ident

		require.Equal(t,
			[]string{
				fmt.Sprintf("ALTER DOMAIN %s DROP CONSTRAINT IF EXISTS %s;", domainIdent, constraint.ident),
				fmt.Sprintf("ALTER DOMAIN %s ADD CONSTRAINT %s CHECK ((VALUE < 1000));", domainIdent, constraint.ident),
			},
			BuildDomainConstraintStatements(
				schema.name, domain.name,
				map[string]string{constraint.name: "CHECK ((VALUE < 1000))"},
				[]string{constraint.name},
			),
		)
		require.Equal(t, "ALTER DOMAIN "+domainIdent+" SET DEFAULT 0;", BuildUpdateDomainDefaultStatement(schema.name, domain.name, "0"))
		require.Equal(t, "ALTER DOMAIN "+domainIdent+" DROP DEFAULT;", BuildDropDomainDefaultStatement(schema.name, domain.name))
		require.Equal(t, "ALTER DOMAIN "+domainIdent+" SET NOT NULL;", BuildUpdateDomainNotNullStatement(schema.name, domain.name, false))
		require.Equal(t, "ALTER DOMAIN "+domainIdent+" DROP NOT NULL;", BuildUpdateDomainNotNullStatement(schema.name, domain.name, true))
	})
}

func Test_OddNames_Sequences(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, a, b, c oddName) {
		require.Equal(t,
			fmt.Sprintf("ALTER SEQUENCE %s.%s OWNED BY %s.%s.%s;", a.ident, b.ident, c.ident, a.ident, b.ident),
			BuildSequencOwnerStatement(&pg_queries.GetSequencesOwnedByTablesRow{
				SequenceSchema: a.name, SequenceName: b.name, TableSchema: c.name, TableName: a.name, ColumnName: b.name,
			}),
		)
		require.Equal(t,
			fmt.Sprintf("ALTER SEQUENCE %s.%s RESTART;", a.ident, b.ident),
			BuildPgResetSequenceSql(a.name, b.name),
		)
	})
}

// pg_get_serial_sequence takes the table as the text of a qualified name, and the column as
// its plain name.
func Test_OddNames_IdentityColumnResetCurrent(t *testing.T) {
	require.Equal(t,
		`SELECT setval(pg_get_serial_sequence('"we""ird"."o''clock"', 'two$$dollars'), COALESCE((SELECT MAX("two$$dollars") FROM "we""ird"."o'clock"), 1));`,
		BuildPgIdentityColumnResetCurrentSql(`we"ird`, `o'clock`, `two$$dollars`),
	)
	require.Equal(t,
		`SELECT setval(pg_get_serial_sequence('"o''clock"."two$$dollars"', 'we"ird'), COALESCE((SELECT MAX("we""ird") FROM "o'clock"."two$$dollars"), 1));`,
		BuildPgIdentityColumnResetCurrentSql(`o'clock`, `two$$dollars`, `we"ird`),
	)
	require.Equal(t,
		`SELECT setval(pg_get_serial_sequence('"two$$dollars"."we""ird"', 'o''clock'), COALESCE((SELECT MAX("o'clock") FROM "two$$dollars"."we""ird"), 1));`,
		BuildPgIdentityColumnResetCurrentSql(`two$$dollars`, `we"ird`, `o'clock`),
	)
	require.Equal(t,
		`SELECT setval(pg_get_serial_sequence('"back\slash"."sp ace"', 'semi;colon'), COALESCE((SELECT MAX("semi;colon") FROM "back\slash"."sp ace"), 1));`,
		BuildPgIdentityColumnResetCurrentSql(`back\slash`, `sp ace`, `semi;colon`),
	)
}

func Test_OddNames_Truncate(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, a, b, c oddName) {
		statement, err := BuildPgTruncateStatement([]*sqlmanager_shared.SchemaTable{
			{Schema: a.name, Table: b.name},
			{Schema: c.name, Table: "a.b"},
		})
		require.NoError(t, err)
		require.Equal(t,
			fmt.Sprintf(`TRUNCATE %s.%s, %s."a.b" RESTART IDENTITY;`, a.ident, b.ident, c.ident),
			statement,
		)

		statement, err = BuildPgTruncateCascadeStatement(a.name, b.name)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("TRUNCATE %s.%s RESTART IDENTITY CASCADE;", a.ident, b.ident), statement)
	})
}

func Test_OddNames_TableRowCount(t *testing.T) {
	where := `"o'clock" > 3`
	eachOddOrder(t, func(t *testing.T, schema, table, _ oddName) {
		statement, err := buildTableRowCountSql(schema.name, table.name, nil)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", schema.ident, table.ident), statement)

		statement, err = buildTableRowCountSql(schema.name, table.name, &where)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE "o'clock" > 3`, schema.ident, table.ident), statement)
	})

	// A dot in a name does not separate a schema from a table.
	statement, err := buildTableRowCountSql("app", "a.b", nil)
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) FROM "app"."a.b"`, statement)

	statement, err = buildTableRowCountSql("", "users", nil)
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) FROM "users"`, statement)
}

// A name that no engine takes is refused where the builder can say so.
func Test_NamesRefused(t *testing.T) {
	for _, name := range []string{"", "nul\x00byte"} {
		_, err := BuildPgTruncateStatement([]*sqlmanager_shared.SchemaTable{{Schema: "app", Table: name}})
		require.Error(t, err)
		_, err = BuildPgTruncateCascadeStatement("app", name)
		require.Error(t, err)
		_, err = buildTableRowCountSql("app", name, nil)
		require.Error(t, err)
		_, err = buildAlterStatementByConstraint(&pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow{
			SchemaName: "app", TableName: "users", ConstraintName: name, ConstraintDefinition: "PRIMARY KEY (id)",
		})
		require.Error(t, err)
		_, err = buildAlterStatementByForeignKeyConstraint(&pg_queries.GetForeignKeyConstraintsBySchemasRow{
			ConstraintName: "fk", ReferencingSchema: "app", ReferencingTable: "orders",
			ReferencingColumns: []string{name}, ReferencedSchema: "app", ReferencedTable: "users",
			ReferencedColumns: []string{"id"},
		})
		require.Error(t, err)
	}

	_, err := BuildPgTruncateCascadeStatement("nul\x00byte", "users")
	require.Error(t, err)
	_, err = buildTableRowCountSql("nul\x00byte", "users", nil)
	require.Error(t, err)
}

// A foreign key is refused when the referenced table, the referenced columns or the name of the
// constraint is empty or holds a NUL byte.
func Test_buildAlterStatementByForeignKeyConstraint_ChecksTheReferencedNames(t *testing.T) {
	build := func(mutate func(c *pg_queries.GetForeignKeyConstraintsBySchemasRow)) error {
		c := &pg_queries.GetForeignKeyConstraintsBySchemasRow{
			ConstraintName: "fk", ReferencingSchema: "app", ReferencingTable: "orders",
			ReferencingColumns: []string{"user_id"}, ReferencedSchema: "app", ReferencedTable: "users",
			ReferencedColumns: []string{"id"},
		}
		mutate(c)
		_, err := buildAlterStatementByForeignKeyConstraint(c)
		return err
	}
	require.NoError(t, build(func(*pg_queries.GetForeignKeyConstraintsBySchemasRow) {}))

	for _, name := range []string{"", "nul\x00byte"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			require.Error(t, build(func(c *pg_queries.GetForeignKeyConstraintsBySchemasRow) { c.ReferencedTable = name }),
				"referenced table")
			require.Error(t, build(func(c *pg_queries.GetForeignKeyConstraintsBySchemasRow) { c.ReferencedColumns = []string{"id", name} }),
				"referenced columns")
			require.Error(t, build(func(c *pg_queries.GetForeignKeyConstraintsBySchemasRow) { c.ConstraintName = name }),
				"constraint name")
		})
	}
}

// The statements that create a table and its partitions carry the names read from the
// catalog, each as one identifier.
func Test_OddNames_TableInitStatements(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, column oddName) {
		key := schema.name + "." + table.name
		partition := table.name + "_1"
		partitionIdent := strings.TrimSuffix(table.ident, `"`) + `_1"`

		querier := pg_queries.NewMockQuerier(t)
		querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, []string{key}).
			Return([]*pg_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
				SchemaName: schema.name, TableName: table.name, ColumnName: column.name, DataType: "integer", IsNullable: "NO",
			}}, nil)
		querier.EXPECT().GetNonForeignKeyTableConstraintsBySchema(mock.Anything, mock.Anything, []string{schema.name}).
			Return([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow{{
				SchemaName: schema.name, TableName: table.name, ConstraintName: column.name,
				ConstraintType: "p", ConstraintDefinition: "PRIMARY KEY (" + column.ident + ")",
			}}, nil)
		querier.EXPECT().GetForeignKeyConstraintsBySchemas(mock.Anything, mock.Anything, []string{schema.name}).Return(nil, nil)
		querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, []string{key}).Return(nil, nil)
		querier.EXPECT().GetPartitionedTablesBySchema(mock.Anything, mock.Anything, []string{schema.name}).
			Return([]*pg_queries.GetPartitionedTablesBySchemaRow{
				{SchemaName: schema.name, TableName: table.name, PartitionKey: "RANGE (" + column.ident + ")"},
			}, nil)
		querier.EXPECT().GetPartitionHierarchyByTable(mock.Anything, mock.Anything, schema.ident+"."+table.ident).
			Return([]*pg_queries.GetPartitionHierarchyByTableRow{
				{SchemaName: schema.name, TableName: table.name},
				{
					SchemaName: schema.name, TableName: partition,
					ParentSchemaName: sql.NullString{String: schema.name, Valid: true},
					ParentTableName:  sql.NullString{String: table.name, Valid: true},
					PartitionBound:   "FOR VALUES FROM (1) TO (100)",
				},
			}, nil)

		statements, err := NewManager(querier, nil, func() {}).GetTableInitStatements(
			context.Background(), []*sqlmanager_shared.SchemaTable{{Schema: schema.name, Table: table.name}},
		)

		require.NoError(t, err)
		require.Len(t, statements, 1)
		require.Equal(t,
			fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s.%s (%s integer NOT NULL) PARTITION BY RANGE (%s);",
				schema.ident, table.ident, column.ident, column.ident,
			),
			statements[0].CreateTableStatement,
		)
		require.Equal(t,
			[]string{fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s.%s PARTITION OF %s.%s FOR VALUES FROM (1) TO (100) ;",
				schema.ident, partitionIdent, schema.ident, table.ident,
			)},
			statements[0].PartitionStatements,
		)
		require.Len(t, statements[0].AlterTableStatements, 1)
		alter := statements[0].AlterTableStatements[0].Statement
		requireDoBlock(t, "$husonym$", alter)
		require.Contains(t, alter, "WHERE conname = "+column.lit+"\n")
		require.Contains(t, alter, fmt.Sprintf(
			"ALTER TABLE %s.%s ADD CONSTRAINT %s PRIMARY KEY (%s);", schema.ident, table.ident, column.ident, column.ident,
		))
	})
}

// The other characters a name may hold are written as they are between the quotes.
func Test_OtherOddNames(t *testing.T) {
	names := map[string]string{
		`back\slash`:    `"back\slash"`,
		`semi;colon`:    `"semi;colon"`,
		"new\nline":     "\"new\nline\"",
		`sp ace`:        `"sp ace"`,
		`a.b`:           `"a.b"`,
		`$husonym$`:     `"$husonym$"`,
		`quote"and'apo`: `"quote""and'apo"`,
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		ident := names[name]
		require.Equal(t, "CREATE SCHEMA IF NOT EXISTS "+ident+";", buildCreateSchemaStatement(name))
		require.Equal(t, "DROP TYPE IF EXISTS "+ident+"."+ident+";", BuildDropDatatypesStatement(name, name))
		require.Equal(t, "ALTER SEQUENCE "+ident+"."+ident+" RESTART;", BuildPgResetSequenceSql(name, name))
		statement, err := BuildPgTruncateCascadeStatement(name, name)
		require.NoError(t, err)
		require.Equal(t, "TRUNCATE "+ident+"."+ident+" RESTART IDENTITY CASCADE;", statement)
	}
}
