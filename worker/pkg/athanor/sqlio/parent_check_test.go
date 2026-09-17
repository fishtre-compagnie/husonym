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

const factureLookup = "SELECT v.n FROM (SELECT ? AS n, ? AS k0 UNION ALL SELECT ? AS n, ? AS k0) v " +
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
		factureCheck(nil), true, func(dropped []int) { discarded += len(dropped) })

	require.NoError(t, w.WriteBatch([]string{"id", "commande_id"},
		[][]any{{int64(1), int64(7)}, {int64(2), int64(1007)}, {int64(3), int64(7)}}))
	require.Equal(t, [][]any{{int64(1), int64(7)}, {int64(3), int64(7)}}, inner.rows)
	require.Equal(t, 1, discarded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestParentCheckWriter_FailsWithoutSkip(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT v.n FROM").WillReturnRows(sqlmock.NewRows([]string{"n"}))

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	inner := &recordingWriter{}
	w := NewParentCheckWriter(context.Background(), tx, MySQLDialect{}, inner, "shop.FACTURE",
		factureCheck(nil), false, func([]int) {})

	err = w.WriteBatch([]string{"id", "commande_id"}, [][]any{{int64(1), int64(1007)}})
	require.ErrorContains(t, err, "violent la clé étrangère (commande_id) vers shop.COMMANDE")
	require.Empty(t, inner.rows)
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
		factureCheck(&zero), true, func([]int) {})

	require.NoError(t, w.WriteBatch([]string{"id", "commande_id"}, [][]any{{int64(1), int64(0)}}))
	require.Len(t, inner.rows, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNewParentCheckWriter_NothingToCheck(t *testing.T) {
	inner := &recordingWriter{}
	require.Same(t, RowWriter(inner), NewParentCheckWriter(context.Background(), nil, MySQLDialect{}, inner, "t", nil, true, nil))
}
