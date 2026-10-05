package sqlmanager

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	schemamanager "github.com/fishtre-compagnie/husonym/internal/schema-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// oddDestinationOptions gives the options of a destination of the engine of the fixture.
func (f *sampleFixture) oddDestinationOptions(initSchema, truncate, cascade bool) *mgmtv1alpha1.JobDestination {
	options := &mgmtv1alpha1.JobDestinationOptions{}
	switch f.engine.family {
	case familyMysql:
		options.Config = &mgmtv1alpha1.JobDestinationOptions_MysqlOptions{
			MysqlOptions: &mgmtv1alpha1.MysqlDestinationConnectionOptions{
				InitTableSchema: initSchema,
				TruncateTable:   &mgmtv1alpha1.MysqlTruncateTableConfig{TruncateBeforeInsert: truncate},
			},
		}
	case familyMssql:
		options.Config = &mgmtv1alpha1.JobDestinationOptions_MssqlOptions{
			MssqlOptions: &mgmtv1alpha1.MssqlDestinationConnectionOptions{
				InitTableSchema: initSchema,
				TruncateTable:   &mgmtv1alpha1.MssqlTruncateTableConfig{TruncateBeforeInsert: truncate},
			},
		}
	default:
		options.Config = &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
			PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{
				InitTableSchema: initSchema,
				TruncateTable: &mgmtv1alpha1.PostgresTruncateTableConfig{
					TruncateBeforeInsert: truncate,
					Cascade:              cascade,
				},
			},
		}
	}
	return &mgmtv1alpha1.JobDestination{Options: options}
}

// withSchemaManager runs fn with the schema manager a sync run builds for a source, a
// destination and its options, and closes its connections.
func (f *sampleFixture) withSchemaManager(
	t *testing.T,
	source, destination *oddDatabase,
	options *mgmtv1alpha1.JobDestination,
	fn func(manager schemamanager.SchemaManagerService),
) {
	t.Helper()
	manager, err := schemamanager.NewSchemaManager(
		f.sqlmanager,
		connectionmanager.NewUniqueSession(),
		testutil.GetTestLogger(t),
		testutil.NewFakeEELicense(testutil.WithIsValid()),
	).New(context.Background(), source.connection, destination.connection, options)
	require.NoError(t, err)
	defer manager.CloseConnections()
	fn(manager)
}

// The schema initialization of a sync run creates in an empty destination the objects of the
// source under their own names, whatever characters the names hold, and creates nothing more when
// it runs again. The destination is then read from its catalog and compared with the source,
// object by object.
func Test_OddNames_SchemaInit(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		forEachOddRotation(t, func(t *testing.T, units []oddUnit) {
			destination := f.oddDestination(t)
			atFirst := destination.objects(t)

			for run := 1; run <= 2; run++ {
				f.withSchemaManager(t, source, destination, f.oddDestinationOptions(true, false, false),
					func(manager schemamanager.SchemaManagerService) {
						failed, err := manager.InitializeSchema(context.Background(), tableKeys(units))
						require.NoError(t, err, "run %d", run)
						for _, failure := range failed {
							// SQL Server creates a sequence at its declared start and says so
							// when the source has moved on: that is not a failure.
							if strings.HasPrefix(failure.Error, "skipped: created at its declared start") {
								continue
							}
							require.Fail(t, "a statement of the initialization failed",
								"run %d: %s: %s", run, failure.Statement, failure.Error)
						}
					})
			}

			want := source.describe(t, schemasOf(units))
			require.NotEmpty(t, want)
			require.Equal(t, want, destination.describe(t, schemasOf(units)),
				"the destination does not hold the objects of the source")
			destination.requireObjects(t, atFirst, units)
			for _, u := range units {
				require.Zero(t, destination.rowCount(t, u.schema, u.parent))
				require.Zero(t, destination.rowCount(t, u.schema, u.child))
			}
			destination.requireCanaryIntact(t)
			source.requireCanaryIntact(t)
		})
	})
}

// A CHECK constraint over a column whose name holds a backslash or a line break is created in the
// destination as the source has it. MySQL and MariaDB give the expression of a constraint in
// their catalog, where MySQL writes it with escapes.
func Test_OddNames_SchemaInit_CheckOverAColumn(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		const database, table = "odd_checks", "checked"
		for label, column := range map[string]string{"backslash": `back\slash`, "line break": "new\nline"} {
			t.Run(label, func(t *testing.T) {
				source := f.oddSource(t)
				destination := f.oddDestination(t)
				for _, d := range []*oddDatabase{source, destination} {
					t.Cleanup(func() {
						_, err := d.db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+d.f.quote(database))
						require.NoError(t, err)
					})
				}
				source.exec(t, "CREATE DATABASE "+f.quote(database))
				source.exec(t, fmt.Sprintf(
					"CREATE TABLE %s (id INT NOT NULL PRIMARY KEY, %s INT NOT NULL, CONSTRAINT positive CHECK (%s > 0))",
					source.table(database, table), f.quote(column), f.quote(column)))

				f.withSchemaManager(t, source, destination, f.oddDestinationOptions(true, false, false),
					func(manager schemamanager.SchemaManagerService) {
						failed, err := manager.InitializeSchema(context.Background(),
							map[string]struct{}{database + "." + table: {}})
						require.NoError(t, err)
						require.Empty(t, failed)
					})

				want := source.describe(t, []string{database})
				require.NotEmpty(t, want)
				require.Equal(t, want, destination.describe(t, []string{database}))
				destination.requireCanaryIntact(t)
			})
		}
	})
}

// generatedValues inserts one row that leaves the identity column and the sequence-fed column to
// the server, and gives the values they took: a parent row, and on MySQL and MariaDB a child row
// of it, which holds the identity column there.
func (d *oddDatabase) generatedValues(t *testing.T, u oddUnit, pk int) (counter, ticket int) {
	t.Helper()
	q := d.f.quote
	d.exec(t, fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES (%d, %s)",
		d.table(u.schema, u.parent), q(u.pk), q(u.other), pk, quoteText(oddLabel(pk))))
	if !d.parentHasCounter() {
		d.exec(t, fmt.Sprintf("INSERT INTO %s (%s, note) VALUES (%d, 'again')",
			d.table(u.schema, u.child), q(u.fk), pk))
		require.NoError(t, d.db.QueryRowContext(context.Background(), fmt.Sprintf(
			"SELECT %s FROM %s WHERE %s = %d", q(u.counter), d.table(u.schema, u.child), q(u.fk), pk)).
			Scan(&counter))
		return counter, 0
	}
	require.NoError(t, d.db.QueryRowContext(context.Background(), fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = %d",
		q(u.counter), q(u.ticket()), d.table(u.schema, u.parent), q(u.pk), pk)).
		Scan(&counter, &ticket))
	return counter, ticket
}

// The truncation of a sync run empties the tables of the job whatever their names hold, and
// leaves the rest of the destination as it was. Identity columns start again at their first
// value on every engine; on PostgreSQL the sequence of a column starts again as well, and on SQL
// Server it goes on from where it was.
func Test_OddNames_Truncate(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		// PostgreSQL truncates the tables in the order of their foreign keys, or each one with
		// those that refer to it.
		modes := map[string]bool{"in order": false}
		if f.engine.family == familyPostgres {
			modes["cascade"] = true
		}
		forEachOddRotation(t, func(t *testing.T, units []oddUnit) {
			for mode, cascade := range modes {
				t.Run(mode, func(t *testing.T) {
					destination := f.oddDestination(t)
					atFirst := destination.objects(t)
					destination.create(t, units)
					for _, u := range units {
						destination.load(t, u, oddSmallParentRows)
					}

					f.withSchemaManager(t, source, destination, f.oddDestinationOptions(false, !cascade, cascade),
						func(manager schemamanager.SchemaManagerService) {
							require.NoError(t, manager.TruncateData(context.Background(), tableKeys(units), schemasOf(units)))
						})

					for _, u := range units {
						require.Zero(t, destination.rowCount(t, u.schema, u.parent), "%q.%q", u.schema, u.parent)
						require.Zero(t, destination.rowCount(t, u.schema, u.child), "%q.%q", u.schema, u.child)
					}
					destination.requireObjects(t, atFirst, units)
					destination.requireCanaryIntact(t)
					source.requireCanaryIntact(t)

					for _, u := range units {
						counter, ticket := destination.generatedValues(t, u, 1)
						require.Equal(t, 1, counter, "the identity column %q of %q.%q", u.counter, u.schema, u.parent)
						switch f.engine.family {
						case familyPostgres:
							require.Equal(t, 1, ticket, "the sequence %q.%q", u.schema, u.counter)
						case familyMssql:
							require.Equal(t, oddSmallParentRows+1, ticket, "the sequence %q.%q", u.schema, u.counter)
						}
					}
				})
			}
		})
	})
}

// connectionData gives the service that samples, streams and counts the rows of the database.
func (d *oddDatabase) connectionData(t *testing.T) *connectiondata.SQLConnectionDataService {
	t.Helper()
	return connectiondata.NewSQLConnectionDataService(
		testutil.GetTestLogger(t),
		&sqlconnect.SqlOpenConnector{},
		d.f.sqlmanager,
		d.connection,
	)
}

// requireRowsOfTheSource fails unless every given row of a parent table holds the values the
// source holds for its key, read here by a query of this test.
func (d *oddDatabase) requireRowsOfTheSource(t *testing.T, u oddUnit, rows []map[string]any) {
	t.Helper()
	q := d.f.quote
	for _, row := range rows {
		pk := intColumn(t, row, u.pk)
		var other string
		require.NoError(t, d.db.QueryRowContext(context.Background(), fmt.Sprintf(
			"SELECT %s FROM %s WHERE %s = %d",
			q(u.other), d.table(u.schema, u.parent), q(u.pk), pk)).Scan(&other))
		require.Equal(t, oddLabel(int(pk)), other)
		require.Equal(t, other, textColumn(t, row, u.other), "column %q of row %d of %q.%q", u.other, pk, u.schema, u.parent)
		if !d.parentHasCounter() {
			continue
		}
		var counter int64
		require.NoError(t, d.db.QueryRowContext(context.Background(), fmt.Sprintf(
			"SELECT %s FROM %s WHERE %s = %d",
			q(u.counter), d.table(u.schema, u.parent), q(u.pk), pk)).Scan(&counter))
		require.Equal(t, counter, intColumn(t, row, u.counter), "column %q of row %d of %q.%q", u.counter, pk, u.schema, u.parent)
	}
}

// textColumn reads a text column of a row the service sent.
func textColumn(t *testing.T, row map[string]any, column string) string {
	t.Helper()
	switch value := row[column].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		require.FailNow(t, "unexpected type for column "+column, "%T", value)
		return ""
	}
}

// requireSourceUnchanged runs fn and fails unless the source holds the same objects after it as
// before, and its canary.
func (d *oddDatabase) requireSourceUnchanged(t *testing.T, fn func()) {
	t.Helper()
	before := d.objects(t)
	fn()
	require.Equal(t, before, d.objects(t), "the schemas, tables, sequences, triggers and routines of the source")
	d.requireCanaryIntact(t)
}

// A sample of a table whose schema, name and columns hold quote characters returns rows of that
// table, with the values the source holds.
func Test_OddNames_Sample(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		forEachOddRotation(t, func(t *testing.T, units []oddUnit) {
			source.requireSourceUnchanged(t, func() {
				for _, u := range units {
					stream := &collectingStream{}
					err := source.connectionData(t).SampleData(context.Background(), stream, u.schema, u.parent, 20)
					require.NoError(t, err, "%q.%q", u.schema, u.parent)
					rows := stream.rows(t)
					require.Len(t, rows, 20, "%q.%q", u.schema, u.parent)
					requireDistinct(t, rows, u.pk)
					source.requireRowsOfTheSource(t, u, rows)
				}
			})
		})
	})
}

// streamRows reads a table through StreamData and gives its rows as they were sent. The service
// writes to the stream of a server call, so the call is made: a handler that streams the table is
// served for the length of it.
func (d *oddDatabase) streamRows(t *testing.T, schema, table string) [][]byte {
	t.Helper()
	const procedure = "/oddnames.Tables/Stream"
	service := d.connectionData(t)
	mux := http.NewServeMux()
	mux.Handle(procedure, connect.NewServerStreamHandler(procedure, func(
		ctx context.Context,
		_ *connect.Request[mgmtv1alpha1.GetConnectionDataStreamRequest],
		stream *connect.ServerStream[mgmtv1alpha1.GetConnectionDataStreamResponse],
	) error {
		return service.StreamData(ctx, stream, nil, schema, table)
	}))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := connect.NewClient[mgmtv1alpha1.GetConnectionDataStreamRequest, mgmtv1alpha1.GetConnectionDataStreamResponse](
		server.Client(), server.URL+procedure)
	stream, err := client.CallServerStream(context.Background(),
		connect.NewRequest(&mgmtv1alpha1.GetConnectionDataStreamRequest{}))
	require.NoError(t, err)
	defer stream.Close()
	var payloads [][]byte
	for stream.Receive() {
		payloads = append(payloads, stream.Msg().GetRowBytes())
	}
	require.NoError(t, stream.Err(), "%q.%q", schema, table)
	return payloads
}

// The stream of a table whose schema, name and columns hold quote characters returns every row
// of that table, with the values the source holds.
func Test_OddNames_Stream(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		forEachOddRotation(t, func(t *testing.T, units []oddUnit) {
			source.requireSourceUnchanged(t, func() {
				for _, u := range units {
					payloads := source.streamRows(t, u.schema, u.parent)
					require.Len(t, payloads, u.sourceParentRows(), "%q.%q", u.schema, u.parent)
					wanted := []int64{1, 2, int64(u.sourceParentRows() / 2), int64(u.sourceParentRows())}
					var rows, checked []map[string]any
					for _, payload := range payloads {
						var row map[string]any
						require.NoError(t, gob.NewDecoder(bytes.NewReader(payload)).Decode(&row))
						rows = append(rows, row)
						if contains64(wanted, intColumn(t, row, u.pk)) {
							checked = append(checked, row)
						}
					}
					requireDistinct(t, rows, u.pk)
					require.Len(t, checked, len(wanted))
					source.requireRowsOfTheSource(t, u, checked)
				}
			})
		})
	})
}

func contains64(list []int64, n int64) bool {
	for _, item := range list {
		if item == n {
			return true
		}
	}
	return false
}

// oddViewName names the view a test defines over the parent table of a unit.
func oddViewName(u oddUnit) string { return u.parent + " view" }

// The row count of a table whose schema and name hold quote characters is the count of that
// table, alone or under a condition that names a column the same way; a table that does not exist
// is an error.
func Test_OddNames_RowCount(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		q := f.quote
		forEachOddRotation(t, func(t *testing.T, units []oddUnit) {
			ctx := context.Background()
			t.Run("table", func(t *testing.T) {
				source.requireSourceUnchanged(t, func() {
					for _, u := range units {
						service := source.connectionData(t)
						count, err := service.GetTableRowCount(ctx, u.schema, u.parent, nil)
						require.NoError(t, err, "%q.%q", u.schema, u.parent)
						require.EqualValues(t, u.sourceParentRows(), count, "%q.%q", u.schema, u.parent)

						some := u.sourceParentRows() - 66
						below := fmt.Sprintf("%s <= %d", q(u.pk), some)
						count, err = service.GetTableRowCount(ctx, u.schema, u.parent, &below)
						require.NoError(t, err, "%q.%q where %s", u.schema, u.parent, below)
						require.EqualValues(t, some, count, "%q.%q where %s", u.schema, u.parent, below)

						one := fmt.Sprintf("%s = %s", q(u.other), quoteText(oddLabel(7)))
						count, err = service.GetTableRowCount(ctx, u.schema, u.parent, &one)
						require.NoError(t, err, "%q.%q where %s", u.schema, u.parent, one)
						require.EqualValues(t, 1, count, "%q.%q where %s", u.schema, u.parent, one)

						_, err = service.GetTableRowCount(ctx, u.schema, u.parent+" absent", nil)
						require.Error(t, err, "a table that does not exist was counted")
					}
				})
			})

			// A view over the table, whose own name holds the same characters, is counted like
			// the table.
			t.Run("view", func(t *testing.T) {
				if f.engine.family != familyMysql {
					t.Skip("the row count of a view is refused until the catalog check is removed")
				}
				for _, u := range units {
					view := source.table(u.schema, oddViewName(u))
					source.exec(t, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s", view, source.table(u.schema, u.parent)))
					t.Cleanup(func() {
						_, err := source.db.ExecContext(context.Background(), "DROP VIEW "+view)
						require.NoError(t, err)
					})
					count, err := source.connectionData(t).GetTableRowCount(ctx, u.schema, oddViewName(u), nil)
					require.NoError(t, err, "%q.%q", u.schema, oddViewName(u))
					require.EqualValues(t, u.sourceParentRows(), count, "%q.%q", u.schema, oddViewName(u))
				}
				source.requireCanaryIntact(t)
			})
		})
	})
}
