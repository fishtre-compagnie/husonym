package usagestore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
