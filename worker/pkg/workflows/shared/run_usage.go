package workflow_shared

// RunTotals is what the tables of a run counted, summed by the run workflow as each table
// finishes. Workflow code runs on one thread at a time, so plain additions are safe; the
// totals must only be added to from workflow code, never from an activity.
type RunTotals struct {
	RowsRead      int64
	RowsDiscarded int64
	Retries       int64
}
