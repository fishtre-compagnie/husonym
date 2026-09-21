package genbenthosconfigs_activity

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/mock"
)

func Test_reportUnmappedPassthroughs(t *testing.T) {
	job := &mgmtv1alpha1.Job{
		Id:        "11111111-1111-1111-1111-111111111111",
		AccountId: "22222222-2222-2222-2222-222222222222",
	}

	t.Run("sends the columns under the run that saw them", func(t *testing.T) {
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		columns := []*mgmtv1alpha1.UnmappedPassthrough{
			{TableSchema: "public", TableName: "users", ColumnName: "email", DataType: "text"},
		}
		jobclient.EXPECT().
			SetJobUnmappedPassthroughs(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.SetJobUnmappedPassthroughsRequest]) bool {
					// The run id is what lets the backend drop the columns this run did not see.
					return req.Msg.GetJobId() == job.GetId() &&
						req.Msg.GetAccountId() == job.GetAccountId() &&
						req.Msg.GetJobRunId() == "run-1" &&
						len(req.Msg.GetColumns()) == 1
				},
			)).
			Return(connect.NewResponse(&mgmtv1alpha1.SetJobUnmappedPassthroughsResponse{}), nil)

		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-1"}
		b.reportUnmappedPassthroughs(context.Background(), job, columns, slog.Default())
	})

	t.Run("sends an empty report too, since that is what clears the list", func(t *testing.T) {
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		jobclient.EXPECT().
			SetJobUnmappedPassthroughs(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.SetJobUnmappedPassthroughsRequest]) bool {
					return len(req.Msg.GetColumns()) == 0
				},
			)).
			Return(connect.NewResponse(&mgmtv1alpha1.SetJobUnmappedPassthroughsResponse{}), nil)

		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-2"}
		b.reportUnmappedPassthroughs(context.Background(), job, nil, slog.Default())
	})

	t.Run("a failed report does not stop the run", func(t *testing.T) {
		// Nothing to assert beyond the call returning: the method has no error to hand back, by
		// design. The data is copied either way; the bookkeeping must not be what fails a sync.
		jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
		jobclient.EXPECT().
			SetJobUnmappedPassthroughs(mock.Anything, mock.Anything).
			Return(nil, errors.New("backend unreachable"))

		b := &benthosBuilder{jobclient: jobclient, jobRunId: "run-3"}
		b.reportUnmappedPassthroughs(context.Background(), job, nil, slog.Default())
	})
}
