package runner

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	"github.com/stretchr/testify/require"
)

// recordingWriter keeps what it is given, and what the progress said while it wrote.
type recordingWriter struct {
	progress    *Progress
	rows        int
	writingSeen []bool
	writtenSeen []int64
	failAtBatch int
	batchesSeen int
}

func (w *recordingWriter) WriteBatch(_ []string, rows [][]any) error {
	w.batchesSeen++
	snap := w.progress.Snapshot()
	w.writingSeen = append(w.writingSeen, snap.Writing)
	w.writtenSeen = append(w.writtenSeen, snap.RowsWritten)
	if w.batchesSeen == w.failAtBatch {
		return errors.New("injected failure")
	}
	w.rows += len(rows)
	return nil
}

func TestProgress_CountsAPage(t *testing.T) {
	src, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer src.Close()

	plan := &tableplan.TablePlan{Schema: "web", Table: "users", Query: "SELECT `id` FROM `web`.`users`"}
	// The query reading the page carries the marker of its tag, ahead of the plan's query.
	mock.ExpectQuery(regexp.QuoteMeta("/* husonym-page:abc123 */ SELECT `id` FROM `web`.`users`")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2).AddRow(3))

	progress := &Progress{}
	page := &TablePage{Plan: plan, Progress: progress, QueryTag: "abc123"}
	reader, err := openPage(context.Background(), src, sqlio.MySQLDialect{}, page, nil)
	require.NoError(t, err)

	inner := &recordingWriter{progress: progress}
	w := progress.batchWriter(progress.rowsWriter(inner))
	require.NoError(t, reader.pipeline(context.Background(), 2, engine.Spec{}, w, []string{"id"}))
	require.NoError(t, mock.ExpectationsWereMet())

	require.Equal(t, ProgressSnapshot{RowsRead: 3, RowsWritten: 3}, progress.Snapshot())
	// Two batches of 2 and 1: each written while the page says it writes, the rows of the
	// first counted before the second.
	require.Equal(t, []bool{true, true}, inner.writingSeen)
	require.Equal(t, []int64{0, 2}, inner.writtenSeen)
}

func TestProgress_FailedBatchIsNotCounted(t *testing.T) {
	progress := &Progress{}
	inner := &recordingWriter{progress: progress, failAtBatch: 1}
	w := progress.batchWriter(progress.rowsWriter(inner))

	require.Error(t, w.WriteBatch([]string{"id"}, [][]any{{1}, {2}}))
	require.Equal(t, ProgressSnapshot{}, progress.Snapshot())
}

func TestProgress_NilCountsNothing(t *testing.T) {
	var progress *Progress
	inner := &recordingWriter{}
	require.Same(t, sqlio.RowWriter(inner), progress.batchWriter(inner))
	require.Same(t, sqlio.RowWriter(inner), progress.rowsWriter(inner))
	progress.rowRead()
	require.Equal(t, ProgressSnapshot{}, progress.Snapshot())
}
