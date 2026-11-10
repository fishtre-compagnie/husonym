package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// What SHOW CREATE TABLE gives on MySQL 5.7 of a table whose name holds a backtick and a line
// break, with a comment that reads like a foreign key and like the AUTO_INCREMENT= option.
const showCreateMysql57 = "CREATE TABLE `we``ird\nx` (\n" +
	"  `id` int(10) unsigned zerofill NOT NULL AUTO_INCREMENT,\n" +
	"  `a` varchar(10) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT 'it''s a\\\\b FOREIGN KEY (x), y',\n" +
	"  `b` text CHARACTER SET latin7,\n" +
	"  `c` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n" +
	"  `d` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00',\n" +
	"  `p_id` int(11) NOT NULL,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  KEY `fk2` (`p_id`),\n" +
	"  FULLTEXT KEY `ft` (`b`),\n" +
	"  CONSTRAINT `fk1` FOREIGN KEY (`p_id`) REFERENCES `p` (`id`) ON DELETE CASCADE,\n" +
	"  CONSTRAINT `fk2` FOREIGN KEY (`p_id`) REFERENCES `other`.`p` (`id`) ON DELETE NO ACTION\n" +
	") ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8 ROW_FORMAT=COMPACT COMMENT='tbl AUTO_INCREMENT=5'"

func Test_ParseShowCreateTable_Mysql57(t *testing.T) {
	parsed, err := parseShowCreateTable(showCreateMysql57)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "a", "b", "c", "d", "p_id"}, parsed.columns())
	require.Equal(t,
		"CREATE TABLE IF NOT EXISTS `db`.`we``ird\nx` (\n"+
			"  `id` int(10) unsigned zerofill NOT NULL AUTO_INCREMENT,\n"+
			"  `a` varchar(10) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT 'it''s a\\\\b FOREIGN KEY (x), y',\n"+
			"  `b` text CHARACTER SET latin7,\n"+
			"  `c` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n"+
			"  `d` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00',\n"+
			"  `p_id` int(11) NOT NULL,\n"+
			"  PRIMARY KEY (`id`),\n"+
			"  KEY `fk2` (`p_id`),\n"+
			"  FULLTEXT KEY `ft` (`b`)\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=utf8 ROW_FORMAT=COMPACT COMMENT='tbl AUTO_INCREMENT=5';",
		parsed.createStatement("db", "we`ird\nx"),
	)
}

// MySQL 8 writes CHECK constraints, functional key parts and partitioning in the statement: they
// are kept, the foreign keys are not.
func Test_ParseShowCreateTable_Mysql8(t *testing.T) {
	shown := "CREATE TABLE `ch` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `p` int DEFAULT NULL,\n" +
		"  `inv` int DEFAULT NULL /*!80023 INVISIBLE */,\n" +
		"  KEY `fkx` (`p`),\n" +
		"  KEY `fx` (((`id` + 1))),\n" +
		"  CONSTRAINT `fkx` FOREIGN KEY (`p`) REFERENCES `p` (`id`),\n" +
		"  CONSTRAINT `c1` CHECK ((`id` > 0))\n" +
		") ENGINE=InnoDB AUTO_INCREMENT=7 DEFAULT CHARSET=latin1\n" +
		"/*!50100 PARTITION BY RANGE (`id`)\n" +
		"(PARTITION p0 VALUES LESS THAN (10) ENGINE = InnoDB,\n" +
		" PARTITION p1 VALUES LESS THAN MAXVALUE ENGINE = InnoDB) */"
	parsed, err := parseShowCreateTable(shown)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "p", "inv"}, parsed.columns())
	require.Equal(t,
		"CREATE TABLE IF NOT EXISTS `s`.`ch` (\n"+
			"  `id` int NOT NULL,\n"+
			"  `p` int DEFAULT NULL,\n"+
			"  `inv` int DEFAULT NULL /*!80023 INVISIBLE */,\n"+
			"  KEY `fkx` (`p`),\n"+
			"  KEY `fx` (((`id` + 1))),\n"+
			"  CONSTRAINT `c1` CHECK ((`id` > 0))\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=latin1\n"+
			"/*!50100 PARTITION BY RANGE (`id`)\n"+
			"(PARTITION p0 VALUES LESS THAN (10) ENGINE = InnoDB,\n"+
			" PARTITION p1 VALUES LESS THAN MAXVALUE ENGINE = InnoDB) */;",
		parsed.createStatement("s", "ch"),
	)
}

func Test_ParseShowCreateTable_UnknownForms(t *testing.T) {
	for name, shown := range map[string]string{
		"view":                    "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`%` SQL SECURITY DEFINER VIEW `v` AS select 1 AS `1`",
		"ansi quotes":             "CREATE TABLE \"t\" (\n  \"id\" int NOT NULL\n)",
		"names not quoted":        "CREATE TABLE t (\n  id int NOT NULL\n)",
		"sequence":                "CREATE SEQUENCE `s` start with 1",
		"unterminated identifier": "CREATE TABLE `t` (\n  `id int NOT NULL\n)",
		"unterminated body":       "CREATE TABLE `t` (\n  `id` int NOT NULL",
		"empty definition":        "CREATE TABLE `t` (\n  `id` int NOT NULL,\n)",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseShowCreateTable(shown)
			require.ErrorIs(t, err, errShowCreateUnreadable)
		})
	}
}

func Test_RewriteShowCreateDatabase(t *testing.T) {
	for name, tc := range map[string]struct{ shown, want string }{
		"mysql 5.7": {
			shown: "CREATE DATABASE /*!32312 IF NOT EXISTS*/ `e1` /*!40100 DEFAULT CHARACTER SET latin1 */",
			want:  "CREATE SCHEMA IF NOT EXISTS `e1` /*!40100 DEFAULT CHARACTER SET latin1 */;",
		},
		"mysql 8": {
			shown: "CREATE DATABASE /*!32312 IF NOT EXISTS*/ `e1` /*!40100 DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci */ /*!80016 DEFAULT ENCRYPTION='N' */",
			want:  "CREATE SCHEMA IF NOT EXISTS `e1` /*!40100 DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci */ /*!80016 DEFAULT ENCRYPTION='N' */;",
		},
		"mariadb": {
			shown: "CREATE DATABASE /*!32312 IF NOT EXISTS*/ `e1` /*!40100 DEFAULT CHARACTER SET latin1 COLLATE latin1_swedish_ci */",
			want:  "CREATE SCHEMA IF NOT EXISTS `e1` /*!40100 DEFAULT CHARACTER SET latin1 COLLATE latin1_swedish_ci */;",
		},
		"odd name": {
			shown: "CREATE DATABASE /*!32312 IF NOT EXISTS*/ `we``ird` /*!40100 DEFAULT CHARACTER SET latin1 */",
			want:  "CREATE SCHEMA IF NOT EXISTS `we``ird` /*!40100 DEFAULT CHARACTER SET latin1 */;",
		},
	} {
		t.Run(name, func(t *testing.T) {
			schema := "e1"
			if name == "odd name" {
				schema = "we`ird"
			}
			actual, err := rewriteShowCreateDatabase(schema, tc.shown)
			require.NoError(t, err)
			require.Equal(t, tc.want, actual)
		})
	}
	_, err := rewriteShowCreateDatabase("e1", "CREATE DATABASE \"e1\"")
	require.ErrorIs(t, err, errShowCreateUnreadable)
}

func Test_WithDdlSqlMode(t *testing.T) {
	plain := "CREATE TABLE IF NOT EXISTS `db`.`back\\slash` (`a` int NOT NULL DEFAULT '0');"
	require.Equal(t, plain, withDdlSqlMode(plain, true), "a backslash in a name needs no mode")

	zero := "ALTER TABLE `db`.`t` ADD COLUMN `d` date NOT NULL DEFAULT '2020-00-00';"
	require.Equal(t, wantDdlSqlMode(zero), withDdlSqlMode(zero, false))

	escaped := "CREATE TABLE IF NOT EXISTS `db`.`t` (`a` int COMMENT 'a\\\\b');"
	require.Equal(t, wantDdlSqlMode(escaped), withDdlSqlMode(escaped, true))
	require.Equal(t, escaped, withDdlSqlMode(escaped, false),
		"a statement built here writes no backslash escape")
}

func Test_BuildTableCol_MysqlAttributes(t *testing.T) {
	text := func(s string) *string { return &s }
	require.Equal(t,
		"`a` varchar(10) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''",
		buildTableColForCreate(&buildTableColRequest{
			ColumnName: "a", DataType: "varchar(10)",
			Mysql: &sqlmanager_shared.MysqlColumn{
				CharacterSet: text("utf8mb4"), Collation: text("utf8mb4_unicode_ci"), EmptyStringDefault: true,
			},
		}),
	)
	require.Equal(t,
		"`c` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)",
		buildTableColForModifyColumn(&buildTableColRequest{
			ColumnName: "c", DataType: "timestamp(3)", ColumnDefault: "CURRENT_TIMESTAMP(3)",
			Mysql: &sqlmanager_shared.MysqlColumn{OnUpdate: text("CURRENT_TIMESTAMP(3)")},
		}),
	)
	// What is not a name, or not an ON UPDATE value, is not written.
	require.Equal(t,
		"`c` timestamp NOT NULL",
		buildTableColForCreate(&buildTableColRequest{
			ColumnName: "c", DataType: "timestamp",
			Mysql: &sqlmanager_shared.MysqlColumn{
				CharacterSet: text("latin1; DROP"), OnUpdate: text("CURRENT_TIMESTAMP; DROP"),
			},
		}),
	)
}

// The table of a job is created from what SHOW CREATE TABLE gives of it, without its foreign
// keys, which are added by the statements built from the catalog.
func Test_GetTableInitStatements_FromShowCreateTable(t *testing.T) {
	querier := mysql_queries.NewMockQuerier(t)
	columns := []*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{}
	for _, name := range []string{"id", "a", "b", "c", "d", "p_id"} {
		columns = append(columns, &mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
			SchemaName: "db", TableName: "we`ird\nx", ColumnName: name, DataType: "int",
			ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
		})
	}
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return(columns, nil)
	querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetTableConstraintsRow{}, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{}, nil)

	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.MatchExpectationsInOrder(false)
	sqlMock.ExpectQuery(regexp.QuoteMeta("FROM information_schema.COLUMNS AS c")).
		WillReturnRows(columnAttributeRows())
	sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `db`.`we``ird\nx`")).
		WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("we`ird\nx", showCreateMysql57))

	manager := &MysqlManager{pool: db, resolvedQuerier: querier, resolvedFlavor: flavorMysql57}
	statements, err := manager.GetTableInitStatements(
		context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "we`ird\nx"}},
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	parsed, err := parseShowCreateTable(showCreateMysql57)
	require.NoError(t, err)
	// The zero date default, and the backslash SHOW CREATE escapes, are read as written.
	require.Equal(t, wantDdlSqlMode(parsed.createStatement("db", "we`ird\nx")), statements[0].CreateTableStatement)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// A table whose statement does not define the columns the catalog shows, a view, and a table the
// session is refused SHOW CREATE TABLE for, are created from the catalog.
func Test_GetTableInitStatements_FromTheCatalog(t *testing.T) {
	for name, answer := range map[string]func(*sqlmock.ExpectedQuery){
		"other columns": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).
				AddRow("t", "CREATE TABLE `t` (\n  `a` int NOT NULL,\n  `hidden` int NOT NULL\n) ENGINE=InnoDB"))
		},
		"view": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"View", "Create View", "character_set_client", "collation_connection"}).
				AddRow("t", "CREATE VIEW `t` AS select 1 AS `a`", "utf8mb4", "utf8mb4_general_ci"))
		},
		"access denied": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnError(&mysqldriver.MySQLError{Number: 1142, Message: "SHOW command denied"})
		},
		"unknown form": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).
				AddRow("t", "CREATE TABLE t (\n  a int NOT NULL\n)"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			querier := mysql_queries.NewMockQuerier(t)
			querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
				Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
					SchemaName: "db", TableName: "t", ColumnName: "a", DataType: "varchar(5)",
					ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
					IdentityGeneration: sql.NullString{Valid: true},
				}}, nil)
			querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
				Return([]*mysql_queries.GetTableConstraintsRow{}, nil)
			querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
				Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{}, nil)
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			sqlMock.MatchExpectationsInOrder(false)
			sqlMock.ExpectQuery(regexp.QuoteMeta("FROM information_schema.COLUMNS AS c")).
				WillReturnRows(columnAttributeRows().AddRow("db", "t", "a", "varchar", "latin1", "latin1_bin", "", 1))
			answer(sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `db`.`t`")))

			manager := &MysqlManager{pool: db, resolvedQuerier: querier, resolvedFlavor: flavorMysql}
			statements, err := manager.GetTableInitStatements(
				context.Background(),
				[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}},
			)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			require.Equal(t,
				"CREATE TABLE IF NOT EXISTS `db`.`t` (`a` varchar(5) CHARACTER SET latin1 COLLATE latin1_bin NOT NULL DEFAULT '');",
				statements[0].CreateTableStatement)
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

// A database is created with the character set it has on the source, or from its name alone when
// the session is refused SHOW CREATE DATABASE.
func Test_BuildCreateSchemaStatement(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE DATABASE IF NOT EXISTS `Legacy57`")).
		WillReturnRows(sqlmock.NewRows([]string{"Database", "Create Database"}).
			AddRow("Legacy57", "CREATE DATABASE /*!32312 IF NOT EXISTS*/ `Legacy57` /*!40100 DEFAULT CHARACTER SET latin1 */"))
	sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE DATABASE IF NOT EXISTS `hidden`")).
		WillReturnError(&mysqldriver.MySQLError{Number: 1044, Message: "Access denied"})
	manager := &MysqlManager{pool: db}

	statement, err := manager.buildCreateSchemaStatement(context.Background(), "Legacy57")
	require.NoError(t, err)
	require.Equal(t, "CREATE SCHEMA IF NOT EXISTS `Legacy57` /*!40100 DEFAULT CHARACTER SET latin1 */;", statement)

	statement, err = manager.buildCreateSchemaStatement(context.Background(), "hidden")
	require.NoError(t, err)
	require.Equal(t, "CREATE SCHEMA IF NOT EXISTS `hidden`;", statement)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func Test_WithoutAutoIncrementOption(t *testing.T) {
	require.Equal(t,
		" ENGINE=MyISAM DEFAULT CHARSET=latin1 COMMENT=' AUTO_INCREMENT=3'",
		withoutAutoIncrementOption(" ENGINE=MyISAM AUTO_INCREMENT=12 DEFAULT CHARSET=latin1 COMMENT=' AUTO_INCREMENT=3'"),
	)
}

// A row format the table has without SHOW CREATE TABLE showing it is written, when it is not
// the default of the source server; nothing else is changed.
func Test_WithImplicitRowFormat(t *testing.T) {
	cases := []struct {
		name, options, engine, rowFormat, defaultFormat, want string
	}{
		{
			name:    "compact under a dynamic default",
			options: " ENGINE=InnoDB DEFAULT CHARSET=latin1 COMMENT='ENGINE=MyISAM'",
			engine:  "InnoDB", rowFormat: "Compact", defaultFormat: "dynamic",
			want: " ENGINE=InnoDB ROW_FORMAT=COMPACT DEFAULT CHARSET=latin1 COMMENT='ENGINE=MyISAM'",
		},
		{
			name:    "before the partitioning",
			options: " ENGINE=InnoDB DEFAULT CHARSET=utf8\n/*!50100 PARTITION BY HASH (`id`)\nPARTITIONS 2 */",
			engine:  "InnoDB", rowFormat: "Redundant", defaultFormat: "dynamic",
			want: " ENGINE=InnoDB ROW_FORMAT=REDUNDANT DEFAULT CHARSET=utf8\n/*!50100 PARTITION BY HASH (`id`)\nPARTITIONS 2 */",
		},
		{
			name:    "the default of the server",
			options: " ENGINE=InnoDB DEFAULT CHARSET=latin1",
			engine:  "InnoDB", rowFormat: "Dynamic", defaultFormat: "dynamic",
			want: " ENGINE=InnoDB DEFAULT CHARSET=latin1",
		},
		{
			name:    "written already",
			options: " ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT",
			engine:  "InnoDB", rowFormat: "Compact", defaultFormat: "dynamic",
			want: " ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT",
		},
		{
			name:    "compressed by its key block size",
			options: " ENGINE=InnoDB DEFAULT CHARSET=latin1 KEY_BLOCK_SIZE=8",
			engine:  "InnoDB", rowFormat: "Compressed", defaultFormat: "dynamic",
			want: " ENGINE=InnoDB DEFAULT CHARSET=latin1 KEY_BLOCK_SIZE=8",
		},
		{
			name:    "MyISAM",
			options: " ENGINE=MyISAM DEFAULT CHARSET=latin1",
			engine:  "MyISAM", rowFormat: "Fixed", defaultFormat: "dynamic",
			want: " ENGINE=MyISAM DEFAULT CHARSET=latin1",
		},
		{
			name:    "no default read",
			options: " ENGINE=InnoDB DEFAULT CHARSET=latin1",
			engine:  "InnoDB", rowFormat: "Compact", defaultFormat: "",
			want: " ENGINE=InnoDB DEFAULT CHARSET=latin1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, withImplicitRowFormat(c.options, c.engine, c.rowFormat, c.defaultFormat))
		})
	}
}

// A table created COMPACT under an older default, on a source server whose default is now
// DYNAMIC, is created COMPACT on the destination.
func Test_GetTableInitStatements_ImplicitRowFormat(t *testing.T) {
	const shown = "CREATE TABLE `t` (\n  `id` int(11) NOT NULL,\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB DEFAULT CHARSET=latin1"
	querier := mysql_queries.NewMockQuerier(t)
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
			SchemaName: "db", TableName: "t", ColumnName: "id", DataType: "int",
			ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
		}}, nil)
	querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetTableConstraintsRow{}, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{}, nil)

	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.MatchExpectationsInOrder(false)
	sqlMock.ExpectQuery(regexp.QuoteMeta("FROM information_schema.COLUMNS AS c")).
		WillReturnRows(sqlmock.NewRows([]string{"x"}))
	sqlMock.ExpectQuery(regexp.QuoteMeta("SELECT @@GLOBAL.innodb_default_row_format")).
		WillReturnRows(sqlmock.NewRows([]string{"f"}).AddRow("dynamic"))
	sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `db`.`t`")).
		WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("t", shown))
	sqlMock.ExpectQuery(regexp.QuoteMeta("SELECT ENGINE, ROW_FORMAT FROM information_schema.TABLES")).
		WithArgs("db", "t").
		WillReturnRows(sqlmock.NewRows([]string{"ENGINE", "ROW_FORMAT"}).AddRow("InnoDB", "Compact"))

	manager := &MysqlManager{pool: db, resolvedQuerier: querier, resolvedFlavor: flavorMysql57}
	statements, err := manager.GetTableInitStatements(
		context.Background(), []*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}})
	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Contains(t, statements[0].CreateTableStatement, ") ENGINE=InnoDB ROW_FORMAT=COMPACT DEFAULT CHARSET=latin1;")
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// MySQL 8 writes a character of four bytes as ???? in the expressions of SHOW CREATE TABLE: the
// expressions the catalog holds are written instead.
func Test_UseCatalogExpressions(t *testing.T) {
	shown := "CREATE TABLE `ck` (\n" +
		"  `c` varchar(40) DEFAULT NULL,\n" +
		"  `d` varchar(5) DEFAULT 0xF09F9880,\n" +
		"  `g` varchar(10) GENERATED ALWAYS AS (concat(`c`,_utf8mb4'????')) VIRTUAL,\n" +
		"  `q` varchar(10) DEFAULT '?',\n" +
		"  CONSTRAINT `p` CHECK ((`c` <> _utf8mb4'????')) /*!80016 NOT ENFORCED */\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	parsed, err := parseShowCreateTable(shown)
	require.NoError(t, err)
	parsed.useCatalogExpressions(
		map[string]string{"p": "(`c` <> _utf8mb4\\'😀\\')"},
		map[string]string{"c": "", "d": "", "g": "concat(`c`,_utf8mb4'😀')", "q": ""},
	)
	require.Equal(t,
		"CREATE TABLE IF NOT EXISTS `s`.`ck` (\n"+
			"  `c` varchar(40) DEFAULT NULL,\n"+
			"  `d` varchar(5) DEFAULT 0xF09F9880,\n"+
			"  `g` varchar(10) GENERATED ALWAYS AS (concat(`c`,_utf8mb4'😀')) VIRTUAL,\n"+
			"  `q` varchar(10) DEFAULT '?',\n"+
			"  CONSTRAINT `p` CHECK ((`c` <> _utf8mb4\\'😀\\')) /*!80016 NOT ENFORCED */\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;",
		parsed.createStatement("s", "ck"),
	)
}

func Test_WithoutDefiner(t *testing.T) {
	for statement, want := range map[string]string{
		"CREATE DEFINER=`root`@`%` TRIGGER `t` BEFORE INSERT ON `x` FOR EACH ROW SET NEW.a = 1": "TRIGGER `t` BEFORE INSERT ON `x` FOR EACH ROW SET NEW.a = 1",
		"CREATE DEFINER=`we``ird`@`local host` FUNCTION `f`() RETURNS int\nRETURN 1":            "FUNCTION `f`() RETURNS int\nRETURN 1",
		"CREATE DEFINER = 'app'@'10.%' TRIGGER `t` AFTER UPDATE ON `x` FOR EACH ROW BEGIN\nEND": "TRIGGER `t` AFTER UPDATE ON `x` FOR EACH ROW BEGIN\nEND",
		"CREATE DEFINER=CURRENT_USER TRIGGER `t` BEFORE DELETE ON `x` FOR EACH ROW SET @a = 1":  "TRIGGER `t` BEFORE DELETE ON `x` FOR EACH ROW SET @a = 1",
		"CREATE TRIGGER `t` BEFORE DELETE ON `x` FOR EACH ROW SET @a = 1":                       "TRIGGER `t` BEFORE DELETE ON `x` FOR EACH ROW SET @a = 1",
	} {
		kind := "TRIGGER"
		if strings.Contains(statement, "FUNCTION") {
			kind = "FUNCTION"
		}
		actual, ok := withoutDefiner(statement, kind)
		require.True(t, ok, statement)
		require.Equal(t, want, actual)
	}
	_, ok := withoutDefiner("CREATE DEFINER=`root`@`%` PROCEDURE `p`() BEGIN END", "FUNCTION")
	require.False(t, ok)
}

// A trigger is created from its original text, in the database of its table, under the session
// it was created in; a function under its own, named with its database.
func Test_BuildRoutinesFromShowCreate(t *testing.T) {
	trigger, ok := buildTriggerFromShowCreate("db", "db", "trg", &shownRoutine{
		statement:          "CREATE DEFINER=`root`@`%` TRIGGER `trg` BEFORE INSERT ON `Client` FOR EACH ROW\nSET NEW.`a` = 'é'",
		sqlMode:            "",
		characterSetClient: "latin1",
		collation:          "latin1_swedish_ci",
	})
	require.True(t, ok)
	require.Equal(t,
		"SET @husonym_routine_sql_mode = @@SESSION.sql_mode, "+
			"@husonym_routine_character_set_client = @@SESSION.character_set_client, "+
			"@husonym_routine_collation_connection = @@SESSION.collation_connection;\n"+
			"SET SESSION sql_mode = '', collation_connection = latin1_swedish_ci;\n"+
			"USE `db`;\n"+
			"DROP TRIGGER IF EXISTS `db`.`trg`;\n"+
			"CREATE TRIGGER `trg` BEFORE INSERT ON `Client` FOR EACH ROW\nSET NEW.`a` = 'é';\n"+
			"SET SESSION sql_mode = @husonym_routine_sql_mode, "+
			"character_set_client = @husonym_routine_character_set_client, "+
			"collation_connection = @husonym_routine_collation_connection;",
		trigger)

	routine := &shownRoutine{
		statement:          "CREATE DEFINER=`root`@`%` FUNCTION `Prix_TTC`(`ht` DECIMAL(10,2)) RETURNS decimal(10,2)\n    NO SQL\n    DETERMINISTIC\nRETURN ROUND(`ht` * 1.20, 2)",
		sqlMode:            "STRICT_TRANS_TABLES",
		characterSetClient: "utf8mb4",
		collation:          "utf8mb4_general_ci",
	}
	function, ok := buildFunctionFromShowCreate(flavorMysql, "db", "Prix_TTC", routine)
	require.True(t, ok)
	require.Contains(t, function,
		"SET SESSION sql_mode = 'STRICT_TRANS_TABLES', character_set_client = utf8mb4, collation_connection = utf8mb4_general_ci;\n"+
			"CREATE FUNCTION IF NOT EXISTS `db`.`Prix_TTC`(`ht` DECIMAL(10,2)) RETURNS decimal(10,2)\n    NO SQL\n    DETERMINISTIC\nRETURN ROUND(`ht` * 1.20, 2);\n")
	function, ok = buildFunctionFromShowCreate(flavorMysql57, "db", "Prix_TTC", routine)
	require.True(t, ok)
	require.Contains(t, function,
		"DROP FUNCTION IF EXISTS `db`.`Prix_TTC`;\nCREATE FUNCTION `db`.`Prix_TTC`(`ht` DECIMAL(10,2))")
}
