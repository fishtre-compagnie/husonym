package piidetect

import (
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// What the workflows ask of their activities and of the run of a table. None of it is
// compared when a run is replayed.

// Reads of the API and pure computations last seconds.
func shortOptions(summary string) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		Summary:             summary,
	}
}

func jobDetailsOptions() workflow.ActivityOptions {
	return shortOptions("Reads the job and what it scans")
}

func lastRunOptions() workflow.ActivityOptions {
	return shortOptions("Looks for the last run of the job that stored a report")
}

// The catalogue of a source may hold thousands of tables.
func tablesOptions() workflow.ActivityOptions {
	options := shortOptions("Lists the tables to scan")
	options.StartToCloseTimeout = 5 * time.Minute
	return options
}

// What a save stores is already in the history of the run, the answers of the model
// included: it is attempted for about half a minute, so that a short absence of the API
// does not waste them.
func saveOptions(summary string) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
		Summary: summary,
	}
}

func saveJobReportOptions() workflow.ActivityOptions {
	return saveOptions("Saves the index of the run")
}

func saveTableReportOptions() workflow.ActivityOptions {
	return saveOptions("Saves the report of the table")
}

// Reading the columns is short; the sampling of rows bounds itself inside the activity.
func columnDataOptions() workflow.ActivityOptions {
	options := shortOptions("Reads the columns of the table and profiles a sample of its rows")
	options.StartToCloseTimeout = 2 * time.Minute
	return options
}

func rulesOptions() workflow.ActivityOptions {
	return shortOptions("Finds personal data by column name and by value format")
}

// The model activity is the long one: a request per batch of columns, each of which a
// local model may take minutes to answer. It reports that it is alive every 30 seconds,
// while a request is answered and between two requests, with the batches answered so far;
// that report is also how a cancellation reaches it.
//
// When it sends values it has a single attempt: another one would read the table again
// and could pick other values. It then repeats a failed request by itself.
func modelOptions(sendsValues bool) workflow.ActivityOptions {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
		Summary: "Asks the model about the columns of the table",
	}
	if sendsValues {
		options.RetryPolicy = &temporal.RetryPolicy{MaximumAttempts: 1}
	}
	return options
}

// The content activity is long too: a call to the API per 20 columns, for each of which
// the API reads 50 values and has them analyzed one by one. It reports that it is alive
// every 30 seconds, while a call is answered and between two calls; that report is also
// how a cancellation reaches it. Another attempt asks every column again.
//
//nolint:unused // used by the table workflow once it runs DetectPiiContent
func contentOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
		Summary: "Asks the API to analyze the content of the free-text columns of the table",
	}
}

// The run of a table is attempted once, on the task queue of the job run, and ends with
// it.
func tableChildOptions(id string) workflow.ChildWorkflowOptions {
	return workflow.ChildWorkflowOptions{
		WorkflowID:         id,
		WorkflowRunTimeout: 2 * time.Hour,
		RetryPolicy:        &temporal.RetryPolicy{MaximumAttempts: 1},
		ParentClosePolicy:  enums.PARENT_CLOSE_POLICY_TERMINATE,
	}
}
