package piidetect

import (
	"context"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/activity"
)

type SaveJobPiiDetectReportRequest struct {
	AccountId string
	JobId     string
	Report    *report.JobReport
}

type SaveJobPiiDetectReportResponse struct {
	Key *mgmtv1alpha1.RunContextKey
}

// SaveJobPiiDetectReport stores the index of the run in the run contexts of the API,
// under the id of the run. Storing it again replaces it.
func (a *Activities) SaveJobPiiDetectReport(
	ctx context.Context,
	req *SaveJobPiiDetectReportRequest,
) (*SaveJobPiiDetectReportResponse, error) {
	index := req.Report
	if index == nil {
		index = &report.JobReport{}
	}
	if index.SuccessfulTableReports == nil {
		// An index always holds its list, even empty: its readers expect one.
		index.SuccessfulTableReports = []*report.TableEntry{}
	}
	key := &mgmtv1alpha1.RunContextKey{
		AccountId:  req.AccountId,
		JobRunId:   activity.GetInfo(ctx).WorkflowExecution.ID,
		ExternalId: report.JobReportExternalId(req.JobId),
	}
	if err := a.store(ctx, key, index); err != nil {
		return nil, err
	}
	return &SaveJobPiiDetectReportResponse{Key: key}, nil
}
