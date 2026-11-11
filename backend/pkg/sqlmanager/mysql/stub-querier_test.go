package sqlmanager_mysql

import (
	"context"
	"sync/atomic"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	"github.com/stretchr/testify/require"
)

// stubQuerier answers the three catalog reads of GetTableInitStatements with fixed rows, for the
// tests that also give the manager a sqlmock pool. A testify mock formats the arguments it is
// called with, the pool among them, by reflection: it would read the fields of the *sql.DB while
// the other reads of GetTableInitStatements use it from their own goroutines (a data race under
// -race). The stub never looks at its arguments. Any other method of the interface panics.
type stubQuerier struct {
	mysql_queries.Querier

	columns     []*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow
	constraints []*mysql_queries.GetTableConstraintsRow
	indices     []*mysql_queries.GetIndicesBySchemasAndTablesRow

	columnsCalls, constraintsCalls, indicesCalls atomic.Int32
}

// newStubQuerier gives a stub that answers the columns given, no constraint and no index. Each
// read it answers is required to have been called once the test ends, as the expectations of a
// mock are; withoutCatalogKeys leaves the constraints and indices out of that requirement, for a
// querier that reads them itself.
func newStubQuerier(
	t *testing.T,
	columns []*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow,
	withoutCatalogKeys bool,
) *stubQuerier {
	t.Helper()
	stub := &stubQuerier{
		columns:     columns,
		constraints: []*mysql_queries.GetTableConstraintsRow{},
		indices:     []*mysql_queries.GetIndicesBySchemasAndTablesRow{},
	}
	t.Cleanup(func() {
		require.Positive(t, stub.columnsCalls.Load(), "GetDatabaseTableSchemasBySchemasAndTables not called")
		if !withoutCatalogKeys {
			require.Positive(t, stub.constraintsCalls.Load(), "GetTableConstraints not called")
			require.Positive(t, stub.indicesCalls.Load(), "GetIndicesBySchemasAndTables not called")
		}
	})
	return stub
}

func (s *stubQuerier) GetDatabaseTableSchemasBySchemasAndTables(
	_ context.Context, _ mysql_queries.DBTX, _ *mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesParams,
) ([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow, error) {
	s.columnsCalls.Add(1)
	return s.columns, nil
}

func (s *stubQuerier) GetTableConstraints(
	_ context.Context, _ mysql_queries.DBTX, _ *mysql_queries.GetTableConstraintsParams,
) ([]*mysql_queries.GetTableConstraintsRow, error) {
	s.constraintsCalls.Add(1)
	return s.constraints, nil
}

func (s *stubQuerier) GetIndicesBySchemasAndTables(
	_ context.Context, _ mysql_queries.DBTX, _ *mysql_queries.GetIndicesBySchemasAndTablesParams,
) ([]*mysql_queries.GetIndicesBySchemasAndTablesRow, error) {
	s.indicesCalls.Add(1)
	return s.indices, nil
}
