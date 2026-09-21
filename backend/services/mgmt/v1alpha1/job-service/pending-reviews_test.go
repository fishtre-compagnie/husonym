package v1alpha1_jobservice

import (
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/stretchr/testify/require"
)

const (
	demoJobId     = "11111111-1111-1111-1111-111111111111"
	samplingJobId = "22222222-2222-2222-2222-222222222222"
)

func copiedColumn(t *testing.T, jobId, column, dataType string) db_queries.HusonymApiUnmappedPassthrough {
	t.Helper()
	jobUuid, err := husonymdb.ToUuid(jobId)
	require.NoError(t, err)
	return db_queries.HusonymApiUnmappedPassthrough{
		JobID:       jobUuid,
		TableSchema: "public",
		TableName:   "users",
		ColumnName:  column,
		DataType:    dataType,
	}
}

func acceptedColumn(t *testing.T, jobId, column, dataType, category string) db_queries.HusonymApiColumnReview {
	t.Helper()
	jobUuid, err := husonymdb.ToUuid(jobId)
	require.NoError(t, err)
	return db_queries.HusonymApiColumnReview{
		JobID:               jobUuid,
		TableSchema:         "public",
		TableName:           "users",
		ColumnName:          column,
		ReviewedDataType:    dataType,
		ReviewedPiiCategory: category,
	}
}

func Test_pendingColumns(t *testing.T) {
	t.Run("a column nobody decided about is pending, and says why", func(t *testing.T) {
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "commentaire", "text"),
			},
			nil,
			nil,
		)
		require.Len(t, pending, 1)
		require.Equal(t, "commentaire", pending[0].GetColumnName())
		require.Equal(t, demoJobId, pending[0].GetJobId())
		require.Equal(t,
			mgmtv1alpha1.PendingColumnReason_PENDING_COLUMN_REASON_NEVER_REVIEWED,
			pending[0].GetReason(),
		)
		// Nothing recognised: no category, and above all no guessed transformer.
		require.Empty(t, pending[0].GetPiiCategory())
		require.Equal(t,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
			pending[0].GetSuggestedTransformerSource(),
		)
	})

	t.Run("personal data comes with the transformer to apply", func(t *testing.T) {
		// This is what lets the review offer "anonymize" first and "accept" second.
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "email", "character varying(255)"),
			},
			nil,
			nil,
		)
		require.Len(t, pending, 1)
		require.Equal(t, "email", pending[0].GetPiiCategory())
		require.NotEqual(t,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
			pending[0].GetSuggestedTransformerSource(),
		)
	})

	t.Run("an acceptance that still holds takes the column off the list", func(t *testing.T) {
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "commentaire", "text"),
			},
			nil,
			[]db_queries.HusonymApiColumnReview{
				acceptedColumn(t, demoJobId, "commentaire", "text", ""),
			},
		)
		require.Empty(t, pending)
	})

	t.Run("an acceptance does not cover a column that changed", func(t *testing.T) {
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "commentaire", "character varying(255)"),
			},
			nil,
			[]db_queries.HusonymApiColumnReview{
				acceptedColumn(t, demoJobId, "commentaire", "text", ""),
			},
		)
		require.Len(t, pending, 1)
		require.Equal(t,
			mgmtv1alpha1.PendingColumnReason_PENDING_COLUMN_REASON_CHANGED_SINCE_ACCEPTED,
			pending[0].GetReason(),
		)
	})

	t.Run("a column mapped since leaves the list at once", func(t *testing.T) {
		// The last run copied it in clear, but the next one will not. Waiting for that run to
		// drop it would leave the bell counting what somebody has just fixed.
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "email", "character varying(255)"),
				copiedColumn(t, demoJobId, "commentaire", "text"),
			},
			map[columnKey]struct{}{
				{demoJobId, "public", "users", "email"}: {},
			},
			nil,
		)
		require.Len(t, pending, 1)
		require.Equal(t, "commentaire", pending[0].GetColumnName())
	})

	t.Run("an acceptance covers its own job only", func(t *testing.T) {
		// The same column may be fine to copy for the demo and not for the sampling: accepting
		// it for one must leave it pending for the other. This is the scoping that makes the
		// decision mean something.
		pending := pendingColumns(
			[]db_queries.HusonymApiUnmappedPassthrough{
				copiedColumn(t, demoJobId, "commentaire", "text"),
				copiedColumn(t, samplingJobId, "commentaire", "text"),
			},
			nil,
			[]db_queries.HusonymApiColumnReview{
				acceptedColumn(t, demoJobId, "commentaire", "text", ""),
			},
		)
		require.Len(t, pending, 1)
		require.Equal(t, samplingJobId, pending[0].GetJobId())
	})
}
