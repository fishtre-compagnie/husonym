package piidetect

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	workflow_shared "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// The names the two workflows are registered under. The API starts the first by its name
// and recognizes the runs of the second by theirs.
const (
	JobWorkflowName   = "JobPiiDetect"
	TableWorkflowName = "TablePiiDetect"
)

// defaultTablesAtOnce is how many tables a run scans at once when nothing says how many.
const defaultTablesAtOnce = 3

// JobPiiDetectRequest is the input of the job workflow. Its serialized form is what the
// schedule of a job holds.
type JobPiiDetectRequest struct {
	JobId string
}

type JobPiiDetectResponse struct {
	// ReportKey is the key of the index of the run.
	ReportKey *mgmtv1alpha1.RunContextKey
}

// JobWorkflow is the workflow of a run of a PII detection job.
type JobWorkflow struct {
	license license.EEInterface
	// tablesAtOnce is used by the runs whose history holds no value of its own.
	tablesAtOnce int
}

func NewJobWorkflow(lic license.EEInterface, tablesAtOnce int) *JobWorkflow {
	return &JobWorkflow{license: lic, tablesAtOnce: tablesAtOnce}
}

// JobPiiDetect scans the tables of the source of a job, each in a run of its own, and
// stores the index of their reports.
func (w *JobWorkflow) JobPiiDetect(ctx workflow.Context, req *JobPiiDetectRequest) (*JobPiiDetectResponse, error) {
	licensed := workflow_shared.LicenseIsValid(ctx, w.license)
	if !licensed {
		return nil, errors.New("ee license is not valid, unable to run pii detect")
	}
	logger := log.With(workflow.GetLogger(ctx), "jobId", req.JobId)

	var activities *Activities
	var details *GetPiiDetectJobDetailsResponse
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, jobDetailsOptions()),
		activities.GetPiiDetectJobDetails,
		&GetPiiDetectJobDetailsRequest{JobId: req.JobId},
	).Get(ctx, &details)
	if err != nil {
		return nil, err
	}

	return workflow_shared.HandleWorkflowEventLifecycle(
		ctx,
		licensed,
		req.JobId,
		workflow.GetInfo(ctx).WorkflowExecution.ID,
		logger,
		func() (string, error) { return details.AccountId, nil },
		func(ctx workflow.Context, logger log.Logger) (*JobPiiDetectResponse, error) {
			return w.scan(ctx, req.JobId, details, logger)
		},
	)
}

func (w *JobWorkflow) scan(
	ctx workflow.Context,
	jobId string,
	details *GetPiiDetectJobDetailsResponse,
	logger log.Logger,
) (*JobPiiDetectResponse, error) {
	var activities *Activities
	config := details.PiiDetectConfig

	var incremental *IncrementalConfig
	if config.GetIncremental().GetIsEnabled() {
		var last *GetLastSuccessfulWorkflowIdResponse
		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, lastRunOptions()),
			activities.GetLastSuccessfulWorkflowId,
			&GetLastSuccessfulWorkflowIdRequest{AccountId: details.AccountId, JobId: jobId},
		).Get(ctx, &last)
		if err != nil {
			return nil, fmt.Errorf("unable to get last successful workflow id: %w", err)
		}
		if last.WorkflowId != nil {
			incremental = &IncrementalConfig{LastWorkflowId: *last.WorkflowId}
		}
	}

	var listed *GetTablesToPiiScanResponse
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, tablesOptions()),
		activities.GetTablesToPiiScan,
		&GetTablesToPiiScanRequest{
			AccountId:          details.AccountId,
			JobId:              jobId,
			SourceConnectionId: details.SourceConnectionId,
			Filter:             config.GetTableScanFilter(),
			IncrementalConfig:  incremental,
			Sampling:           config.GetDataSampling().GetIsEnabled(),
			UserPrompt:         config.GetUserPrompt(),
			ModelInput:         details.ModelInput,
		},
	).Get(ctx, &listed)
	if err != nil {
		return nil, err
	}

	// The number recorded by the first activity of the run is the one every replay of
	// the run reads. A run whose history holds none uses the number its worker was
	// registered with.
	tablesAtOnce := details.TablesAtOnce
	if tablesAtOnce <= 0 {
		tablesAtOnce = w.tablesAtOnce
	}
	if tablesAtOnce <= 0 {
		tablesAtOnce = defaultTablesAtOnce
	}
	outcome := scanTables(ctx, &tableScan{
		jobId:        jobId,
		details:      details,
		tables:       listed.Tables,
		previous:     listed.PreviousReports,
		tablesAtOnce: tablesAtOnce,
	}, logger)

	// The index is saved whatever became of the run meanwhile: a canceled run cancels
	// the save it has just asked for, and ends canceled.
	var saved *SaveJobPiiDetectReportResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, saveJobReportOptions()),
		activities.SaveJobPiiDetectReport,
		&SaveJobPiiDetectReportRequest{
			AccountId: details.AccountId,
			JobId:     jobId,
			Report:    outcome.report(listed.PreviousReports),
		},
	).Get(ctx, &saved)
	if err != nil {
		return nil, fmt.Errorf("unable to save job pii detect report: %w", err)
	}

	// A run that ends well says that every table was scanned. The version is only read
	// when one was not, so that a run that scanned them all records nothing of it.
	if notScanned := outcome.notFullyScanned(); len(notScanned) > 0 &&
		workflow.GetVersion(ctx, incompleteRunFailsChangeId, workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		return nil, temporal.NewApplicationError(
			incompleteMessage(notScanned, len(listed.Tables)),
			errorTypeIncompleteScan,
		)
	}
	logger.Info("PII detection completed")
	return &JobPiiDetectResponse{ReportKey: saved.Key}, nil
}

// scanOutcome is what became of the tables of a run.
type scanOutcome struct {
	scanned []*report.TableEntry
	failed  []*report.FailedTable
}

// report builds the index of the run: the tables it scanned, then the entries of the
// earlier run for the tables it did not scan anew, in the order of their names. A table
// that was scanned takes the place of its earlier entry.
func (o *scanOutcome) report(previous []*report.TableEntry) *report.JobReport {
	entries := make([]*report.TableEntry, 0, len(o.scanned)+len(previous))
	scanned := make(map[[2]string]bool, len(o.scanned))
	for _, entry := range o.scanned {
		scanned[[2]string{entry.TableSchema, entry.TableName}] = true
		entries = append(entries, entry)
	}
	for _, entry := range previous {
		if entry != nil && !scanned[[2]string{entry.TableSchema, entry.TableName}] {
			entries = append(entries, entry)
		}
	}
	slices.SortStableFunc(entries, func(a, b *report.TableEntry) int {
		return cmp.Or(cmp.Compare(a.TableSchema, b.TableSchema), cmp.Compare(a.TableName, b.TableName))
	})
	failed := slices.Clone(o.failed)
	slices.SortStableFunc(failed, func(a, b *report.FailedTable) int {
		return cmp.Or(cmp.Compare(a.TableSchema, b.TableSchema), cmp.Compare(a.TableName, b.TableName))
	})
	return &report.JobReport{SuccessfulTableReports: entries, FailedTables: failed}
}

// notFullyScanned names the tables of this run that failed or were scanned without the
// model, each with what happened to it, in the order of their names.
func (o *scanOutcome) notFullyScanned() []string {
	var names []string
	for _, table := range o.failed {
		names = append(names, table.TableSchema+"."+table.TableName+" (failed)")
	}
	for _, entry := range o.scanned {
		if entry.Incomplete {
			names = append(names, entry.TableSchema+"."+entry.TableName+" (the model did not answer)")
		}
	}
	slices.Sort(names)
	return names
}

// maxNamedTables is how many tables the message of an incomplete run names.
const maxNamedTables = 10

func incompleteMessage(notScanned []string, tables int) string {
	named := notScanned
	if len(named) > maxNamedTables {
		named = append(slices.Clone(named[:maxNamedTables]), "…")
	}
	return fmt.Sprintf("%d of %d tables were not fully scanned: %s", len(notScanned), tables, strings.Join(named, ", "))
}
