package sqlmanager_mssql

import (
	"context"
	"database/sql"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testQuoting requires the two quoting functions to write what the server itself writes.
func testQuoting(t *testing.T, server *testServer) {
	names := []string{
		"plain", "Order Lines", "we]ird", "a]]b", "we[ird", "it's", "''", `say "hi"`, "a.b", "x, y",
		"select", "données_日本", "100%", strings.Repeat("n", 128), strings.Repeat("]", 64),
	}
	for _, name := range names {
		var identifier, literal string
		err := server.container.DB.QueryRowContext(
			t.Context(), "SELECT QUOTENAME(@p1), QUOTENAME(@p1, '''')", name,
		).Scan(&identifier, &literal)
		require.NoError(t, err, name)
		require.Equal(t, identifier, ddl.QuoteIdentifier(name), name)
		require.Equal(t, "N"+literal, ddl.QuoteLiteral(name), name)
	}
}

// interferingQuerier changes the catalog once, while the columns are read for the first time.
type interferingQuerier struct {
	mssql_queries.Querier
	interfere   func()
	once        sync.Once
	columnReads atomic.Int32
}

func (q *interferingQuerier) GetColumns(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*mssql_queries.GetColumnsRow, error) {
	q.columnReads.Add(1)
	q.once.Do(q.interfere)
	return q.Querier.GetColumns(ctx, db, ids)
}

// testChangingCatalog changes a table between the two looks at its version: the plan tells the
// catalog as it is after the change, read a second time.
func testChangingCatalog(t *testing.T, server *testServer) {
	db := server.database(t, "rt_changing", "")
	for _, statement := range []string{
		"CREATE TABLE dbo.t (id int NOT NULL CONSTRAINT PK_t PRIMARY KEY, v int NULL, w int NULL)",
		"CREATE INDEX IX_dropped ON dbo.t (v)",
		"ALTER TABLE dbo.t ADD CONSTRAINT CK_dropped CHECK (v > 0)",
		"CREATE TRIGGER dbo.trg_dropped ON dbo.t AFTER INSERT AS RETURN",
		"CREATE TRIGGER dbo.trg_disabled ON dbo.t AFTER DELETE AS RETURN",
	} {
		_, err := db.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}

	cases := []struct {
		name   string
		change string
		// absent and present are parts of statements the plan must not hold, and must hold.
		absent  string
		present string
	}{
		{name: "an index dropped", change: "DROP INDEX IX_dropped ON dbo.t", absent: "[IX_dropped]"},
		{name: "an index created", change: "CREATE INDEX IX_created ON dbo.t (w)", present: "[IX_created]"},
		{name: "a constraint dropped", change: "ALTER TABLE dbo.t DROP CONSTRAINT CK_dropped", absent: "[CK_dropped]"},
		{
			name:    "a constraint added",
			change:  "ALTER TABLE dbo.t ADD CONSTRAINT CK_added CHECK (w > 0)",
			present: "[CK_added]",
		},
		{name: "a trigger dropped", change: "DROP TRIGGER dbo.trg_dropped", absent: "[trg_dropped]"},
		{
			name:    "a trigger created",
			change:  "CREATE TRIGGER dbo.trg_created ON dbo.t AFTER UPDATE AS RETURN",
			present: "[trg_created]",
		},
		{
			name:    "a trigger disabled",
			change:  "DISABLE TRIGGER dbo.trg_disabled ON dbo.t",
			present: "DISABLE TRIGGER [dbo].[trg_disabled]",
		},
		{name: "a column added", change: "ALTER TABLE dbo.t ADD added int NULL", present: "[added] int NULL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			querier := &interferingQuerier{Querier: mssql_queries.New()}
			querier.interfere = func() {
				// The change and the reads of the catalog may wait for each other: the server
				// then ends one of them, and it must be the read, which is done again.
				// It runs on the goroutine of a read: a failure is told, and the test goes on to
				// its own assertions.
				conn, err := db.Conn(t.Context())
				if !assert.NoError(t, err) {
					return
				}
				defer conn.Close()
				_, err = conn.ExecContext(t.Context(), "SET DEADLOCK_PRIORITY HIGH")
				assert.NoError(t, err)
				_, err = conn.ExecContext(t.Context(), tc.change)
				assert.NoError(t, err, tc.change)
			}
			manager := mssql.NewManager(querier, db, func() {}, testutil.GetTestLogger(t))

			blocks, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{table("dbo", "t")})

			require.NoError(t, err)
			require.Equal(t, int32(2), querier.columnReads.Load(), "the catalog is read a second time")
			statements := []string{}
			for _, block := range blocks {
				statements = append(statements, block.Statements...)
			}
			plan := strings.Join(statements, "\n")
			if tc.absent != "" {
				require.NotContains(t, plan, tc.absent)
			}
			if tc.present != "" {
				require.Contains(t, plan, tc.present)
			}
		})
	}

	t.Run("a catalog that does not change is read once", func(t *testing.T) {
		querier := &interferingQuerier{Querier: mssql_queries.New(), interfere: func() {}}
		manager := mssql.NewManager(querier, db, func() {}, testutil.GetTestLogger(t))
		_, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{table("dbo", "t")})
		require.NoError(t, err)
		require.Equal(t, int32(1), querier.columnReads.Load())
	})
}

// testSessionOptions shows that the options a module is created under end with its statement:
// the connection that ran it keeps its own.
func testSessionOptions(t *testing.T, server *testServer) {
	source := server.database(t, "rt_options", "")
	dest := server.database(t, "rt_options_dest", "")
	runScript(t, source, "testdata", "options", "source.sql")

	blocks, err := newManager(t, source).GetSchemaInitStatements(
		t.Context(), []*sqlmanager_shared.SchemaTable{table("dbo", "t")},
	)
	require.NoError(t, err)

	conn, err := dest.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	options := func() (quotedIdentifier, ansiNulls int) {
		err := conn.QueryRowContext(
			t.Context(),
			"SELECT CAST(SESSIONPROPERTY('QUOTED_IDENTIFIER') AS int), CAST(SESSIONPROPERTY('ANSI_NULLS') AS int)",
		).Scan(&quotedIdentifier, &ansiNulls)
		require.NoError(t, err)
		return quotedIdentifier, ansiNulls
	}
	quotedBefore, nullsBefore := options()
	require.Equal(t, 1, quotedBefore)
	require.Equal(t, 1, nullsBefore)

	for _, block := range blocks {
		for _, statement := range block.Statements {
			_, err := conn.ExecContext(t.Context(), statement)
			require.NoError(t, err, statement)
		}
	}

	quotedAfter, nullsAfter := options()
	require.Equal(t, 1, quotedAfter, "QUOTED_IDENTIFIER OFF ended with the statement that set it")
	require.Equal(t, 1, nullsAfter, "ANSI_NULLS OFF ended with the statement that set it")

	// The modules exist with the options of the source.
	created := map[string][2]bool{}
	rows, err := dest.QueryContext(t.Context(), `
SELECT o.name, m.uses_quoted_identifier, m.uses_ansi_nulls
FROM sys.sql_modules m JOIN sys.objects o ON o.object_id = m.object_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var name string
		var quoted, nulls bool
		require.NoError(t, rows.Scan(&name, &quoted, &nulls))
		created[name] = [2]bool{quoted, nulls}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string][2]bool{
		"v_both_off":   {false, false},
		"v_quoted_off": {false, true},
		"v_nulls_off":  {true, false},
		"v_both_on":    {true, true},
		"trg_both_off": {false, false},
	}, created)

	// A module whose text holds another name than the catalog's fails its statement: the view
	// was renamed, and its text still creates it under its first name.
	_, err = source.ExecContext(t.Context(), "EXEC sys.sp_rename N'dbo.v_both_on', N'v_renamed'")
	require.NoError(t, err)
	blocks, err = newManager(t, source).GetSchemaInitStatements(
		t.Context(), []*sqlmanager_shared.SchemaTable{table("dbo", "t")},
	)
	require.NoError(t, err)
	var failure error
	for _, statement := range blocks[3].Statements {
		if strings.Contains(statement, "[v_renamed]") {
			_, failure = dest.ExecContext(t.Context(), "DROP VIEW dbo.v_both_on")
			require.NoError(t, failure)
			_, failure = dest.ExecContext(t.Context(), statement)
		}
	}
	require.ErrorContains(t, failure, "the definition of view [dbo].[v_renamed] did not create it under that name")
}

// testIdentityReset shows that the reset statement makes the next value the seed, whatever the
// table held.
func testIdentityReset(t *testing.T, server *testServer) {
	db := server.database(t, "rt_identity", "")
	exec := func(t *testing.T, statement string) error {
		t.Helper()
		_, err := db.ExecContext(t.Context(), statement)
		return err
	}
	nextValue := func(t *testing.T, tableName string) int {
		t.Helper()
		require.NoError(t, exec(t, "INSERT INTO [it's].["+tableName+"] (v) VALUES (1)"))
		var id int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT MAX(id) FROM [it's].["+tableName+"]").Scan(&id))
		return id
	}
	require.NoError(t, exec(t, "CREATE SCHEMA [it's]"))
	seed, increment := 5, 2

	t.Run("a table that never generated a value starts at its seed", func(t *testing.T) {
		require.NoError(t, exec(t, "CREATE TABLE [it's].[never ]] used] (id int IDENTITY(5,2) NOT NULL, v int NULL)"))
		require.NoError(t, exec(t, mssql.BuildMssqlIdentityColumnResetStatement("it's", "never ] used", &seed, &increment)))
		require.Equal(t, 5, nextValue(t, "never ]] used"))
	})

	t.Run("a table emptied by DELETE starts at its seed again", func(t *testing.T) {
		require.NoError(t, exec(t, "CREATE TABLE [it's].[used] (id int IDENTITY(5,2) NOT NULL, v int NULL)"))
		require.Equal(t, 5, nextValue(t, "used"))
		require.Equal(t, 7, nextValue(t, "used"))
		require.NoError(t, exec(t, mssql.BuildMssqlDeleteStatement("it's", "used")))
		require.NoError(t, exec(t, mssql.BuildMssqlIdentityColumnResetStatement("it's", "used", &seed, &increment)))
		require.Equal(t, 5, nextValue(t, "used"))
	})

	t.Run("the reset to the greatest value of the column", func(t *testing.T) {
		require.NoError(t, exec(t, "CREATE TABLE [it's].[explicit] (id int IDENTITY(5,2) NOT NULL, v int NULL)"))
		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()
		for _, statement := range []string{
			mssql.BuildMssqlSetIdentityInsertStatement("it's", "explicit", true),
			"INSERT INTO [it's].[explicit] (id, v) VALUES (41, 1)",
			mssql.BuildMssqlSetIdentityInsertStatement("it's", "explicit", false),
			mssql.BuildMssqlIdentityColumnResetCurrent("it's", "explicit"),
		} {
			_, err := conn.ExecContext(t.Context(), statement)
			require.NoError(t, err, statement)
		}
		require.Equal(t, 43, nextValue(t, "explicit"))
	})

	t.Run("a seed at the least value of its type cannot be reset below it", func(t *testing.T) {
		require.NoError(t, exec(t, "CREATE TABLE [it's].[tiny] (id tinyint IDENTITY(0,1) NOT NULL, v int NULL)"))
		require.Equal(t, 0, nextValue(t, "tiny"))
		require.NoError(t, exec(t, mssql.BuildMssqlDeleteStatement("it's", "tiny")))
		zero, one := 0, 1
		require.Error(t, exec(t, mssql.BuildMssqlIdentityColumnResetStatement("it's", "tiny", &zero, &one)))
	})

	t.Run("nor can a seed at the least value of bigint", func(t *testing.T) {
		require.NoError(t, exec(t,
			"CREATE TABLE [it's].[big] (id bigint IDENTITY(-9223372036854775808,1) NOT NULL, v int NULL)"))
		require.NoError(t, exec(t, "INSERT INTO [it's].[big] (v) VALUES (1)"))
		require.NoError(t, exec(t, mssql.BuildMssqlDeleteStatement("it's", "big")))
		least, one := math.MinInt64, 1
		require.Error(t, exec(t, mssql.BuildMssqlIdentityColumnResetStatement("it's", "big", &least, &one)))
		// The table still generates values.
		require.NoError(t, exec(t, "INSERT INTO [it's].[big] (v) VALUES (1)"))
	})
}

// testRestrictedLogin shows what a login that may read the rows of a database, and not the
// definitions of its objects, is told: the server answers its catalog queries without the
// definitions and without an error, so the plan is refused by name of the permission.
func testRestrictedLogin(t *testing.T, server *testServer) {
	owner := server.database(t, "rt_restricted", "")
	const password = "Re4der!Passw0rd"
	for _, statement := range []string{
		"CREATE SEQUENCE dbo.numbers AS int START WITH 1",
		"CREATE TABLE dbo.t (id int NOT NULL CONSTRAINT DF_t DEFAULT (NEXT VALUE FOR dbo.numbers), v int NULL, " +
			"CONSTRAINT CK_t CHECK (v > 0))",
		"CREATE VIEW dbo.v AS SELECT id FROM dbo.t",
		"CREATE LOGIN rt_reader WITH PASSWORD = '" + password + "', CHECK_POLICY = OFF",
		"CREATE USER rt_reader FOR LOGIN rt_reader",
		"ALTER ROLE db_datareader ADD MEMBER rt_reader",
	} {
		_, err := owner.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	address, err := url.Parse(server.container.URL)
	require.NoError(t, err)
	address.User = url.UserPassword("rt_reader", password)
	reader := newManager(t, server.open(t, "rt_restricted", address.String()))
	requested := []*sqlmanager_shared.SchemaTable{table("dbo", "t")}

	_, err = reader.GetSchemaInitStatements(t.Context(), requested)
	require.ErrorContains(t, err, "the login lacks the VIEW DEFINITION permission on the database")

	// The lists and the columns are still given.
	tables, err := reader.GetAllTables(t.Context())
	require.NoError(t, err)
	require.Len(t, tables, 1)
	columns, err := reader.GetDatabaseSchema(t.Context())
	require.NoError(t, err)
	require.Len(t, columns, 2)

	// With the permission the login is given the plan the owner is given.
	_, err = owner.ExecContext(t.Context(), "GRANT VIEW DEFINITION TO rt_reader")
	require.NoError(t, err)
	expected, err := newManager(t, owner).GetSchemaInitStatements(t.Context(), requested)
	require.NoError(t, err)
	actual, err := reader.GetSchemaInitStatements(t.Context(), requested)
	require.NoError(t, err)
	// The grant is itself a permission on nothing but the database: the plans tell the same.
	require.Equal(t, expected, actual)
}

// testDestinationTriggers reads the triggers of a database the way a run reads those of its
// destination: nothing is known of it. The read is given whatever the tables hold, whatever
// the compatibility level, and to a login that may only read and write rows.
func testDestinationTriggers(t *testing.T, server *testServer) {
	owner := server.database(t, "rt_triggers", "")
	const password = "Wr1ter!Passw0rd"
	conn, err := owner.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	for _, statement := range []string{
		// A column a plan refuses.
		"SET ANSI_PADDING OFF",
		"CREATE TABLE dbo.legacy (id int NOT NULL, code varchar(10) NULL)",
		"SET ANSI_PADDING ON",
		"CREATE TRIGGER dbo.trg_legacy ON dbo.legacy AFTER INSERT AS RETURN",
		"CREATE TABLE dbo.plain (id int NOT NULL)",
		"CREATE TRIGGER dbo.trg_plain ON dbo.plain AFTER INSERT AS RETURN",
		"DISABLE TRIGGER dbo.trg_plain ON dbo.plain",
		"CREATE LOGIN rt_writer WITH PASSWORD = '" + password + "', CHECK_POLICY = OFF",
		"CREATE USER rt_writer FOR LOGIN rt_writer",
		"ALTER ROLE db_datareader ADD MEMBER rt_writer",
		"ALTER ROLE db_datawriter ADD MEMBER rt_writer",
		"ALTER DATABASE rt_triggers SET COMPATIBILITY_LEVEL = 120",
	} {
		_, err := conn.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	requested := []*sqlmanager_shared.SchemaTable{table("dbo", "legacy"), table("dbo", "plain"), table("dbo", "gone")}

	// A plan of these tables is refused; their triggers are read all the same.
	_, err = newManager(t, owner).GetSchemaInitStatements(t.Context(), requested)
	require.Error(t, err)
	triggers, err := newManager(t, owner).GetSchemaTableTriggers(t.Context(), requested)
	require.NoError(t, err)
	states := map[string]string{}
	for _, trigger := range triggers {
		states[trigger.Table+"/"+trigger.TriggerName] = trigger.EnabledState
	}
	require.Equal(t, map[string]string{"legacy/trg_legacy": "", "plain/trg_plain": "D"}, states)

	// A login that reads and writes rows is not told the text of the triggers: it is given
	// none, and no error.
	address, err := url.Parse(server.container.URL)
	require.NoError(t, err)
	address.User = url.UserPassword("rt_writer", password)
	writer := newManager(t, server.open(t, "rt_triggers", address.String()))
	triggers, err = writer.GetSchemaTableTriggers(t.Context(), requested)
	require.NoError(t, err)
	require.Empty(t, triggers)
}

// testTriggerNameTaken shows that a trigger is guarded by its name and its parent: when another
// table of the destination holds a trigger of that name, the statement runs and fails.
func testTriggerNameTaken(t *testing.T, server *testServer) {
	source := server.database(t, "rt_trigger_name", "")
	dest := server.database(t, "rt_trigger_name_dest", "")
	for _, statement := range []string{
		"CREATE TABLE dbo.t (id int NOT NULL)",
		"CREATE TRIGGER dbo.trg_taken ON dbo.t AFTER INSERT AS RETURN",
	} {
		_, err := source.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	for _, statement := range []string{
		"CREATE TABLE dbo.t (id int NOT NULL)",
		"CREATE TABLE dbo.holder (id int NOT NULL)",
		"CREATE TRIGGER dbo.trg_taken ON dbo.holder AFTER INSERT AS RETURN",
	} {
		_, err := dest.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	blocks, err := newManager(t, source).GetSchemaInitStatements(
		t.Context(), []*sqlmanager_shared.SchemaTable{table("dbo", "t")},
	)
	require.NoError(t, err)
	triggers := blocks[7]
	require.Equal(t, mssql.TableTriggersLabel, triggers.Label)
	require.Len(t, triggers.Statements, 1)

	_, err = dest.ExecContext(t.Context(), triggers.Statements[0])

	require.ErrorContains(t, err, "trg_taken")
}

// testObjectVersions shows what the version check looks at: the tables themselves, and their
// children — constraints and triggers — whose own dates move without the table's.
func testObjectVersions(t *testing.T, server *testServer) {
	db := server.database(t, "rt_versions", "")
	for _, statement := range []string{
		"CREATE TABLE dbo.t (id int NOT NULL CONSTRAINT PK_t PRIMARY KEY, v int NULL CONSTRAINT CK_t CHECK (v > 0))",
		"CREATE TRIGGER dbo.trg_t ON dbo.t AFTER INSERT AS RETURN",
		"CREATE TABLE dbo.other (id int NOT NULL CONSTRAINT PK_other PRIMARY KEY)",
	} {
		_, err := db.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	names := map[int64]string{}
	rows, err := db.QueryContext(t.Context(), "SELECT object_id, name FROM sys.objects WHERE is_ms_shipped = 0")
	require.NoError(t, err)
	defer rows.Close()
	var requestedID int64
	for rows.Next() {
		var id int64
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		names[id] = name
		if name == "t" {
			requestedID = id
		}
	}
	require.NoError(t, rows.Err())

	versions, err := mssql_queries.New().GetObjectVersions(t.Context(), db, []int64{requestedID, requestedID})

	require.NoError(t, err)
	found := []string{}
	for _, version := range versions {
		found = append(found, names[version.ObjectID])
		require.False(t, version.ModifyDate.IsZero())
	}
	require.ElementsMatch(t, []string{"t", "PK_t", "CK_t", "trg_t"}, found, "the table and its children, each once")
}

// testListing shows what the lists of schemas, tables and columns hold.
func testListing(t *testing.T, server *testServer) {
	db := server.database(t, "rt_listing", "")
	for _, statement := range []string{
		"CREATE SCHEMA db_x",
		"CREATE SCHEMA [a.b]",
		"CREATE TABLE db_x.t (id int IDENTITY(-5,10) NOT NULL, name nvarchar(40) NULL, big varchar(max) NULL)",
		"CREATE TABLE [a.b].[c.d] (id int NOT NULL CONSTRAINT [PK c.d] PRIMARY KEY, [x, y] int NOT NULL, " +
			"CONSTRAINT [UQ x, y] UNIQUE ([x, y], id))",
		"CREATE TABLE guest.g (id int NOT NULL)",
		`CREATE TABLE dbo.versioned (
    id int NOT NULL PRIMARY KEY,
    valid_from datetime2 GENERATED ALWAYS AS ROW START NOT NULL,
    valid_to datetime2 GENERATED ALWAYS AS ROW END NOT NULL,
    PERIOD FOR SYSTEM_TIME (valid_from, valid_to)
) WITH (SYSTEM_VERSIONING = ON (HISTORY_TABLE = dbo.versioned_history))`,
	} {
		_, err := db.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	manager := newManager(t, db)

	schemas, err := manager.GetAllSchemas(t.Context())
	require.NoError(t, err)
	names := []string{}
	for _, schema := range schemas {
		names = append(names, schema.SchemaName)
	}
	require.Equal(t, []string{"a.b", "db_x", "dbo"}, names)

	tables, err := manager.GetAllTables(t.Context())
	require.NoError(t, err)
	listed := []string{}
	for _, table := range tables {
		listed = append(listed, table.SchemaName+"/"+table.TableName)
	}
	require.Equal(t, []string{"a.b/c.d", "db_x/t", "dbo/versioned"}, listed, "neither the history table nor guest's")

	columns, err := manager.GetSchemaColumnMap(t.Context())
	require.NoError(t, err)
	require.Len(t, columns, 3)
	identity := columns["db_x.t"]["id"]
	require.Equal(t, "IDENTITY(-5,10)", *identity.IdentityGeneration)
	require.Equal(t, -5, *identity.IdentitySeed)
	require.Equal(t, 10, *identity.IdentityIncrement)
	require.False(t, identity.UpdateAllowed)
	require.Equal(t, 40, columns["db_x.t"]["name"].CharacterMaximumLength)
	require.Equal(t, -1, columns["db_x.t"]["big"].CharacterMaximumLength)
	require.Equal(t, "GENERATED ALWAYS AS ROW START", *columns["dbo.versioned"]["valid_from"].GeneratedType)

	byTable, err := manager.GetDatabaseTableSchemasBySchemasAndTables(
		t.Context(), []*sqlmanager_shared.SchemaTable{table("A.B", "C.D"), table("dbo", "gone")},
	)
	require.NoError(t, err)
	require.Len(t, byTable, 2)
	require.Equal(t, "c.d", byTable[0].TableName)

	constraints, err := manager.GetTableConstraintsBySchema(t.Context(), []string{"a.b", "it's not there"})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"a.b.c.d": {"id"}}, constraints.PrimaryKeyConstraints)
	require.Equal(t, map[string][][]string{"a.b.c.d": {{"x, y", "id"}}}, constraints.UniqueConstraints)

	count, err := manager.GetTableRowCount(t.Context(), "a.b", "c.d", nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)

	var compatibility sql.NullInt32
	require.NoError(t, db.QueryRowContext(
		t.Context(), "SELECT compatibility_level FROM sys.databases WHERE database_id = DB_ID()",
	).Scan(&compatibility))
	_, err = db.ExecContext(t.Context(), "ALTER DATABASE rt_listing SET COMPATIBILITY_LEVEL = 120")
	require.NoError(t, err)
	_, err = manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{table("db_x", "t")})
	require.ErrorContains(t, err, "compatibility level 120: 130 or more is required")
}
