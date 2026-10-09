package usagestore

import "github.com/jackc/pgx/v5/pgtype"

// ErrorCategory is what kept a run from completing. The values are the ones the table allows:
// members of a closed list, never the message of an error.
type ErrorCategory string

// ErrorStep is the step a run was at when it stopped. The values are the ones the table allows.
type ErrorStep string

// The members the store gives on its own, when nothing or not enough is told of a run.
const (
	ErrorCategoryTimeout  ErrorCategory = "timeout"
	ErrorCategoryCanceled ErrorCategory = "canceled"
	ErrorCategoryOther    ErrorCategory = "other"

	ErrorStepOther ErrorStep = "other"
)

// RunError is the category and the step of the error of a run. The zero value tells nothing.
type RunError struct {
	Category ErrorCategory
	Step     ErrorStep
}

// errorOf is what the row of a run holds of its error, from its status and what was told of it.
// A run that completed or still runs holds nothing; every other run holds a category and a step:
// a canceled run is canceled and a run that timed out is a timeout whatever is told, and what is
// not told is other.
func errorOf(status Status, told RunError) RunError {
	if told.Step == "" {
		told.Step = ErrorStepOther
	}
	switch status {
	case StatusRunning, StatusCompleted:
		return RunError{}
	case StatusCanceled:
		return RunError{Category: ErrorCategoryCanceled, Step: told.Step}
	case StatusTimedOut:
		return RunError{Category: ErrorCategoryTimeout, Step: ErrorStepOther}
	case StatusFailed, StatusTerminated:
	}
	if told.Category == "" {
		told.Category = ErrorCategoryOther
	}
	return told
}

// columns are the two values of the row: null for an error that tells nothing.
func (e RunError) columns() (category, step pgtype.Text) {
	if e == (RunError{}) {
		return pgtype.Text{}, pgtype.Text{}
	}
	return pgtype.Text{String: string(e.Category), Valid: true}, pgtype.Text{String: string(e.Step), Valid: true}
}
