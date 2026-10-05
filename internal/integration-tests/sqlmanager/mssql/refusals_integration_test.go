package sqlmanager_mssql

import (
	"database/sql"
	"testing"

	mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// casesDatabase holds the objects of testdata/refusals/source.sql. It has a filegroup for
// memory-optimized tables.
func casesDatabase(t *testing.T, server *testServer, name string) *sql.DB {
	t.Helper()
	db := server.database(t, name, "")
	for _, statement := range []string{
		"ALTER DATABASE [" + name + "] ADD FILEGROUP in_memory CONTAINS MEMORY_OPTIMIZED_DATA",
		"ALTER DATABASE [" + name + "] ADD FILE (NAME = '" + name + "_in_memory', " +
			"FILENAME = '/var/opt/mssql/data/" + name + "_in_memory') TO FILEGROUP in_memory",
		"ALTER DATABASE [" + name + "] ADD FILEGROUP archive",
		"ALTER DATABASE [" + name + "] ADD FILE (NAME = '" + name + "_archive', " +
			"FILENAME = '/var/opt/mssql/data/" + name + "_archive.ndf') TO FILEGROUP archive",
		"ALTER DATABASE [" + name + "] SET CHANGE_TRACKING = ON",
	} {
		_, err := db.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	runScript(t, db, "testdata", "refusals", "source.sql")
	return db
}

func table(schema, name string) *sqlmanager_shared.SchemaTable {
	return &sqlmanager_shared.SchemaTable{Schema: schema, Table: name}
}

// testRefusals shows that the read of the catalog sets what each refusal is decided on. The
// rules themselves are tested on snapshots, in the ddl package.
func testRefusals(t *testing.T, server *testServer) {
	manager := newManager(t, casesDatabase(t, server, "rt_refusals"))

	cases := []struct {
		table    string
		expected []ddl.Refusal
	}{
		{"in_memory", []ddl.Refusal{{Object: "[refused].[in_memory]", Reason: "memory-optimized table"}}},
		{"graph_node", []ddl.Refusal{{Object: "[refused].[graph_node]", Reason: "graph node table"}}},
		{"graph_edge", []ddl.Refusal{{Object: "[refused].[graph_edge]", Reason: "graph edge table"}}},
		{"ledger", []ddl.Refusal{{Object: "[refused].[ledger]", Reason: "ledger table"}}},
		{"typed_xml", []ddl.Refusal{{
			Object: "[refused].[typed_xml].[doc]", Reason: "xml column bound to an XML schema collection",
		}}},
		{"not_padded", []ddl.Refusal{
			{Object: "[refused].[not_padded].[code]", Reason: "created under ANSI_PADDING OFF"},
			{Object: "[refused].[not_padded].[fixed]", Reason: "created under ANSI_PADDING OFF"},
		}},
		{"bound", []ddl.Refusal{
			{Object: "[refused].[bound].[ruled]", Reason: "a rule is bound to the column"},
			{Object: "[refused].[bound].[defaulted]", Reason: "a stand-alone default is bound to the column"},
		}},
		{"bound_type", []ddl.Refusal{{
			Object: "[refused].[bound_type].[v]", Reason: "alias type [refused].[ruled_type] has a bound rule",
		}}},
		{"disabled_clustered", []ddl.Refusal{{
			Object: "[refused].[disabled_clustered].[CIX_disabled]", Reason: "disabled clustered index",
		}}},
		{"disabled_key", []ddl.Refusal{{
			Object: "[refused].[disabled_key].[UQ_disabled]", Reason: "disabled index backing a key",
		}}},
		{"calls_encrypted", []ddl.Refusal{{
			Object: "[helpers].[secret]",
			Reason: "needed by table [refused].[calls_encrypted]: encrypted, its definition cannot be read",
		}}},
		{"calls_bound", []ddl.Refusal{{
			Object: "[helpers].[bound_rate]",
			Reason: "needed by table [refused].[calls_bound]: schema-bound to table [helpers].[rates]",
		}}},
		{"calls_inline", []ddl.Refusal{{
			Object: "[helpers].[inline_rates]",
			Reason: "needed by table [refused].[calls_inline]: " +
				"inline function reads table [helpers].[rates], which is created after it",
		}}},
		{"calls_reader", []ddl.Refusal{{
			Object: "[helpers].[reads_rates]",
			Reason: "needed by table [refused].[calls_reader]: " +
				"depends on table [helpers].[rates], which is outside the selection",
		}}},
		{"calls_alias", []ddl.Refusal{{
			Object: "[helpers].[takes_alias]",
			Reason: "needed by table [refused].[calls_alias]: " +
				"depends on alias type [helpers].[only_in_module], which no column of the selection uses",
		}}},
	}
	all := []*sqlmanager_shared.SchemaTable{}
	refusals := 0
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			_, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{table("refused", tc.table)})
			var refusal *ddl.RefusalError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, tc.expected, refusal.Refusals)
		})
		all = append(all, table("refused", tc.table))
		refusals += len(tc.expected)
	}

	t.Run("every refused object is named at once", func(t *testing.T) {
		_, err := manager.GetSchemaInitStatements(t.Context(), all)
		var refusal *ddl.RefusalError
		require.ErrorAs(t, err, &refusal)
		require.Len(t, refusal.Refusals, refusals)
		require.ErrorContains(t, err, "[refused].[in_memory]: memory-optimized table\n")
	})

	t.Run("a function that reads a table requested with the one that calls it is created", func(t *testing.T) {
		blocks, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{
			table("refused", "calls_reader"), table("helpers", "rates"),
		})
		require.NoError(t, err)
		dest := server.database(t, "rt_refusals_dest", "")
		apply(t, dest, blocks)
		_, err = dest.ExecContext(t.Context(), "INSERT INTO helpers.rates (id, rate) VALUES (1, 5)")
		require.NoError(t, err)
		_, err = dest.ExecContext(t.Context(), "INSERT INTO refused.calls_reader (id) VALUES (1)")
		require.NoError(t, err, "the check runs its function, which finds its table")
	})

	t.Run("the other operations of a plan are refused as well", func(t *testing.T) {
		refused := []*sqlmanager_shared.SchemaTable{table("refused", "graph_node")}
		var refusal *ddl.RefusalError
		_, err := manager.GetTableInitStatements(t.Context(), refused)
		require.ErrorAs(t, err, &refusal)
		_, err = manager.GetSchemaTableDataTypes(t.Context(), refused)
		require.ErrorAs(t, err, &refusal)
		_, err = manager.GetSequencesByTables(t.Context(), "refused", []string{"graph_node"})
		require.ErrorAs(t, err, &refusal)
		// The triggers of a table are read whatever the table is.
		_, err = manager.GetSchemaTableTriggers(t.Context(), refused)
		require.NoError(t, err)
	})
}

// testReported shows what the plan tells of what it does not reproduce beside storage and
// administration: options that are not at their default, a table created under ANSI_NULLS OFF,
// the index of a view, a requested name that is a view, a module that needs an alias type no
// column uses. A sequence only a procedure draws from is created.
func testReported(t *testing.T, server *testServer) {
	source := casesDatabase(t, server, "rt_reported")
	dest := server.database(t, "rt_reported_dest", "")
	manager := newManager(t, source)

	blocks, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{
		table("reported", "options"), table("reported", "nulls_off"), table("reported", "v_indexed"),
	})
	require.NoError(t, err)

	skipped := skippedOf(blocks)
	require.ElementsMatch(t, []string{
		"[reported].[v_indexed]: a view, not a table: only tables are requested",
		"[reported].[nulls_off]: created under ANSI_NULLS OFF at the source: it is created under ANSI_NULLS ON",
		"[reported].[options]: XML compression not reproduced",
		"[reported].[options]: extended properties not reproduced: 3",
		"[reported].[options]: OPTIMIZE_FOR_SEQUENTIAL_KEY not reproduced: 1",
		"[reported].[options]: STATISTICS_NORECOMPUTE not reproduced: 1",
		"[reported].[options]: LOCK_ESCALATION not reproduced: DISABLE",
		"[reported].[options]: text in row not reproduced: 256",
		"[reported].[options]: large value types out of row not reproduced",
	}, skipped[sqlmanager_shared.CreateTablesLabel])
	require.ElementsMatch(t, []string{
		"[reported].[v_indexed]: the indexes of a view are not reproduced",
	}, skipped[mssql.TableIndexLabel])
	require.ElementsMatch(t, []string{
		"[reported].[p_alias]: depends on alias type [reported].[only_in_module], which no column of the selection uses",
	}, skipped[mssql.ViewsFunctionsLabel])
	require.Empty(t, skipped[mssql.DataTypesLabel])

	apply(t, dest, blocks)
	var sequences, procedures int
	require.NoError(t, dest.QueryRowContext(t.Context(), `
SELECT (SELECT COUNT(*) FROM sys.sequences WHERE name = 'of_a_procedure'),
       (SELECT COUNT(*) FROM sys.procedures WHERE name IN ('p_next', 'p_alias'))`).Scan(&sequences, &procedures))
	require.Equal(t, 1, sequences, "the sequence the procedure draws from is created")
	require.Equal(t, 1, procedures, "the procedure that draws from it is created, the other is not")
	_, err = dest.ExecContext(t.Context(), "EXEC reported.p_next")
	require.NoError(t, err)
}

// testSkips shows that the read of the catalog sets what each skip is decided on, and that the
// plan runs without what it leaves out.
func testSkips(t *testing.T, server *testServer) {
	source := casesDatabase(t, server, "rt_skips")
	dest := server.database(t, "rt_skips_dest", "")
	manager := newManager(t, source)

	blocks, err := manager.GetSchemaInitStatements(t.Context(), []*sqlmanager_shared.SchemaTable{
		table("skipped", "child"), table("skipped", "Gone"), table("Skipped", "CHILD"), table("skipped", "stored"),
	})
	require.NoError(t, err)

	skipped := skippedOf(blocks)
	require.ElementsMatch(t, []string{
		"[skipped].[Gone]: not found in the source database",
		"[skipped].[child]: statistics not reproduced: 1",
		"[skipped].[stored]: filegroup not reproduced: archive",
		"[skipped].[stored]: row-level security not reproduced: 1",
		"[skipped].[child]: change tracking not reproduced",
	}, skipped[sqlmanager_shared.CreateTablesLabel])
	require.ElementsMatch(t, []string{
		"[skipped].[numbers]: created at its declared start 100; the source is at 101",
	}, skipped[mssql.DataTypesLabel])
	require.ElementsMatch(t, []string{
		"[skipped].[v_secret]: encrypted: its definition cannot be read",
		"[skipped].[p_secret]: encrypted: its definition cannot be read",
		"[skipped].[v_outside]: depends on table [outside].[parent], which is outside the selection",
		"[skipped].[v_on_outside]: depends on [skipped].[v_outside], which is not reproduced",
		"[skipped].[p_synonym]: depends on synonym [skipped].[parent_synonym]",
		"[skipped].[p_table_type]: depends on table type [skipped].[lines]",
	}, skipped[mssql.ViewsFunctionsLabel])
	require.ElementsMatch(t, []string{
		"[skipped].[child].[XI_child_doc]: XML indexes are not reproduced",
		"[skipped].[child].[SI_child_place]: spatial indexes are not reproduced",
		"[skipped].[stored]: columnstore order not reproduced: 1",
	}, skipped[mssql.TableIndexLabel])
	require.ElementsMatch(t, []string{
		"[skipped].[child].[FK_child_outside]: foreign key to [outside].[parent], which is not among the requested tables",
	}, skipped[mssql.FkAlterTableLabel])
	require.ElementsMatch(t, []string{
		"[skipped].[trg_secret]: encrypted: its definition cannot be read",
		"[skipped].[child]: trigger order not reproduced: 1",
	}, skipped[mssql.TableTriggersLabel])

	// What is left runs on an empty destination: the table with its key, its sequence, the view
	// and the trigger that can be read.
	apply(t, dest, blocks)
	var created []string
	rows, err := dest.QueryContext(t.Context(), `
SELECT RTRIM(o.type), o.name FROM sys.objects o JOIN sys.schemas s ON s.schema_id = o.schema_id
WHERE s.name = 'skipped' ORDER BY 1, 2`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var kind, name string
		require.NoError(t, rows.Scan(&kind, &name))
		created = append(created, kind+" "+name)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"D DF_child_id", "PK PK_child", "PK PK_stored", "SO numbers", "TR trg_first",
		"U child", "U stored", "V v_kept",
	}, created)
}
