package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"encoding/json"
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

func Test_DetectServerFlavor(t *testing.T) {
	for version, expected := range map[string]serverFlavor{
		"5.7.42-log":              flavorMysql57,
		"5.7.42":                  flavorMysql57,
		"5.7.42-46-log":           flavorMysql57,
		"5.7.44-0ubuntu0.18.04.1": flavorMysql57,
		"5.6.51":                  flavorMysql57,
		"8.0.36":                  flavorMysql,
		"8.0.36-0ubuntu0.22.04.1": flavorMysql,
		"8.4.3":                   flavorMysql,
		"9.1.0":                   flavorMysql,
		"10.11.6-MariaDB-1:10.11.6+maria~ubu2204": flavorMariadb,
		"11.4.2-MariaDB":            flavorMariadb,
		"5.5.5-10.6.12-MariaDB-log": flavorMariadb,
		"":                          flavorMysql,
		"unknown":                   flavorMysql,
		"v5.7":                      flavorMysql,
	} {
		require.Equal(t, expected, detectServerFlavor(version), "version %q", version)
	}
}

func Test_GetQuerier_SelectsByVersion(t *testing.T) {
	for version, check := range map[string]func(t *testing.T, inner, actual mysql_queries.Querier){
		"5.7.42-log": func(t *testing.T, _, actual mysql_queries.Querier) {
			require.IsType(t, &mysql57Querier{}, actual)
		},
		"8.0.36": func(t *testing.T, inner, actual mysql_queries.Querier) {
			require.Same(t, inner, actual)
		},
		"8.4.3": func(t *testing.T, inner, actual mysql_queries.Querier) {
			require.Same(t, inner, actual)
		},
		"10.11.6-MariaDB": func(t *testing.T, _, actual mysql_queries.Querier) {
			require.IsType(t, &mariadbQuerier{}, actual)
		},
		"not a version": func(t *testing.T, inner, actual mysql_queries.Querier) {
			require.Same(t, inner, actual)
		},
	} {
		t.Run(version, func(t *testing.T) {
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			// Asked once: the querier is memoized.
			sqlMock.ExpectQuery(regexp.QuoteMeta("SELECT VERSION()")).
				WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow(version))

			inner := mysql_queries.New()
			manager := NewManager(inner, db, func() {})
			first, err := manager.getQuerier(context.Background())
			require.NoError(t, err)
			check(t, inner, first)
			second, err := manager.getQuerier(context.Background())
			require.NoError(t, err)
			require.Same(t, first, second)
			require.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func Test_NormalizeMysql57Default(t *testing.T) {
	type expectation struct {
		columnType string
		def        string
		extra      string
		// wantExtra is nil when EXTRA must become NULL
		wantExtra *string
	}
	str := func(s string) *string { return &s }
	for name, tc := range map[string]expectation{
		"current timestamp":             {"timestamp", "CURRENT_TIMESTAMP", "", nil},
		"current timestamp on update":   {"timestamp", "CURRENT_TIMESTAMP", "on update CURRENT_TIMESTAMP", nil},
		"current timestamp fsp":         {"datetime(3)", "CURRENT_TIMESTAMP(3)", "on update CURRENT_TIMESTAMP(3)", nil},
		"zero date stays a literal":     {"timestamp", "0000-00-00 00:00:00", "", str("")},
		"zero date on update":           {"timestamp", "0000-00-00 00:00:00", "on update CURRENT_TIMESTAMP", str("")},
		"literal on a varchar":          {"varchar(40)", "CURRENT_TIMESTAMP", "", str("")},
		"plain literal":                 {"int(11)", "0", "", str("")},
		"auto_increment kept":           {"int(11)", "", "auto_increment", str("auto_increment")},
		"no default keeps on update":    {"timestamp", "", "on update CURRENT_TIMESTAMP", str("on update CURRENT_TIMESTAMP")},
		"generated column kept":         {"int(11)", "", "VIRTUAL GENERATED", str("VIRTUAL GENERATED")},
		"lowercase reported by a proxy": {"timestamp", "current_timestamp", "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			extra := sql.NullString{String: tc.extra, Valid: true}
			normalizeMysql57Default([]uint8(tc.def), tc.columnType, &extra)
			if tc.wantExtra == nil {
				require.False(t, extra.Valid)
				return
			}
			require.True(t, extra.Valid)
			require.Equal(t, *tc.wantExtra, extra.String)
		})
	}
}

// A 5.7 table is created with its timestamp defaults written in a form 5.7 accepts:
// CURRENT_TIMESTAMP bare, a zero date quoted, whether or not the column is updated on
// CURRENT_TIMESTAMP.
func Test_GetTableInitStatements_Mysql57Defaults(t *testing.T) {
	column := func(name, columnType, columnDefault, extra string, nullable int32) *mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow {
		return &mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
			SchemaName: "db", TableName: "t", ColumnName: name, DataType: columnType, ColumnType: columnType,
			IsNullable: nullable, ColumnDefault: []uint8(columnDefault), GenerationExp: []uint8(""),
			IdentityGeneration: sql.NullString{String: extra, Valid: true},
		}
	}
	inner := newStubQuerier(t, []*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
		column("created_at", "timestamp", "CURRENT_TIMESTAMP", "", 0),
		column("updated_at", "timestamp", "CURRENT_TIMESTAMP", "on update CURRENT_TIMESTAMP", 0),
		column("precise_at", "datetime(3)", "CURRENT_TIMESTAMP(3)", "", 0),
		column("legacy_at", "timestamp", "0000-00-00 00:00:00", "on update CURRENT_TIMESTAMP", 0),
		column("label", "varchar(40)", "CURRENT_TIMESTAMP", "", 1),
	}, true)

	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.MatchExpectationsInOrder(false)
	sqlMock.ExpectQuery(regexp.QuoteMeta("-- name: GetTableConstraints :many")).
		WillReturnRows(sqlmock.NewRows(constraintColumns))
	sqlMock.ExpectQuery(regexp.QuoteMeta("-- name: GetIndicesBySchemasAndTables :many")).
		WillReturnRows(sqlmock.NewRows(indexColumns))
	// The attributes of the columns, as 5.7 reports them: the ON UPDATE clause is read here, the
	// querier taking it out of EXTRA.
	sqlMock.ExpectQuery(regexp.QuoteMeta("FROM information_schema.COLUMNS AS c")).
		WillReturnRows(columnAttributeRows().
			AddRow("db", "t", "created_at", "timestamp", nil, nil, "", 0).
			AddRow("db", "t", "updated_at", "timestamp", nil, nil, "on update CURRENT_TIMESTAMP", 0).
			AddRow("db", "t", "precise_at", "datetime", nil, nil, "", 0).
			AddRow("db", "t", "legacy_at", "timestamp", nil, nil, "on update CURRENT_TIMESTAMP", 0).
			AddRow("db", "t", "label", "varchar", "latin1", "latin1_swedish_ci", "", 0))
	// The table is built from the catalog here: the session is refused SHOW CREATE TABLE.
	sqlMock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `db`.`t`")).
		WillReturnError(&mysqldriver.MySQLError{Number: 1142, Message: "SHOW command denied"})

	manager := &MysqlManager{
		pool:            db,
		resolvedQuerier: newMysql57Querier(inner),
		resolvedFlavor:  flavorMysql57,
	}
	statements, err := manager.GetTableInitStatements(
		context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}},
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	// The ON UPDATE clauses and the character set of the source are kept, and the zero date
	// default is written under a sql_mode that takes it whatever the one of the destination.
	require.Equal(t,
		wantDdlSqlMode("CREATE TABLE IF NOT EXISTS `db`.`t` ("+
			"`created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP, "+
			"`updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, "+
			"`precise_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), "+
			"`legacy_at` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00' ON UPDATE CURRENT_TIMESTAMP, "+
			"`label` varchar(40) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL DEFAULT 'CURRENT_TIMESTAMP');"),
		statements[0].CreateTableStatement,
	)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// The columns read from 5.7 for a schema diff carry the same defaults, so that an added or
// modified column is written as the table was.
func Test_GetColumnsByTables_Mysql57Defaults(t *testing.T) {
	inner := mysql_queries.NewMockQuerier(t)
	inner.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{
			{
				SchemaName: "db", TableName: "t", ColumnName: "updated_at", DataType: "timestamp",
				ColumnType: "timestamp", ColumnDefault: []uint8("CURRENT_TIMESTAMP"), GenerationExp: []uint8(""),
				IdentityGeneration: sql.NullString{String: "on update CURRENT_TIMESTAMP", Valid: true},
			},
			{
				SchemaName: "db", TableName: "t", ColumnName: "legacy_at", DataType: "timestamp",
				ColumnType: "timestamp", ColumnDefault: []uint8("0000-00-00 00:00:00"), GenerationExp: []uint8(""),
				IdentityGeneration: sql.NullString{String: "on update CURRENT_TIMESTAMP", Valid: true},
			},
		}, nil)
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.ExpectQuery(regexp.QuoteMeta("FROM information_schema.COLUMNS AS c")).
		WillReturnRows(columnAttributeRows().
			AddRow("db", "t", "updated_at", "timestamp", nil, nil, "on update CURRENT_TIMESTAMP", 0).
			AddRow("db", "t", "legacy_at", "timestamp", nil, nil, "on update CURRENT_TIMESTAMP", 0))
	manager := &MysqlManager{pool: db, resolvedQuerier: newMysql57Querier(inner), resolvedFlavor: flavorMysql57}

	columns, err := manager.GetColumnsByTables(
		context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}},
	)
	require.NoError(t, err)
	require.Len(t, columns, 2)
	statements := map[string]string{}
	for _, column := range columns {
		statement, err := BuildAddColumnStatement(column)
		require.NoError(t, err)
		statements[column.Name] = statement
	}
	require.Equal(t, map[string]string{
		"updated_at": "ALTER TABLE `db`.`t` ADD COLUMN `updated_at` timestamp NOT NULL " +
			"DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;",
		"legacy_at": "ALTER TABLE `db`.`t` ADD COLUMN `legacy_at` timestamp NOT NULL " +
			"DEFAULT '0000-00-00 00:00:00' ON UPDATE CURRENT_TIMESTAMP;",
	}, statements)
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// columnAttributeRows are the rows of getColumnAttributes.
func columnAttributeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME", "DATA_TYPE", "CHARACTER_SET_NAME", "COLLATION_NAME",
		"EXTRA", "empty_default",
	})
}

// wantDdlSqlMode is a statement run under the sql_mode withDdlSqlMode sets.
func wantDdlSqlMode(statement string) string {
	return "SET @husonym_ddl_sql_mode = @@SESSION.sql_mode;\n" +
		"SET SESSION sql_mode = REPLACE(REPLACE(REPLACE(@@SESSION.sql_mode, " +
		"'NO_ZERO_IN_DATE', ''), 'NO_ZERO_DATE', ''), 'NO_BACKSLASH_ESCAPES', '');\n" +
		statement + "\n" +
		"SET SESSION sql_mode = @husonym_ddl_sql_mode;"
}

func Test_Mysql57Querier_GetDatabaseSchema(t *testing.T) {
	inner := mysql_queries.NewMockQuerier(t)
	inner.EXPECT().GetDatabaseSchema(mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseSchemaRow{
			{
				TableSchema: "db", TableName: "t", ColumnName: "updated_at", DataType: "timestamp",
				ColumnType: "timestamp", ColumnDefault: []uint8("CURRENT_TIMESTAMP"), IsNullable: "NO",
				Extra: sql.NullString{String: "on update CURRENT_TIMESTAMP", Valid: true},
			},
			{
				TableSchema: "db", TableName: "t", ColumnName: "legacy_at", DataType: "timestamp",
				ColumnType: "timestamp", ColumnDefault: []uint8("0000-00-00 00:00:00"), IsNullable: "NO",
				Extra: sql.NullString{String: "on update CURRENT_TIMESTAMP", Valid: true},
			},
		}, nil)
	manager := &MysqlManager{resolvedQuerier: newMysql57Querier(inner), resolvedFlavor: flavorMysql57}

	rows, err := manager.GetDatabaseSchema(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "CURRENT_TIMESTAMP", rows[0].ColumnDefault)
	require.Nil(t, rows[0].ColumnDefaultType)
	require.Nil(t, rows[0].GeneratedType)
	require.Equal(t, "0000-00-00 00:00:00", rows[1].ColumnDefault)
	require.NotNil(t, rows[1].ColumnDefaultType)
	require.Equal(t, columnDefaultString, *rows[1].ColumnDefaultType)
}

var constraintColumns = []string{
	"schema_name", "table_name", "constraint_columns", "not_nullable", "constraint_name",
	"constraint_type", "referenced_schema_name", "referenced_table_name", "referenced_column_names",
	"constraint_column_prefixes", "update_rule", "delete_rule", "check_clause",
}

var indexColumns = []string{
	"schema_name", "table_name", "column_name", "expression", "index_name", "index_type",
	"seq_in_index", "sub_part", "nullable",
}

// queryRecorder matches any query and keeps the last one it saw, so that a test can read
// the statement that was sent.
type queryRecorder struct {
	last string
}

func (r *queryRecorder) Match(_, actual string) error {
	r.last = actual
	return nil
}

func newRecordingMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *queryRecorder) {
	t.Helper()
	recorder := &queryRecorder{}
	db, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(recorder))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlMock, recorder
}

func Test_Mysql57Querier_GetTableConstraints(t *testing.T) {
	t.Run("one placeholder per table, no check_constraints", func(t *testing.T) {
		db, sqlMock, recorder := newRecordingMock(t)
		sqlMock.ExpectQuery("").
			WithArgs("db", "a", "b").
			WillReturnRows(sqlmock.NewRows(constraintColumns).AddRow(
				"db", "a", []byte(`["id"]`), []byte(`[1]`), "PRIMARY", "PRIMARY KEY", "NULL", "NULL",
				[]byte(`[null]`), []byte(`[{"column":"id","sub_part":null}]`), nil, nil, []byte(""),
			).AddRow(
				"db", "b", []byte(`["a_id"]`), []byte(`[0]`), "fk_a", "FOREIGN KEY", "db", "a",
				[]byte(`["id"]`), []byte(`[{"column":"a_id","sub_part":null}]`), "CASCADE", "RESTRICT", []byte(""),
			))

		rows, err := newMysql57Querier(nil).GetTableConstraints(context.Background(), db,
			&mysql_queries.GetTableConstraintsParams{Schema: "db", Tables: []string{"a", "b"}})
		require.NoError(t, err)
		require.NoError(t, sqlMock.ExpectationsWereMet())

		require.Contains(t, recorder.last, "tc.table_schema = ?")
		require.Contains(t, recorder.last, "tc.table_name IN (?,?)")
		require.NotContains(t, recorder.last, "SLICE")
		require.NotContains(t, recorder.last, "check_constraints")
		require.NotContains(t, recorder.last, "cc.")

		require.Len(t, rows, 2)
		require.Equal(t, &mysql_queries.GetTableConstraintsRow{
			SchemaName: "db", TableName: "a", ConstraintName: "PRIMARY", ConstraintType: "PRIMARY KEY",
			ConstraintColumns: json.RawMessage(`["id"]`), NotNullable: json.RawMessage(`[1]`),
			ReferencedSchemaName: "NULL", ReferencedTableName: "NULL",
			ReferencedColumnNames:    json.RawMessage(`[null]`),
			ConstraintColumnPrefixes: json.RawMessage(`[{"column":"id","sub_part":null}]`),
			CheckClause:              []byte(""),
		}, rows[0])
		require.Equal(t, sql.NullString{String: "CASCADE", Valid: true}, rows[1].UpdateRule)
		require.Equal(t, sql.NullString{String: "RESTRICT", Valid: true}, rows[1].DeleteRule)

		// The rows go through the manager as those of MySQL 8 do.
		stmt, err := buildAlterStatementByConstraint(rows[1])
		require.NoError(t, err)
		require.Contains(t, stmt.Statement,
			"ADD CONSTRAINT `fk_a` FOREIGN KEY (`a_id`) REFERENCES `db`.`a`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;")
	})

	t.Run("no table matches nothing", func(t *testing.T) {
		db, sqlMock, recorder := newRecordingMock(t)
		sqlMock.ExpectQuery("").WithArgs("db").WillReturnRows(sqlmock.NewRows(constraintColumns))

		rows, err := newMysql57Querier(nil).GetTableConstraints(context.Background(), db,
			&mysql_queries.GetTableConstraintsParams{Schema: "db"})
		require.NoError(t, err)
		require.Empty(t, rows)
		require.Contains(t, recorder.last, "tc.table_name IN (NULL)")
		require.NoError(t, sqlMock.ExpectationsWereMet())
	})
}

func Test_Mysql57Querier_GetTableConstraintsBySchemas(t *testing.T) {
	db, sqlMock, recorder := newRecordingMock(t)
	sqlMock.ExpectQuery("").
		WithArgs("s1", "s2").
		WillReturnRows(sqlmock.NewRows(constraintColumns).AddRow(
			"s1", "t", []byte(`["email"]`), []byte(`[1]`), "uniq_email", "UNIQUE", "NULL", "NULL",
			[]byte(`[null]`), []byte(`[{"column":"email","sub_part":191}]`), nil, nil, []byte(""),
		))

	manager := &MysqlManager{pool: db, resolvedQuerier: newMysql57Querier(nil), resolvedFlavor: flavorMysql57}
	constraints, err := manager.GetTableConstraintsBySchema(context.Background(), []string{"s1", "s2"})
	require.NoError(t, err)
	require.NoError(t, sqlMock.ExpectationsWereMet())

	require.Contains(t, recorder.last, "tc.table_schema IN (?,?)")
	require.NotContains(t, recorder.last, "check_constraints")
	require.Equal(t, map[string][][]string{"s1.t": {{"email"}}}, constraints.UniqueConstraints)
	require.Empty(t, constraints.CheckConstraints)

	t.Run("no schema matches nothing", func(t *testing.T) {
		db, sqlMock, recorder := newRecordingMock(t)
		sqlMock.ExpectQuery("").WillReturnRows(sqlmock.NewRows(constraintColumns))
		rows, err := newMysql57Querier(nil).GetTableConstraintsBySchemas(context.Background(), db, nil)
		require.NoError(t, err)
		require.Empty(t, rows)
		require.Contains(t, recorder.last, "tc.table_schema IN (NULL)")
		require.NoError(t, sqlMock.ExpectationsWereMet())
	})
}

func Test_Mysql57Querier_GetIndicesBySchemasAndTables(t *testing.T) {
	db, sqlMock, recorder := newRecordingMock(t)
	sqlMock.ExpectQuery("").
		WithArgs("db", "t").
		WillReturnRows(sqlmock.NewRows(indexColumns).AddRow(
			"db", "t", "name", nil, "idx_name", "BTREE", int64(1), int64(20), "YES",
		))

	rows, err := newMysql57Querier(nil).GetIndicesBySchemasAndTables(context.Background(), db,
		&mysql_queries.GetIndicesBySchemasAndTablesParams{Schema: "db", Tables: []string{"t"}})
	require.NoError(t, err)
	require.NoError(t, sqlMock.ExpectationsWereMet())
	require.Contains(t, recorder.last, "NULL as expression")
	require.NotContains(t, recorder.last, "s.EXPRESSION")
	require.Contains(t, recorder.last, "s.TABLE_NAME in (?)")
	require.Equal(t, []*mysql_queries.GetIndicesBySchemasAndTablesRow{{
		SchemaName: "db", TableName: "t", ColumnName: sql.NullString{String: "name", Valid: true},
		IndexName: "idx_name", IndexType: "BTREE",
		SeqInIndex: sql.NullInt64{Int64: 1, Valid: true}, SubPart: sql.NullInt64{Int64: 20, Valid: true},
		Nullable: "YES",
	}}, rows)
}

// MySQL 5.7 has neither CREATE TRIGGER IF NOT EXISTS nor CREATE FUNCTION IF NOT EXISTS
// (8.0.29+): the statements read from it drop the object, then create it.
func Test_Mysql57_TriggersAndFunctionsAreReplaced(t *testing.T) {
	t.Run("trigger", func(t *testing.T) {
		actual, err := buildIdempotentTriggerStatement(flavorMysql57, "db", "t", "trg", "db",
			"BEFORE", "INSERT", "ROW", "SET NEW.id = 1")
		require.NoError(t, err)
		require.Equal(t,
			"DROP TRIGGER IF EXISTS `db`.`trg`;\nCREATE TRIGGER `db`.`trg`\nBEFORE INSERT ON `db`.`t`\nFOR EACH ROW\nSET NEW.id = 1;",
			actual)
		require.NotContains(t, actual, "IF NOT EXISTS")
	})

	t.Run("function", func(t *testing.T) {
		actual, err := buildIdempotentFunctionStatement(flavorMysql57, "db", "fn", "a int", "int", "RETURN a + 1", true)
		require.NoError(t, err)
		require.Equal(t,
			"DROP FUNCTION IF EXISTS `db`.`fn`;\nCREATE FUNCTION `db`.`fn`(a int)\nRETURNS int\nDETERMINISTIC\nRETURN a + 1;",
			actual)
	})

	t.Run("other flavors keep IF NOT EXISTS", func(t *testing.T) {
		for _, flavor := range []serverFlavor{flavorMysql, flavorMariadb} {
			trigger, err := buildIdempotentTriggerStatement(flavor, "db", "t", "trg", "db",
				"BEFORE", "INSERT", "ROW", "SET NEW.id = 1")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(trigger, "CREATE TRIGGER IF NOT EXISTS "))
			function, err := buildIdempotentFunctionStatement(flavor, "db", "fn", "a int", "int", "RETURN a + 1", true)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(function, "CREATE FUNCTION IF NOT EXISTS "))
		}
	})
}

func Test_CreatesTriggersIfNotExists(t *testing.T) {
	for version, expected := range map[string]bool{
		"5.7.42-log":      false,
		"5.6.51":          false,
		"8.0.28":          false,
		"8.0.29":          true,
		"8.0.36-log":      true,
		"8.4.3":           true,
		"9.1.0":           true,
		"10.11.6-MariaDB": true,
		"":                true,
		"garbage":         true,
	} {
		require.Equal(t, expected, CreatesTriggersIfNotExists(version), "version %q", version)
	}
}
