package piidetect

import (
	"errors"
	"strconv"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	workflow_shared "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// maxReason is the length, in bytes, the reason of a failed table is cut at in the
	// index.
	maxReason = 300
	// maxWorkflowId is the length an id of a workflow is cut at.
	maxWorkflowId = 1000
)

// tableScan is what the scan of the tables of a run needs.
type tableScan struct {
	jobId        string
	details      *GetPiiDetectJobDetailsResponse
	tables       []TableToScan
	previous     []*report.TableEntry
	tablesAtOnce int
}

// scanTables scans the tables, a fixed number of them at once, each in a child workflow,
// and returns what became of each.
//
// The arrangement is the one recorded in the histories of the runs and must stay as it
// is: every table is put on a queue, which is closed; as many consumers as tables are
// scanned at once, however few the tables, each take a table, start its child, wait for
// its end and take the next; the run waits for all the consumers. A table then starts in
// the workflow task that sees another one end. Consumers and children share the context
// of the run, so that a canceled run asks its children to cancel in the order they
// started, and starts no other.
//
// A table that fails, times out or cannot be started does not stop the others.
func scanTables(ctx workflow.Context, scan *tableScan, logger log.Logger) *scanOutcome {
	runId := workflow.GetInfo(ctx).WorkflowExecution.ID
	previousKeys := make(map[[2]string]*mgmtv1alpha1.RunContextKey, len(scan.previous))
	for _, entry := range scan.previous {
		if entry != nil {
			previousKeys[[2]string{entry.TableSchema, entry.TableName}] = entry.ReportKey
		}
	}

	queue := workflow.NewBufferedChannel(ctx, len(scan.tables))
	for _, table := range scan.tables {
		queue.Send(ctx, table)
	}
	queue.Close()

	config := scan.details.PiiDetectConfig
	outcome := &scanOutcome{}
	started := map[string]bool{} // the ids of the children of this run
	consumers := workflow.NewWaitGroup(ctx)
	for range scan.tablesAtOnce {
		consumers.Add(1)
		workflow.Go(ctx, func(ctx workflow.Context) {
			defer consumers.Done()
			var table TableToScan
			for queue.Receive(ctx, &table) {
				// Two tables whose names give the same id, started at the same instant of
				// the run's clock, would be one child. The version is only read when that
				// happens, so that a run without such tables records nothing of it.
				id := workflow_shared.BuildChildWorkflowId(runId, table.Schema+"."+table.Table, workflow.Now(ctx))
				if started[id] &&
					workflow.GetVersion(ctx, tableChildIdUniqueChangeId, workflow.DefaultVersion, 1) != workflow.DefaultVersion {
					id = uniqueChildId(id, started)
				}
				started[id] = true

				var scanned *TablePiiDetectResponse
				err := workflow.ExecuteChildWorkflow(
					workflow.WithChildOptions(ctx, tableChildOptions(id)),
					TablePiiDetect,
					&TablePiiDetectRequest{
						AccountId:          scan.details.AccountId,
						JobId:              scan.jobId,
						ConnectionId:       scan.details.SourceConnectionId,
						TableSchema:        table.Schema,
						TableName:          table.Table,
						ShouldSampleData:   config.GetDataSampling().GetIsEnabled(),
						UserPrompt:         config.GetUserPrompt(),
						PreviousResultsKey: previousKeys[[2]string{table.Schema, table.Table}],
						ParentExecutionId:  &runId,
						ModelInput:         scan.details.ModelInput,
					},
				).Get(ctx, &scanned)
				if err != nil {
					logger.Error("the table was not scanned", "schema", table.Schema, "table", table.Table, "error", err)
					outcome.failed = append(outcome.failed, &report.FailedTable{
						TableSchema: table.Schema,
						TableName:   table.Table,
						Reason:      cutReason(cause(err)),
					})
					continue
				}
				outcome.scanned = append(outcome.scanned, &report.TableEntry{
					TableSchema:     table.Schema,
					TableName:       table.Table,
					ReportKey:       scanned.ResultKey,
					ScanFingerprint: table.Fingerprint,
					Incomplete:      scanned.Model == report.ModelFailed,
				})
			}
		})
	}
	consumers.Wait(ctx)
	return outcome
}

// uniqueChildId gives an id that no child of the run has yet: the id followed by "-2",
// "-3", and so on. An id stays within its length: the suffix takes the place of its end.
func uniqueChildId(id string, started map[string]bool) string {
	for n := 2; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		candidate := id
		if len(candidate)+len(suffix) > maxWorkflowId {
			candidate = candidate[:maxWorkflowId-len(suffix)]
		}
		candidate += suffix
		if !started[candidate] {
			return candidate
		}
	}
}

// cutReason cuts a reason to maxReason, on a character.
func cutReason(reason string) string {
	if len(reason) <= maxReason {
		return reason
	}
	return strings.ToValidUTF8(reason[:maxReason], "")
}

// cause is the message of the innermost error of a failed child: the outer ones name the
// workflow and the activity, which the index already says.
func cause(err error) string {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			break
		}
		err = inner
	}
	var application *temporal.ApplicationError
	if errors.As(err, &application) {
		return application.Message()
	}
	return err.Error()
}
