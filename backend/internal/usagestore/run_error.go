package usagestore

import (
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
)

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
// not told is other. What is told and is no member of the lists is other as well: the table
// refuses anything else, and a refused row would lose the whole end of the run.
func errorOf(status Status, told RunError) RunError {
	step := ErrorStep(telemetry.ErrorStep(string(told.Step)))
	switch status {
	case StatusRunning, StatusCompleted:
		return RunError{}
	case StatusCanceled:
		return RunError{Category: ErrorCategoryCanceled, Step: step}
	case StatusTimedOut:
		return RunError{Category: ErrorCategoryTimeout, Step: ErrorStepOther}
	case StatusFailed, StatusTerminated:
	}
	return RunError{Category: ErrorCategory(telemetry.ErrorCategory(string(told.Category))), Step: step}
}

// errorRead is the error a row counts for, from its status and the two values it holds. It is
// errorOf again, with what the row holds as what was told: a row the store wrote reads as it was
// written, and a row that did not complete and holds nothing reads as a run nothing was told of.
// Such rows exist, an API of the version before the two columns writes them while both versions
// serve, so a count of errors goes through here and never asks the table which rows hold a
// category.
func errorRead(status Status, category, step pgtype.Text) RunError {
	return errorOf(status, RunError{Category: ErrorCategory(category.String), Step: ErrorStep(step.String)})
}

// columns are the two values of the row: null for an error that tells nothing.
func (e RunError) columns() (category, step pgtype.Text) {
	if e == (RunError{}) {
		return pgtype.Text{}, pgtype.Text{}
	}
	return pgtype.Text{String: string(e.Category), Valid: true}, pgtype.Text{String: string(e.Step), Valid: true}
}
