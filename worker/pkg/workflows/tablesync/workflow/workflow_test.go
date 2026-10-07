package tablesync_workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	sync_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/activities/sync"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// pointerToString returns a pointer to the given string.
func pointerToString(s string) *string {
	return &s
}

// Test_TableSync_SingleIteration verifies that when the activity returns a nil continuation token immediately,
// the workflow completes in a single iteration.
func Test_TableSync_SingleIteration(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	// Register the workflow.
	maxIterations := 1
	tsWf := New(maxIterations)
	env.RegisterWorkflow(tsWf.TableSync)

	// Register a fake activity implementation.
	// This activity returns a response with a nil continuation token immediately.
	var syncActivity *sync_activity.Activity
	env.OnActivity(syncActivity.SyncTable, mock.Anything, mock.Anything, mock.Anything).
		Return(&sync_activity.SyncTableResponse{
			ContinuationToken: nil,
		}, nil)

	// Set activity options to be used by the workflow.
	options := workflow.ActivityOptions{
		ScheduleToStartTimeout: time.Minute,
		StartToCloseTimeout:    time.Minute,
	}
	request := &TableSyncRequest{
		AccountId:           "account1",
		Id:                  "id1",
		JobRunId:            "jobrun1",
		ContinuationToken:   nil,
		SyncActivityOptions: &options,
		TableSchema:         "schema1",
		TableName:           "table1",
	}

	env.ExecuteWorkflow(tsWf.TableSync, request)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result *TableSyncResponse
	err := env.GetWorkflowResult(&result)
	require.NoError(t, err)
	require.Equal(t, "schema1", result.Schema)
	require.Equal(t, "table1", result.Table)
}

// Test_TableSync_MultipleIterations simulates the case where the activity returns a non-nil continuation token
// on the first call and nil on the second call, causing one iteration loop.
func Test_TableSync_MultipleIterations(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	maxIterations := 30
	tsWf := New(maxIterations)
	env.RegisterWorkflow(tsWf.TableSync)

	// Use a counter so that the first activity call returns a token and the second returns nil.
	callCount := 0
	var syncActivity *sync_activity.Activity
	env.OnActivity(syncActivity.SyncTable, mock.Anything, mock.Anything, mock.Anything).
		Return(func(ctx context.Context, req *sync_activity.SyncTableRequest, meta *sync_activity.SyncMetadata) (*sync_activity.SyncTableResponse, error) {
			callCount++
			if callCount == 1 {
				// Return a non-nil token.
				return &sync_activity.SyncTableResponse{
					ContinuationToken: pointerToString("token1"),
				}, nil
			}
			// Second call returns nil token to finish the loop.
			return &sync_activity.SyncTableResponse{
				ContinuationToken: nil,
			}, nil
		})

	options := workflow.ActivityOptions{
		ScheduleToStartTimeout: time.Minute,
		StartToCloseTimeout:    time.Minute,
	}
	request := &TableSyncRequest{
		AccountId:           "account2",
		Id:                  "id2",
		JobRunId:            "jobrun2",
		ContinuationToken:   nil,
		SyncActivityOptions: &options,
		TableSchema:         "schema2",
		TableName:           "table2",
	}

	env.ExecuteWorkflow(tsWf.TableSync, request)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result *TableSyncResponse
	err := env.GetWorkflowResult(&result)
	require.NoError(t, err)
	require.Equal(t, "schema2", result.Schema)
	require.Equal(t, "table2", result.Table)
	require.Equal(t, 2, callCount)
}

// Test_TableSync_ContinueAsNew verifies that when the activity always returns a non-nil continuation token,
// after MAX_ITERATIONS the workflow issues a ContinueAsNew error.
func Test_TableSync_ContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	maxIterations := 2
	tsWf := New(maxIterations)
	env.RegisterWorkflow(tsWf.TableSync)

	// The activity always returns a non-nil token.
	var syncActivity *sync_activity.Activity
	env.OnActivity(syncActivity.SyncTable, mock.Anything, mock.Anything, mock.Anything).
		Return(func(ctx context.Context, req *sync_activity.SyncTableRequest, meta *sync_activity.SyncMetadata) (*sync_activity.SyncTableResponse, error) {
			return &sync_activity.SyncTableResponse{
				ContinuationToken: pointerToString("loop"),
			}, nil
		})

	options := workflow.ActivityOptions{
		ScheduleToStartTimeout: time.Minute,
		StartToCloseTimeout:    time.Minute,
	}
	request := &TableSyncRequest{
		AccountId:           "account3",
		Id:                  "id3",
		JobRunId:            "jobrun3",
		ContinuationToken:   nil,
		SyncActivityOptions: &options,
		TableSchema:         "schema3",
		TableName:           "table3",
	}

	env.ExecuteWorkflow(tsWf.TableSync, request)
	// Since the activity always returns a token, after MAX_ITERATIONS the workflow should not complete normally.
	err := env.GetWorkflowError()
	require.Error(t, err)

	// Verify that the error is a ContinueAsNewError.
	var continueErr *workflow.ContinueAsNewError
	require.True(t, errors.As(err, &continueErr))
}

// tableSyncWithPages runs a table workflow whose activity answers with the given pages, one
// per call, the last page having no continuation token unless it is cut off earlier.
func tableSyncWithPages(
	t *testing.T,
	maxIterations int,
	request *TableSyncRequest,
	pages []*sync_activity.SyncTableResponse,
) (*testsuite.TestWorkflowEnvironment, *Workflow) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	tsWf := New(maxIterations)
	env.RegisterWorkflow(tsWf.TableSync)

	call := 0
	var syncActivity *sync_activity.Activity
	env.OnActivity(syncActivity.SyncTable, mock.Anything, mock.Anything, mock.Anything).
		Return(func(context.Context, *sync_activity.SyncTableRequest, *sync_activity.SyncMetadata) (*sync_activity.SyncTableResponse, error) {
			page := pages[call]
			call++
			return page, nil
		})
	options := workflow.ActivityOptions{ScheduleToStartTimeout: time.Minute, StartToCloseTimeout: time.Minute}
	request.SyncActivityOptions = &options
	env.ExecuteWorkflow(tsWf.TableSync, request)
	return env, tsWf
}

func Test_TableSync_SumsTheRowsOfItsPages(t *testing.T) {
	env, _ := tableSyncWithPages(t, 30, &TableSyncRequest{TableSchema: "s", TableName: "t"}, []*sync_activity.SyncTableResponse{
		{ContinuationToken: pointerToString("a"), RowsRead: 10},
		{ContinuationToken: pointerToString("b"), RowsRead: 20, RowsDiscarded: 1},
		{RowsRead: 5},
	})
	require.NoError(t, env.GetWorkflowError())
	var result *TableSyncResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	require.EqualValues(t, 35, result.RowsRead)
	require.EqualValues(t, 1, result.RowsDiscarded)
}

func Test_TableSync_ContinueAsNewCarriesTheSums(t *testing.T) {
	env, _ := tableSyncWithPages(t, 2, &TableSyncRequest{TableSchema: "s", TableName: "t"}, []*sync_activity.SyncTableResponse{
		{ContinuationToken: pointerToString("a"), RowsRead: 10, Retries: 1},
		{ContinuationToken: pointerToString("b"), RowsRead: 20, RowsDiscarded: 1},
		{RowsRead: 5},
	})
	var continueErr *workflow.ContinueAsNewError
	require.True(t, errors.As(env.GetWorkflowError(), &continueErr))
	var next TableSyncRequest
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &next))
	require.EqualValues(t, 30, next.RowsRead)
	require.EqualValues(t, 1, next.RowsDiscarded)
	require.EqualValues(t, 1, next.Retries)
}

// One page that was not counted is enough: the table is not counted.
func Test_TableSync_TellsATableWithAPageThatWasNotCounted(t *testing.T) {
	env, _ := tableSyncWithPages(t, 30, &TableSyncRequest{TableSchema: "s", TableName: "t"}, []*sync_activity.SyncTableResponse{
		{ContinuationToken: pointerToString("a"), RowsRead: 10},
		{ContinuationToken: pointerToString("b"), Uncounted: true},
		{RowsRead: 5},
	})
	require.NoError(t, env.GetWorkflowError())
	var result *TableSyncResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Uncounted)
	require.EqualValues(t, 15, result.RowsRead)
}

func Test_TableSync_ATableWhosePagesWereAllCountedIsCounted(t *testing.T) {
	env, _ := tableSyncWithPages(t, 30, &TableSyncRequest{TableSchema: "s", TableName: "t"}, []*sync_activity.SyncTableResponse{
		{ContinuationToken: pointerToString("a"), RowsRead: 10},
		{RowsRead: 5},
	})
	require.NoError(t, env.GetWorkflowError())
	var result *TableSyncResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.Uncounted)
}

func Test_TableSync_ContinueAsNewCarriesAPageThatWasNotCounted(t *testing.T) {
	env, _ := tableSyncWithPages(t, 2, &TableSyncRequest{TableSchema: "s", TableName: "t"}, []*sync_activity.SyncTableResponse{
		{ContinuationToken: pointerToString("a"), Uncounted: true},
		{ContinuationToken: pointerToString("b"), RowsRead: 20},
		{RowsRead: 5},
	})
	var continueErr *workflow.ContinueAsNewError
	require.True(t, errors.As(env.GetWorkflowError(), &continueErr))
	var next TableSyncRequest
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &next))
	require.True(t, next.Uncounted)
}

// What an earlier run of the table could not count stays so, whatever the pages after it.
func Test_TableSync_KeepsWhatTheRequestBroughtUncounted(t *testing.T) {
	env, _ := tableSyncWithPages(t, 30, &TableSyncRequest{TableSchema: "s", TableName: "t", Uncounted: true},
		[]*sync_activity.SyncTableResponse{{RowsRead: 5}})
	require.NoError(t, env.GetWorkflowError())
	var result *TableSyncResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Uncounted)
}

func Test_TableSync_AddsToWhatTheRequestBrought(t *testing.T) {
	env, _ := tableSyncWithPages(t, 30, &TableSyncRequest{TableSchema: "s", TableName: "t", RowsRead: 30},
		[]*sync_activity.SyncTableResponse{{RowsRead: 5}})
	require.NoError(t, env.GetWorkflowError())
	var result *TableSyncResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	require.EqualValues(t, 35, result.RowsRead)
}
