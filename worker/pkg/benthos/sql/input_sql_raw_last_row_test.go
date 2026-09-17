package husonym_benthos_sql

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	database_record_mapper "github.com/fishtre-compagnie/husonym/internal/database-record-mapper"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// readPage reads a whole page through the input and returns, for each row, the value of
// its id column and whether it carries the "last row of the page" mark.
func readPage(t *testing.T, ids []int64) []struct {
	id   int64
	last bool
} {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id"})
	for _, id := range ids {
		rows = rows.AddRow(id)
	}
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	queried, err := db.QueryContext(context.Background(), "SELECT id FROM t")
	require.NoError(t, err)

	mapper, err := database_record_mapper.NewDatabaseRecordMapper(sqlmanager_shared.MysqlDriver)
	require.NoError(t, err)
	input := &pooledInput{
		logger:       service.MockResources().Logger(),
		driver:       sqlmanager_shared.MysqlDriver,
		db:           db,
		rows:         queried,
		recordMapper: mapper,
	}

	var read []struct {
		id   int64
		last bool
	}
	for {
		msg, _, err := input.Read(context.Background())
		if err != nil {
			require.ErrorIs(t, err, service.ErrEndOfInput)
			break
		}
		structured, err := msg.AsStructured()
		require.NoError(t, err)
		mark, _ := msg.MetaGet(husonym_benthos.LastRowOfPageMetaKey)
		read = append(read, struct {
			id   int64
			last bool
		}{structured.(map[string]any)["id"].(int64), mark == "true"})
		require.LessOrEqual(t, len(read), len(ids)+1, "the input keeps returning rows")
	}
	return read
}

func TestInputMarksTheLastRowOfAPage(t *testing.T) {
	read := readPage(t, []int64{1, 2, 3})
	require.Len(t, read, 3)
	for i, row := range read {
		require.Equal(t, int64(i+1), row.id, "rows come out in the order they were read")
		require.Equal(t, i == len(read)-1, row.last, "only the last row carries the mark")
	}
}

func TestInputMarksASingleRowPage(t *testing.T) {
	read := readPage(t, []int64{42})
	require.Len(t, read, 1)
	require.True(t, read[0].last)
}

func TestInputReadsAnEmptyPage(t *testing.T) {
	require.Empty(t, readPage(t, nil))
}
