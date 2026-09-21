package v1alpha1_jobservice

import (
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/stretchr/testify/require"
)

const testJobId = "11111111-1111-1111-1111-111111111111"

func changeRow(t *testing.T, kind, column string, transformer *mgmtv1alpha1.JobMappingTransformer) db_queries.HusonymApiJobMappingChange {
	t.Helper()
	jobUuid, err := husonymdb.ToUuid(testJobId)
	require.NoError(t, err)
	row := db_queries.HusonymApiJobMappingChange{
		JobID:       jobUuid,
		TableSchema: "public",
		TableName:   "users",
		ColumnName:  column,
		Kind:        kind,
	}
	if transformer != nil {
		row.Transformer = &pg_models.JobMappingTransformerModel{}
		require.NoError(t, row.Transformer.FromTransformerDto(transformer))
	}
	return row
}

func mappingsNow(t *testing.T, dtos ...*mgmtv1alpha1.JobMapping) map[jobColumn]*pg_models.JobMapping {
	t.Helper()
	out := map[jobColumn]*pg_models.JobMapping{}
	for _, m := range storedMappings(t, dtos...) {
		out[jobColumn{testJobId, columnRef{m.Schema, m.Table, m.Column}}] = m
	}
	return out
}

func kindsOf(changes []*mgmtv1alpha1.JobMappingChange) map[string]mgmtv1alpha1.JobMappingChangeKind {
	out := map[string]mgmtv1alpha1.JobMappingChangeKind{}
	for _, c := range changes {
		out[c.GetColumn().GetColumn()] = c.GetKind()
	}
	return out
}

func Test_pendingChanges(t *testing.T) {
	t.Run("an added column waits while its mapping is the run's", func(t *testing.T) {
		changes, err := pendingChanges(
			[]db_queries.HusonymApiJobMappingChange{changeRow(t, changeAdded, "email", emailMapping("users", "email").GetTransformer())},
			mappingsNow(t, emailMapping("users", "email")),
		)
		require.NoError(t, err)
		require.Equal(t, map[string]mgmtv1alpha1.JobMappingChangeKind{
			"email": mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_ADDED,
		}, kindsOf(changes))
		require.NotNil(t, changes[0].GetTransformer().GetConfig().GetGenerateEmailConfig())
	})

	t.Run("an added column whose mapping somebody changed is settled", func(t *testing.T) {
		changes, err := pendingChanges(
			[]db_queries.HusonymApiJobMappingChange{changeRow(t, changeAdded, "email", passthroughMapping("users", "email").GetTransformer())},
			mappingsNow(t, emailMapping("users", "email")),
		)
		require.NoError(t, err)
		require.Empty(t, changes)
	})

	t.Run("a removal waits for its review, the column being gone", func(t *testing.T) {
		changes, err := pendingChanges(
			[]db_queries.HusonymApiJobMappingChange{changeRow(t, changeRemoved, "commentaire", passthroughMapping("users", "commentaire").GetTransformer())},
			mappingsNow(t),
		)
		require.NoError(t, err)
		require.Equal(t, map[string]mgmtv1alpha1.JobMappingChangeKind{
			"commentaire": mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_REMOVED,
		}, kindsOf(changes))
	})

	t.Run("a type change shows the mapping the column has now, and ends with the column", func(t *testing.T) {
		row := changeRow(t, changeTypeChanged, "email", passthroughMapping("users", "email").GetTransformer())

		changes, err := pendingChanges([]db_queries.HusonymApiJobMappingChange{row}, mappingsNow(t, emailMapping("users", "email")))
		require.NoError(t, err)
		require.Len(t, changes, 1)
		require.NotNil(t, changes[0].GetTransformer().GetConfig().GetGenerateEmailConfig())

		changes, err = pendingChanges([]db_queries.HusonymApiJobMappingChange{row}, mappingsNow(t))
		require.NoError(t, err)
		require.Empty(t, changes)
	})
}
