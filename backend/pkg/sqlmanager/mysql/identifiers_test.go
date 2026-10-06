package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// oddName is a name with a character that the statements of the manager must carry: how it
// is written as an identifier and as a string literal.
type oddName struct {
	name, ident, lit string
}

var oddNames = []oddName{
	{name: "we`ird", ident: "`we``ird`", lit: "'we`ird'"},
	{name: `o'clock`, ident: "`o'clock`", lit: `'o''clock'`},
	{name: `back\slash`, ident: "`back\\slash`", lit: `_utf8mb4 0x6261636B5C736C617368`},
}

// eachOddOrder runs a case three times, so that each of the three names takes each place.
func eachOddOrder(t *testing.T, run func(t *testing.T, a, b, c oddName)) {
	t.Helper()
	for i := range oddNames {
		a, b, c := oddNames[i], oddNames[(i+1)%3], oddNames[(i+2)%3]
		t.Run(fmt.Sprintf("order %d", i), func(t *testing.T) { run(t, a, b, c) })
	}
}

// A procedure name ends with a token drawn for each statement: it is replaced by a fixed
// name before two statements are compared.
var procedureNamePattern = regexp.MustCompile("`Husonym(AddConstraint|AddIndex)_[0-9a-f]+_[0-9a-f]{12}`")

func withFixedProcedureName(t *testing.T, statement string) string {
	t.Helper()
	names := procedureNamePattern.FindAllString(statement, -1)
	require.Len(t, names, 4, statement)
	for _, name := range names {
		require.Equal(t, names[0], name)
		require.LessOrEqual(t, len(name)-2, maxIdentifierLength)
	}
	return procedureNamePattern.ReplaceAllString(statement, "`PROC`")
}

func wantConstraintProcedure(schema, table, constraint oddName, alter string) string {
	return "DROP PROCEDURE IF EXISTS `PROC`;\n" +
		"\n" +
		"CREATE PROCEDURE `PROC`()\n" +
		"BEGIN\n" +
		"    DECLARE constraint_exists INT DEFAULT 0;\n" +
		"\n" +
		"    SELECT COUNT(*) INTO constraint_exists\n" +
		"    FROM information_schema.TABLE_CONSTRAINTS\n" +
		"    WHERE CONSTRAINT_SCHEMA = " + schema.lit + "\n" +
		"    AND TABLE_NAME = " + table.lit + "\n" +
		"    AND CONSTRAINT_NAME = " + constraint.lit + ";\n" +
		"\n" +
		"    IF constraint_exists = 0 THEN\n" +
		"        " + alter + "\n" +
		"    END IF;\n" +
		"END;\n" +
		"\n" +
		"CALL `PROC`();\n" +
		"DROP PROCEDURE `PROC`;"
}

func wantIndexProcedure(schema, table, index oddName, alter string) string {
	return "DROP PROCEDURE IF EXISTS `PROC`;\n" +
		"\n" +
		"CREATE PROCEDURE `PROC`()\n" +
		"BEGIN\n" +
		"    DECLARE index_exists INT DEFAULT 0;\n" +
		"\n" +
		"    SELECT COUNT(*) INTO index_exists\n" +
		"    FROM information_schema.statistics\n" +
		"    WHERE table_schema = " + schema.lit + "\n" +
		"    AND table_name = " + table.lit + "\n" +
		"    AND index_name = " + index.lit + ";\n" +
		"\n" +
		"    IF index_exists = 0 THEN\n" +
		"        " + alter + "\n" +
		"    END IF;\n" +
		"END;\n" +
		"\n" +
		"CALL `PROC`();\n" +
		"DROP PROCEDURE `PROC`;"
}

func jsonNames(t *testing.T, names ...string) json.RawMessage {
	t.Helper()
	bits, err := json.Marshal(names)
	require.NoError(t, err)
	return bits
}

func Test_OddNames_CreateSchema(t *testing.T) {
	for _, n := range oddNames {
		actual, err := buildCreateSchemaStatement(n.name)
		require.NoError(t, err)
		require.Equal(t, "CREATE SCHEMA IF NOT EXISTS "+n.ident+";", actual)
	}
}

func Test_OddNames_CreateTable(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, column oddName) {
		actual, err := buildCreateTableStatement(schema.name, table.name, []string{
			buildTableColForCreate(&buildTableColRequest{ColumnName: column.name, DataType: "int"}),
		})
		require.NoError(t, err)
		require.Equal(
			t,
			"CREATE TABLE IF NOT EXISTS "+schema.ident+"."+table.ident+" ("+column.ident+" int NOT NULL);",
			actual,
		)
	})

	// Other characters need nothing: the name is written between the backticks as it is.
	for _, name := range []string{"semi;colon", "sp ace", "new\nline", `we"ird`, "do.t"} {
		actual, err := buildCreateTableStatement(name, name, []string{EscapeMysqlColumn(name) + " int NULL"})
		require.NoError(t, err)
		require.Equal(t, "CREATE TABLE IF NOT EXISTS `"+name+"`.`"+name+"` (`"+name+"` int NULL);", actual)
	}
}

func Test_OddNames_PrimaryKey(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, column oddName) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:            schema.name,
			TableName:             table.name,
			ConstraintName:        "PRIMARY",
			ConstraintType:        "PRIMARY KEY",
			ConstraintColumns:     jsonNames(t, column.name, "id"),
			ReferencedColumnNames: json.RawMessage(`[null,null]`),
			ConstraintColumnPrefixes: json.RawMessage(
				`[{"column":"id","sub_part":10}]`,
			),
		})
		require.NoError(t, err)
		require.Equal(t, sqlmanager_shared.PrimaryConstraintType, actual.ConstraintType)
		require.Equal(
			t,
			wantConstraintProcedure(schema, table, oddName{lit: "'PRIMARY'"},
				"ALTER TABLE "+schema.ident+"."+table.ident+" ADD PRIMARY KEY ("+column.ident+",`id`(10));"),
			withFixedProcedureName(t, actual.Statement),
		)
	})
}

func Test_OddNames_Unique(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, constraint oddName) {
		column := schema
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:            schema.name,
			TableName:             table.name,
			ConstraintName:        constraint.name,
			ConstraintType:        "UNIQUE",
			ConstraintColumns:     jsonNames(t, column.name),
			ReferencedColumnNames: json.RawMessage(`[null]`),
		})
		require.NoError(t, err)
		require.Equal(t, sqlmanager_shared.UniqueConstraintType, actual.ConstraintType)
		require.Equal(
			t,
			wantConstraintProcedure(schema, table, constraint,
				"ALTER TABLE "+schema.ident+"."+table.ident+" ADD CONSTRAINT "+constraint.ident+
					" UNIQUE ("+column.ident+");"),
			withFixedProcedureName(t, actual.Statement),
		)
	})
}

func Test_OddNames_ForeignKey(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, constraint oddName) {
		// The parent takes the names in another order, and so do the columns.
		parentSchema, parentTable, column, parentColumn := table, constraint, constraint, schema
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:            schema.name,
			TableName:             table.name,
			ConstraintName:        constraint.name,
			ConstraintType:        "FOREIGN KEY",
			ConstraintColumns:     jsonNames(t, column.name),
			ReferencedSchemaName:  parentSchema.name,
			ReferencedTableName:   parentTable.name,
			ReferencedColumnNames: jsonNames(t, parentColumn.name),
			UpdateRule:            sql.NullString{String: "NO ACTION", Valid: true},
			DeleteRule:            sql.NullString{String: "CASCADE", Valid: true},
		})
		require.NoError(t, err)
		require.Equal(t, sqlmanager_shared.ForeignConstraintType, actual.ConstraintType)
		require.Equal(
			t,
			wantConstraintProcedure(schema, table, constraint,
				"ALTER TABLE "+schema.ident+"."+table.ident+" ADD CONSTRAINT "+constraint.ident+
					" FOREIGN KEY ("+column.ident+") REFERENCES "+parentSchema.ident+"."+parentTable.ident+
					"("+parentColumn.ident+") ON DELETE CASCADE ON UPDATE NO ACTION;"),
			withFixedProcedureName(t, actual.Statement),
		)
	})
}

func Test_OddNames_Check(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, constraint oddName) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:            schema.name,
			TableName:             table.name,
			ConstraintName:        constraint.name,
			ConstraintType:        "CHECK",
			ConstraintColumns:     json.RawMessage(`[null]`),
			ReferencedColumnNames: json.RawMessage(`[null]`),
			CheckClause:           []uint8("`age` >= 0"),
		})
		require.NoError(t, err)
		require.Equal(t, sqlmanager_shared.CheckConstraintType, actual.ConstraintType)
		require.Equal(
			t,
			wantConstraintProcedure(schema, table, constraint,
				"ALTER TABLE "+schema.ident+"."+table.ident+" ADD CONSTRAINT "+constraint.ident+
					" CHECK (`age` >= 0);"),
			withFixedProcedureName(t, actual.Statement),
		)
	})
}

func Test_CheckConstraint_NameIsWrittenAsAnIdentifier(t *testing.T) {
	build := func(name string) string {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:            "db",
			TableName:             "t",
			ConstraintName:        name,
			ConstraintType:        "CHECK",
			ConstraintColumns:     json.RawMessage(`[null]`),
			ReferencedColumnNames: json.RawMessage(`[null]`),
			CheckClause:           []uint8("`age` >= 0"),
		})
		require.NoError(t, err)
		return actual.Statement
	}

	require.Contains(t, build("chk_age"), "ALTER TABLE `db`.`t` ADD CONSTRAINT `chk_age` CHECK (`age` >= 0);")
	// A reserved word is a name like another.
	require.Contains(t, build("order"), "ALTER TABLE `db`.`t` ADD CONSTRAINT `order` CHECK (`age` >= 0);")

	// A constraint without a name keeps the statement it had: the server names it.
	unnamed := build("")
	require.Contains(t, unnamed, "ALTER TABLE `db`.`t` ADD CONSTRAINT  CHECK (`age` >= 0);")
	require.Contains(t, unnamed, "AND CONSTRAINT_NAME = '';")
}

func Test_OddNames_Index(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, index oddName) {
		column := schema
		t.Run("btree", func(t *testing.T) {
			actual := wrapIdempotentIndex(schema.name, table.name, &indexInfo{
				indexName:      index.name,
				indexType:      "BTREE",
				columns:        []string{column.name, "(lower(`c`))"},
				columnPrefixes: map[string]int64{column.name: 20},
			})
			require.Equal(
				t,
				wantIndexProcedure(schema, table, index,
					"ALTER TABLE "+schema.ident+"."+table.ident+" ADD INDEX "+index.ident+
						" ("+column.ident+"(20), (lower(`c`))) USING BTREE;"),
				withFixedProcedureName(t, actual),
			)
		})
		t.Run("fulltext", func(t *testing.T) {
			actual := wrapIdempotentIndex(schema.name, table.name, &indexInfo{
				indexName: index.name,
				indexType: "FULLTEXT",
				columns:   []string{column.name},
			})
			require.Equal(
				t,
				wantIndexProcedure(schema, table, index,
					"ALTER TABLE "+schema.ident+"."+table.ident+" ADD FULLTEXT INDEX "+index.ident+
						" ("+column.ident+");"),
				withFixedProcedureName(t, actual),
			)
		})
	})
}

func Test_OddNames_TriggerAndFunction(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, name oddName) {
		require.Equal(
			t,
			"CREATE TRIGGER IF NOT EXISTS "+schema.ident+"."+name.ident+"\n"+
				"BEFORE INSERT ON "+schema.ident+"."+table.ident+"\n"+
				"FOR EACH ROW\n"+
				"SET NEW.id = 1;",
			wrapIdempotentTrigger(schema.name, table.name, name.name, schema.name,
				"BEFORE", "INSERT", "ROW", "SET NEW.id = 1"),
		)
		require.Equal(
			t,
			"CREATE FUNCTION IF NOT EXISTS "+schema.ident+"."+name.ident+"(a int)\n"+
				"RETURNS int\n"+
				"DETERMINISTIC\n"+
				"RETURN a + 1;",
			wrapIdempotentFunction(schema.name, name.name, "a int", "int", "RETURN a + 1", true),
		)
	})
}

func Test_OddNames_ColumnStatements(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, name oddName) {
		empty := ""
		column := &sqlmanager_shared.TableColumn{
			Schema: schema.name, Table: table.name, Name: name.name,
			DataType: "int", IsNullable: true, GeneratedExpression: &empty,
		}
		qualified := schema.ident + "." + table.ident

		add, err := BuildAddColumnStatement(column)
		require.NoError(t, err)
		require.Equal(t, "ALTER TABLE "+qualified+" ADD COLUMN "+name.ident+" int NULL;", add)

		modify, err := BuildUpdateColumnStatement(column)
		require.NoError(t, err)
		require.Equal(t, "ALTER TABLE "+qualified+" MODIFY COLUMN "+name.ident+" int NULL;", modify)

		require.Equal(t, "ALTER TABLE "+qualified+" DROP COLUMN "+name.ident+";", BuildDropColumnStatement(column))

		require.Equal(t, "ALTER TABLE "+qualified+" DROP PRIMARY KEY;",
			BuildDropConstraintStatement(schema.name, table.name, "PRIMARY KEY", "PRIMARY"))
		require.Equal(t, "ALTER TABLE "+qualified+" DROP INDEX "+name.ident+";",
			BuildDropConstraintStatement(schema.name, table.name, "UNIQUE", name.name))
		require.Equal(t, "ALTER TABLE "+qualified+" DROP CONSTRAINT "+name.ident+";",
			BuildDropConstraintStatement(schema.name, table.name, "CHECK", name.name))
		require.Equal(t, "ALTER TABLE "+qualified+" DROP FOREIGN KEY "+name.ident+";",
			BuildDropConstraintStatement(schema.name, table.name, "FOREIGN KEY", name.name))

		require.Equal(t, "DROP TRIGGER IF EXISTS "+schema.ident+"."+name.ident+";",
			BuildDropTriggerStatement(&schema.name, name.name))
		require.Equal(t, "DROP TRIGGER IF EXISTS "+name.ident+";", BuildDropTriggerStatement(nil, name.name))
		require.Equal(t, "DROP FUNCTION IF EXISTS "+schema.ident+"."+name.ident+";",
			BuildDropFunctionStatement(schema.name, name.name))
	})
}

func Test_ColumnComment(t *testing.T) {
	build := func(comment string) string {
		return buildTableColForCreate(&buildTableColRequest{
			ColumnName: "c", DataType: "int", IsNullable: true, Comment: &comment,
		})
	}

	require.Equal(t, "`c` int NULL COMMENT 'a plain comment'", build("a plain comment"))
	// An apostrophe is doubled, which reads the same under every sql_mode.
	require.Equal(t, "`c` int NULL COMMENT 'o''clock'", build("o'clock"))
	require.Equal(t, "`c` int NULL COMMENT 'we`ird; \"x\"'", build("we`ird; \"x\""))
	// No quoted string holding a backslash reads the same under every sql_mode, and a
	// COMMENT clause takes nothing else: the comment is left out.
	require.Equal(t, "`c` int NULL", build(`back\slash`))
	require.Equal(t, "`c` int NULL", build(""))
}

// A default the catalog gives as a plain string value is written as one string literal; a
// default it gives as an expression is replayed as read.
func Test_ColumnDefault(t *testing.T) {
	stringDefault, expressionDefault := columnDefaultString, columnDefaultDefault
	for value, literal := range map[string]string{
		"plain value_1": `'plain value_1'`,
		`o'clock`:       `'o''clock'`,
		`back\slash`:    `_utf8mb4 0x6261636B5C736C617368`,
		`quarter past'`: `'quarter past'''`,
	} {
		actual, err := EscapeMysqlDefaultColumn(value, &stringDefault)
		require.NoError(t, err)
		require.Equal(t, literal, actual)

		empty := ""
		statement, err := BuildAddColumnStatement(&sqlmanager_shared.TableColumn{
			Schema: "db", Table: "t", Name: "c", DataType: "varchar(40)", IsNullable: true,
			ColumnDefault: value, ColumnDefaultType: &stringDefault, GeneratedExpression: &empty,
		})
		require.NoError(t, err)
		require.Equal(t, "ALTER TABLE `db`.`t` ADD COLUMN `c` varchar(40) NULL DEFAULT "+literal+";", statement)

		actual, err = EscapeMysqlDefaultColumn(value, &expressionDefault)
		require.NoError(t, err)
		require.Equal(t, "("+value+")", actual)

		actual, err = EscapeMysqlDefaultColumn(value, nil)
		require.NoError(t, err)
		require.Equal(t, value, actual)
	}
}

// The table of a job is created with the string default of a column written as one literal.
func Test_GetTableInitStatements_WritesAStringDefaultAsOneLiteral(t *testing.T) {
	querier := mysql_queries.NewMockQuerier(t)
	column := func(name, columnDefault, extra string) *mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow {
		return &mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
			SchemaName: "db", TableName: "t", ColumnName: name, DataType: "varchar(40)", IsNullable: 1,
			ColumnDefault: []uint8(columnDefault), GenerationExp: []uint8(""),
			IdentityGeneration: sql.NullString{String: extra, Valid: true},
		}
	}
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
			column("plain", "plain value_1", ""),
			column("apostrophe", `o'clock`, ""),
			column("backslash", `back\slash`, ""),
			column("expression", `concat(_utf8mb4'o',_utf8mb4'clock')`, "DEFAULT_GENERATED"),
		}, nil)
	querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetTableConstraintsRow{}, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{}, nil)
	manager := &MysqlManager{resolvedQuerier: querier}

	statements, err := manager.GetTableInitStatements(
		context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}},
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Equal(t,
		"CREATE TABLE IF NOT EXISTS `db`.`t` ("+
			"`plain` varchar(40) NULL DEFAULT 'plain value_1', "+
			"`apostrophe` varchar(40) NULL DEFAULT 'o''clock', "+
			"`backslash` varchar(40) NULL DEFAULT _utf8mb4 0x6261636B5C736C617368, "+
			"`expression` varchar(40) NULL DEFAULT (concat(_utf8mb4'o',_utf8mb4'clock')));",
		statements[0].CreateTableStatement,
	)
}

func Test_OddNames_Truncate(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, _ oddName) {
		actual, err := BuildMysqlTruncateStatement(schema.name, table.name)
		require.NoError(t, err)
		require.Equal(t, "TRUNCATE "+schema.ident+"."+table.ident+";", actual)
	})

	// A dot is a character of the name.
	actual, err := BuildMysqlTruncateStatement("my.db", "my.table")
	require.NoError(t, err)
	require.Equal(t, "TRUNCATE `my.db`.`my.table`;", actual)
}

func Test_OddNames_TableRowCount(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, _ oddName) {
		actual, err := buildTableRowCountSql(schema.name, table.name, nil)
		require.NoError(t, err)
		require.Equal(t, "SELECT COUNT(*) FROM "+schema.ident+"."+table.ident, actual)
	})

	t.Run("a dot is a character of the name", func(t *testing.T) {
		actual, err := buildTableRowCountSql("my.db", "my.table", nil)
		require.NoError(t, err)
		require.Equal(t, "SELECT COUNT(*) FROM `my.db`.`my.table`", actual)
	})

	t.Run("a table without a schema is written alone", func(t *testing.T) {
		actual, err := buildTableRowCountSql("", "users", nil)
		require.NoError(t, err)
		require.Equal(t, "SELECT COUNT(*) FROM `users`", actual)
	})

	t.Run("the where clause is written as given", func(t *testing.T) {
		where := "`name` = 'o''clock' AND id > 3"
		actual, err := buildTableRowCountSql("db", "users", &where)
		require.NoError(t, err)
		require.Equal(t, "SELECT COUNT(*) FROM `db`.`users` WHERE `name` = 'o''clock' AND id > 3", actual)

		empty := ""
		actual, err = buildTableRowCountSql("db", "users", &empty)
		require.NoError(t, err)
		require.Equal(t, "SELECT COUNT(*) FROM `db`.`users`", actual)
	})
}

func Test_NamesRefused(t *testing.T) {
	const nul = "nul\x00byte"

	for _, name := range []string{"", nul} {
		_, err := buildCreateSchemaStatement(name)
		require.Error(t, err)

		_, err = buildCreateTableStatement(name, "t", nil)
		require.Error(t, err)
		_, err = buildCreateTableStatement("db", name, nil)
		require.Error(t, err)

		_, err = BuildMysqlTruncateStatement("db", name)
		require.Error(t, err)
		_, err = buildTableRowCountSql("db", name, nil)
		require.Error(t, err)

		empty := ""
		for _, column := range []*sqlmanager_shared.TableColumn{
			{Schema: name, Table: "t", Name: "c", DataType: "int", GeneratedExpression: &empty},
			{Schema: "db", Table: name, Name: "c", DataType: "int", GeneratedExpression: &empty},
			{Schema: "db", Table: "t", Name: name, DataType: "int", GeneratedExpression: &empty},
		} {
			_, err = BuildAddColumnStatement(column)
			require.Error(t, err)
			_, err = BuildUpdateColumnStatement(column)
			require.Error(t, err)
		}
	}

	_, err := BuildMysqlTruncateStatement(nul, "t")
	require.Error(t, err)
	_, err = buildTableRowCountSql(nul, "t", nil)
	require.Error(t, err)

	constraint := func(kind string, change func(row *mysql_queries.GetTableConstraintsRow)) error {
		row := &mysql_queries.GetTableConstraintsRow{
			SchemaName:            "db",
			TableName:             "t",
			ConstraintName:        "c1",
			ConstraintType:        kind,
			ConstraintColumns:     json.RawMessage(`["a"]`),
			ReferencedSchemaName:  "db",
			ReferencedTableName:   "p",
			ReferencedColumnNames: json.RawMessage(`["id"]`),
			CheckClause:           []uint8("`a` > 0"),
		}
		change(row)
		_, err := buildAlterStatementByConstraint(row)
		return err
	}
	for _, kind := range []string{"PRIMARY KEY", "UNIQUE", "FOREIGN KEY", "CHECK"} {
		require.NoError(t, constraint(kind, func(*mysql_queries.GetTableConstraintsRow) {}), kind)
		for _, name := range []string{"", nul} {
			require.Error(t, constraint(kind, func(r *mysql_queries.GetTableConstraintsRow) { r.SchemaName = name }), kind)
			require.Error(t, constraint(kind, func(r *mysql_queries.GetTableConstraintsRow) { r.TableName = name }), kind)
		}
		require.Error(t, constraint(kind, func(r *mysql_queries.GetTableConstraintsRow) { r.ConstraintName = nul }), kind)
	}
	for _, kind := range []string{"UNIQUE", "FOREIGN KEY"} {
		require.Error(t, constraint(kind, func(r *mysql_queries.GetTableConstraintsRow) { r.ConstraintName = "" }), kind)
	}
	for _, name := range []string{"", nul} {
		require.Error(t, constraint("FOREIGN KEY", func(r *mysql_queries.GetTableConstraintsRow) { r.ReferencedSchemaName = name }))
		require.Error(t, constraint("FOREIGN KEY", func(r *mysql_queries.GetTableConstraintsRow) { r.ReferencedTableName = name }))
	}
}

func Test_Constraint_WithoutAColumnName_KeepsItsStatement(t *testing.T) {
	// The catalog gives no column name for a key part that is an expression. The statement
	// is built as before, so that the server answers it alone and the other statements of
	// the table are still built.
	actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
		SchemaName:            "db",
		TableName:             "t",
		ConstraintName:        "uniq_lower_email",
		ConstraintType:        "UNIQUE",
		ConstraintColumns:     json.RawMessage(`[null]`),
		ReferencedColumnNames: json.RawMessage(`[null]`),
	})
	require.NoError(t, err)
	require.Contains(t, actual.Statement, "ALTER TABLE `db`.`t` ADD CONSTRAINT `uniq_lower_email` UNIQUE (``);")
}

func Test_OddNames_GetSchemaInitStatements(t *testing.T) {
	eachOddOrder(t, func(t *testing.T, schema, table, name oddName) {
		column := name
		querier := mysql_queries.NewMockQuerier(t)
		querier.EXPECT().GetCustomFunctionsBySchemas(mock.Anything, mock.Anything, []string{schema.name}).
			Return([]*mysql_queries.GetCustomFunctionsBySchemasRow{{
				FunctionName: name.name, SchemaName: schema.name, ReturnDataType: "int",
				Definition: "RETURN a + 1", IsDeterministic: 1, FunctionSignature: []uint8("a int"),
			}}, nil)
		querier.EXPECT().GetCustomTriggersBySchemaAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetCustomTriggersBySchemaAndTablesRow{{
				TriggerName: name.name, TriggerSchema: schema.name, SchemaName: schema.name, TableName: table.name,
				Statement: "SET NEW.id = 1", EventType: "INSERT", Orientation: "ROW", Timing: "BEFORE",
			}}, nil)
		querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
				{
					SchemaName: schema.name, TableName: table.name, ColumnName: column.name, DataType: "int",
					ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
					Comment: sql.NullString{String: "it's the key", Valid: true},
				},
				{
					SchemaName: schema.name, TableName: table.name, ColumnName: "age", DataType: "int",
					ColumnDefault: []uint8(""), GenerationExp: []uint8(""), IsNullable: 1,
				},
			}, nil)
		querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetTableConstraintsRow{
				{
					SchemaName: schema.name, TableName: table.name, ConstraintName: "PRIMARY", ConstraintType: "PRIMARY KEY",
					ConstraintColumns: jsonNames(t, column.name), ReferencedColumnNames: json.RawMessage(`[null]`),
				},
				{
					SchemaName: schema.name, TableName: table.name, ConstraintName: name.name, ConstraintType: "CHECK",
					ConstraintColumns: json.RawMessage(`[null]`), ReferencedColumnNames: json.RawMessage(`[null]`),
					CheckClause: []uint8("`age` >= 0"),
				},
				{
					SchemaName: schema.name, TableName: table.name, ConstraintName: name.name, ConstraintType: "FOREIGN KEY",
					ConstraintColumns:    jsonNames(t, "age"),
					ReferencedSchemaName: schema.name, ReferencedTableName: table.name,
					ReferencedColumnNames: jsonNames(t, column.name),
					UpdateRule:            sql.NullString{String: "NO ACTION", Valid: true},
					DeleteRule:            sql.NullString{String: "NO ACTION", Valid: true},
				},
			}, nil)
		querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{{
				SchemaName: schema.name, TableName: table.name, IndexName: name.name, IndexType: "BTREE",
				ColumnName: sql.NullString{String: "age", Valid: true},
			}}, nil)

		manager := &MysqlManager{resolvedQuerier: querier}
		blocks, err := manager.GetSchemaInitStatements(
			context.Background(),
			[]*sqlmanager_shared.SchemaTable{{Schema: schema.name, Table: table.name}},
		)
		require.NoError(t, err)

		qualified := schema.ident + "." + table.ident
		actual := map[string][]string{}
		for _, block := range blocks {
			for _, statement := range block.Statements {
				if procedureNamePattern.MatchString(statement) {
					statement = withFixedProcedureName(t, statement)
				}
				actual[block.Label] = append(actual[block.Label], statement)
			}
		}
		require.Equal(t, map[string][]string{
			sqlmanager_shared.SchemasLabel: {"CREATE SCHEMA IF NOT EXISTS " + schema.ident + ";"},
			"data types": {
				"CREATE FUNCTION IF NOT EXISTS " + schema.ident + "." + name.ident + "(a int)\n" +
					"RETURNS int\nDETERMINISTIC\nRETURN a + 1;",
			},
			sqlmanager_shared.CreateTablesLabel: {
				"CREATE TABLE IF NOT EXISTS " + qualified + " (" + column.ident +
					" int NOT NULL COMMENT 'it''s the key', `age` int NULL);",
			},
			"non-fk alter table": {
				wantConstraintProcedure(schema, table, oddName{lit: "'PRIMARY'"},
					"ALTER TABLE "+qualified+" ADD PRIMARY KEY ("+column.ident+");"),
				wantConstraintProcedure(schema, table, name,
					"ALTER TABLE "+qualified+" ADD CONSTRAINT "+name.ident+" CHECK (`age` >= 0);"),
			},
			"table index": {
				wantIndexProcedure(schema, table, name,
					"ALTER TABLE "+qualified+" ADD INDEX "+name.ident+" (`age`) USING BTREE;"),
			},
			"fk alter table": {
				wantConstraintProcedure(schema, table, name,
					"ALTER TABLE "+qualified+" ADD CONSTRAINT "+name.ident+" FOREIGN KEY (`age`) REFERENCES "+
						qualified+"("+column.ident+") ON DELETE NO ACTION ON UPDATE NO ACTION;"),
			},
			"table triggers": {
				"CREATE TRIGGER IF NOT EXISTS " + schema.ident + "." + name.ident + "\n" +
					"BEFORE INSERT ON " + qualified + "\nFOR EACH ROW\nSET NEW.id = 1;",
			},
		}, actual)
	})
}

func Test_GetTableInitStatements_NamesRefused(t *testing.T) {
	const nul = "nul\x00byte"
	run := func(column, index string) error {
		querier := mysql_queries.NewMockQuerier(t)
		querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
				SchemaName: "db", TableName: "t", ColumnName: column, DataType: "int",
				ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
			}}, nil)
		querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetTableConstraintsRow{}, nil)
		querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{{
				SchemaName: "db", TableName: "t", IndexName: index, IndexType: "BTREE",
				ColumnName: sql.NullString{String: "a", Valid: true},
			}}, nil)
		manager := &MysqlManager{resolvedQuerier: querier}
		_, err := manager.GetTableInitStatements(
			context.Background(),
			[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}},
		)
		return err
	}
	require.NoError(t, run("a", "idx"))
	require.Error(t, run("", "idx"))
	require.Error(t, run(nul, "idx"))
	require.Error(t, run("a", ""))
	require.Error(t, run("a", nul))
}
