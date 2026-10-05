package piidetect

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/activity"
)

type SaveTablePiiDetectReportRequest struct {
	// ParentRunId is the id of the job run the report belongs to; without it the report
	// is stored under the id of the run of the table.
	ParentRunId    *string
	AccountId      string
	TableSchema    string
	TableName      string
	Report         map[string]report.Combined
	ScannedColumns []string
	Scan           *report.Scan `json:",omitempty"`
}

type SaveTablePiiDetectReportResponse struct {
	Key *mgmtv1alpha1.RunContextKey
}

// SaveTablePiiDetectReport stores the report of a table in the run contexts of the API,
// its columns in the order of their names. Storing it again replaces it.
func (a *Activities) SaveTablePiiDetectReport(
	ctx context.Context,
	req *SaveTablePiiDetectReportRequest,
) (*SaveTablePiiDetectReportResponse, error) {
	runId := activity.GetInfo(ctx).WorkflowExecution.ID
	if req.ParentRunId != nil && *req.ParentRunId != "" {
		runId = *req.ParentRunId
	}

	stored := &report.TableReport{
		TableSchema:    req.TableSchema,
		TableName:      req.TableName,
		ColumnReports:  make([]report.ColumnReport, 0, len(req.Report)),
		ScannedColumns: req.ScannedColumns,
		Scan:           req.Scan,
	}
	for column, found := range req.Report {
		stored.ColumnReports = append(stored.ColumnReports, report.ColumnReport{ColumnName: column, Report: found})
	}
	slices.SortFunc(stored.ColumnReports, func(a, b report.ColumnReport) int {
		return cmp.Compare(a.ColumnName, b.ColumnName)
	})

	key := &mgmtv1alpha1.RunContextKey{
		AccountId:  req.AccountId,
		JobRunId:   runId,
		ExternalId: report.TableReportExternalId(req.TableSchema, req.TableName),
	}
	if err := a.store(ctx, key, stored); err != nil {
		return nil, err
	}
	return &SaveTablePiiDetectReportResponse{Key: key}, nil
}

// store writes a value, as JSON, under a key of the run contexts.
func (a *Activities) store(ctx context.Context, key *mgmtv1alpha1.RunContextKey, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("unable to encode the report: %w", err)
	}
	_, err = a.jobs.SetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{Id: key, Value: encoded}))
	if err != nil {
		return fmt.Errorf("the API did not store the report: %w", err)
	}
	return nil
}
