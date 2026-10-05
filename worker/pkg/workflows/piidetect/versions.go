package piidetect

// The change ids of the two workflows. Each is read only where the history of a run has
// already decided the branch: a run that does not take the branch records nothing of it,
// and replays the same whatever version the worker knows.
const (
	// A model activity that fails does not fail the table: its report is saved with what
	// the rules found. Runs started before the failure was tolerated replay as they
	// ran: the table fails with the error of the activity, and nothing is saved.
	modelFailureToleratedChangeId = "pii-detect-model-failure-tolerated"

	// A run in which a table failed, or was scanned without the model, ends failed once
	// its index is saved. Runs started before that replay as they ran: they complete.
	incompleteRunFailsChangeId = "pii-detect-incomplete-run-fails"

	// The run of a table whose id another table of the run already took gets a suffix.
	// Runs started before that replay as they ran: the second run is started under the
	// id of the first, which the server refuses.
	tableChildIdUniqueChangeId = "pii-detect-table-child-id-unique"
)
