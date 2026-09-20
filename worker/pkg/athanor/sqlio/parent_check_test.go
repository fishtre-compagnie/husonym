package sqlio

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type recordingWriter struct {
	rows [][]any
}

func (w *recordingWriter) WriteBatch(_ []string, rows [][]any) error {
	w.rows = append(w.rows, rows...)
	return nil
}

func factureCheck(noParent *string) []ParentCheck {
	return []ParentCheck{{
		Columns: []string{"commande_id"}, ParentSchema: "shop", ParentTable: "COMMANDE",
		ParentColumns: []string{"id"}, NoParentValue: noParent,
	}}
}

const factureLookup = "SELECT v.n FROM (SELECT 0 AS n, p.`id` AS k0 FROM `shop`.`COMMANDE` p WHERE 1 = 0" +
	" UNION ALL SELECT ?, ? UNION ALL SELECT ?, ?) v " +
	"WHERE EXISTS (SELECT 1 FROM `shop`.`COMMANDE` p WHERE p.`id` = v.k0)"

func TestParentCheckWriter_DiscardsRowsWithoutParent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	// Two distinct keys for three rows: 7 exists, 1007 was left out of the subset.
	mock.ExpectQuery(regexp.QuoteMeta(factureLookup)).
		WithArgs(0, int64(7), 1, int64(1007)).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	inner, discarded := &recordingWriter{}, 0
	w := NewParentCheckWriter(context.Background(), tx, MySQLDialect{}, inner, "shop.FACTURE",
		factureCheck(nil), func(dropped []int) { discarded += len(dropped) })

	require.NoError(t, w.WriteBatch([]string{"id", "commande_id"},
		[][]any{{int64(1), int64(7)}, {int64(2), int64(1007)}, {int64(3), int64(7)}}))
	require.Equal(t, [][]any{{int64(1), int64(7)}, {int64(3), int64(7)}}, inner.rows)
	require.Equal(t, 1, discarded)
	require.NoError(t, mock.ExpectationsWereMet())
}

// A mandatory key to a parent the destination does not hold leaves the row out whatever
// skip_foreign_key_violations says: the row cannot be written at all, so failing the page
// would turn a row that has to go into a run that does not finish. A live source makes
// this ordinary — a parent and its child created between the read of the two tables.
func TestParentCheckWriter_DiscardsEvenWhenViolationsAreNotSkipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT v.n FROM").WillReturnRows(sqlmock.NewRows([]string{"n"}))

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	inner, discarded := &recordingWriter{}, 0
	w := NewParentCheckWriter(context.Background(), tx, MySQLDialect{}, inner, "shop.FACTURE",
		factureCheck(nil), func(dropped []int) { discarded += len(dropped) })

	require.NoError(t, w.WriteBatch([]string{"id", "commande_id"}, [][]any{{int64(1), int64(1007)}}))
	require.Empty(t, inner.rows, "la ligne sans parent n'est pas écrite")
	require.Equal(t, 1, discarded, "elle est comptée, pour que le run le dise")
}

// parent_id NOT NULL DEFAULT 0: rows holding the "no parent" value are kept without
// asking the database.
func TestParentCheckWriter_KeepsNoParentValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	inner, zero := &recordingWriter{}, "0"
	w := NewParentCheckWriter(context.Background(), tx, MySQLDialect{}, inner, "shop.AVOIR",
		factureCheck(&zero), func([]int) {})

	require.NoError(t, w.WriteBatch([]string{"id", "commande_id"}, [][]any{{int64(1), int64(0)}}))
	require.Len(t, inner.rows, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNewParentCheckWriter_NothingToCheck(t *testing.T) {
	inner := &recordingWriter{}
	require.Same(t, RowWriter(inner), NewParentCheckWriter(context.Background(), nil, MySQLDialect{}, inner, "t", nil, nil))
}

// A composite key costs one parameter per column plus the ordinal: a fixed bound of 500
// keys goes past the 2100 parameters of SQL Server as soon as a key has three columns.
func TestMaxKeysPerLookup_StaysUnderTheLimitOfTheDatabase(t *testing.T) {
	for _, columns := range []int{1, 2, 4, 8} {
		require.LessOrEqual(t, maxKeysPerLookup(MSSQLDialect{}, columns)*(columns+1), 2000,
			"SQL Server refuses a statement of more than 2100 parameters")
		require.Positive(t, maxKeysPerLookup(MSSQLDialect{}, columns))
	}
	require.Equal(t, 500, maxKeysPerLookup(MySQLDialect{}, 1), "the batch bound leads where the database is roomy")
	require.Equal(t, 500, maxKeysPerLookup(PostgresDialect{}, 4))
}
