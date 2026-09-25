package genbenthosconfigs_activity

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_reconcileJobMappings(t *testing.T) {
	job := &mgmtv1alpha1.Job{
		Id:        "11111111-1111-1111-1111-111111111111",
		AccountId: "22222222-2222-2222-2222-222222222222",
	}
	added := []*mgmtv1alpha1.JobMapping{{Schema: "public", Table: "users", Column: "telephone"}}
	removed := []*mgmtv1alpha1.JobMapping{{Schema: "public", Table: "users", Column: "commentaire"}}
	columns := []*mgmtv1alpha1.JobSourceColumn{{
		Column:   &mgmtv1alpha1.JobColumn{Schema: "public", Table: "users", Column: "telephone"},
		DataType: "text",
	}}

	t.Run("sends the changes, the columns and whether to record them, under the run", func(t *testing.T) {
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		jobclient.EXPECT().
			ReconcileJobMappings(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.ReconcileJobMappingsRequest]) bool {
					return req.Msg.GetJobId() == job.GetId() &&
						req.Msg.GetAccountId() == job.GetAccountId() &&
						req.Msg.GetJobRunId() == "run-1" &&
						len(req.Msg.GetAdded()) == 1 &&
						len(req.Msg.GetRemoved()) == 1 &&
						req.Msg.GetRemoved()[0].GetColumn() == "commentaire" &&
						len(req.Msg.GetColumns()) == 1 &&
						req.Msg.GetRecordChanges()
				},
			)).
			Return(connect.NewResponse(&mgmtv1alpha1.ReconcileJobMappingsResponse{}), nil)

		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-1"}
		require.NoError(t, b.reconcileJobMappings(context.Background(), job, added, removed, columns, true))
	})

	t.Run("a source that gave nothing to write calls nothing", func(t *testing.T) {
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-1"}
		require.NoError(t, b.reconcileJobMappings(context.Background(), job, nil, nil, nil, false))
	})

	t.Run("a failure fails the run", func(t *testing.T) {
		// The job would not say what the run decided, and nobody would be asked to review it.
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		jobclient.EXPECT().
			ReconcileJobMappings(mock.Anything, mock.Anything).
			Return(nil, errors.New("backend unavailable"))

		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-1"}
		require.Error(t, b.reconcileJobMappings(context.Background(), job, added, nil, columns, true))
	})
}

// The mappings a run would give the job: those of columns gone left out, the new ones added.
func Test_mappingsOfRun(t *testing.T) {
	mapping := func(column string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{Schema: "public", Table: "users", Column: column}
	}
	got := mappingsOfRun(
		[]*mgmtv1alpha1.JobMapping{mapping("id"), mapping("gone"), mapping("name")},
		[]*mgmtv1alpha1.JobMapping{mapping("gone")},
		[]*mgmtv1alpha1.JobMapping{mapping("email")},
	)
	columns := []string{}
	for _, m := range got {
		columns = append(columns, m.GetColumn())
	}
	require.Equal(t, []string{"id", "name", "email"}, columns)
}
