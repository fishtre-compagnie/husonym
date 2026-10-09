package usagestore

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// Reading a row is the rule it was written with: whatever the status and whatever was told, the
// row the store writes reads as it was written, and a row emptied of its pair reads as the row
// of a run nothing was told of.
func Test_ErrorRead_IsTheRuleTheRowWasWrittenWith(t *testing.T) {
	categories := append([]string{"", "deadlock"}, telemetry.ErrorCategories...)
	steps := append([]string{"", "somewhere"}, telemetry.ErrorSteps...)
	for _, status := range Statuses() {
		for _, category := range categories {
			for _, step := range steps {
				written := errorOf(status, RunError{Category: ErrorCategory(category), Step: ErrorStep(step)})
				heldCategory, heldStep := written.columns()
				require.Equal(t, written, errorRead(status, heldCategory, heldStep), "%s %s %s", status, category, step)
			}
		}
		require.Equal(t, errorOf(status, RunError{}), errorRead(status, pgtype.Text{}, pgtype.Text{}), status)
	}
}

func Test_ErrorOf(t *testing.T) {
	other := RunError{Category: ErrorCategoryOther, Step: ErrorStepOther}
	for name, c := range map[string]struct {
		status Status
		told   RunError
		want   RunError
	}{
		"a run that completed has no error, whatever is told": {
			StatusCompleted, RunError{Category: "timeout", Step: "table_sync"}, RunError{},
		},
		"a run still running has no error": {StatusRunning, RunError{}, RunError{}},
		"a failure nothing is told of is other": {StatusFailed, RunError{}, other},
		"a failure keeps what is told": {
			StatusFailed,
			RunError{Category: "constraint_violated", Step: "table_sync"},
			RunError{Category: "constraint_violated", Step: "table_sync"},
		},
		"a failure told without its step is at the step other": {
			StatusFailed, RunError{Category: "timeout"}, RunError{Category: "timeout", Step: ErrorStepOther},
		},
		"a canceled run is canceled, at the step told": {
			StatusCanceled, RunError{Category: "timeout", Step: "hooks"}, RunError{Category: ErrorCategoryCanceled, Step: "hooks"},
		},
		"a canceled run nothing is told of": {
			StatusCanceled, RunError{}, RunError{Category: ErrorCategoryCanceled, Step: ErrorStepOther},
		},
		"a run that timed out": {
			StatusTimedOut, RunError{}, RunError{Category: ErrorCategoryTimeout, Step: ErrorStepOther},
		},
		"a run that was terminated": {StatusTerminated, RunError{}, other},
		"a category outside the list is other": {
			StatusFailed, RunError{Category: "deadlock", Step: "table_sync"}, RunError{Category: ErrorCategoryOther, Step: "table_sync"},
		},
		"a step outside the list is other": {
			StatusFailed, RunError{Category: "timeout", Step: "somewhere"}, RunError{Category: "timeout", Step: ErrorStepOther},
		},
		"a canceled run at a step outside the list": {
			StatusCanceled, RunError{Step: "somewhere"}, RunError{Category: ErrorCategoryCanceled, Step: ErrorStepOther},
		},
		"a member spelled another way is the member": {
			StatusFailed, RunError{Category: " Timeout ", Step: "HOOKS"}, RunError{Category: "timeout", Step: "hooks"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.want, errorOf(c.status, c.told))
		})
	}
}
