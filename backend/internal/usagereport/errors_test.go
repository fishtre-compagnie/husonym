package usagereport

import (
	"errors"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func Test_ErrorCounts(t *testing.T) {
	t.Run("no row is an empty list, never nil", func(t *testing.T) {
		counts := ErrorCounts(nil)
		require.NotNil(t, counts)
		require.Empty(t, counts)
	})

	for name, tc := range map[string]struct {
		rows []usagestore.ErrorCount
		want []telemetry.ErrorCount
	}{
		"a row of the lists passes as it is": {
			[]usagestore.ErrorCount{{Category: "constraint_violated", Step: "table_sync", Count: 2}},
			[]telemetry.ErrorCount{{Category: "constraint_violated", Step: "table_sync", Count: 2}},
		},
		"a category outside the list is other, and meets the row that is other": {
			[]usagestore.ErrorCount{
				{Category: "deadlock", Step: "table_sync", Count: 1},
				{Category: "other", Step: "table_sync", Count: 3},
			},
			[]telemetry.ErrorCount{{Category: "other", Step: "table_sync", Count: 4}},
		},
		"a step outside the list is other": {
			[]usagestore.ErrorCount{{Category: "timeout", Step: "somewhere", Count: 1}},
			[]telemetry.ErrorCount{{Category: "timeout", Step: "other", Count: 1}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.ElementsMatch(t, tc.want, ErrorCounts(tc.rows))
		})
	}
}

func Test_Build_TellsTheErrorsOfTheDay(t *testing.T) {
	t.Run("the errors the store counted are the block of the report", func(t *testing.T) {
		f := newFixture(t)
		f.counters.errors = []usagestore.ErrorCount{
			{Category: "timeout", Step: "preflight", Count: 1},
			{Category: "constraint_violated", Step: "table_sync", Count: 2},
		}

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.NoError(t, err)
		require.NoError(t, telemetry.Validate(sealed.Document))
		require.Equal(t, []any{
			map[string]any{"category": "constraint_violated", "step": "table_sync", "count": float64(2)},
			map[string]any{"category": "timeout", "step": "preflight", "count": float64(1)},
		}, block(t, tree(t, sealed.Document), "diagnostics")["errors"])
		// The errors are the ones of the day the runs are counted for.
		require.Equal(t, []time.Time{reportDay}, f.counters.errorDays)
	})

	t.Run("errors that cannot be read fail the report, which is not sealed", func(t *testing.T) {
		f := newFixture(t)
		failure := errors.New("unavailable")
		f.counters.errorsErr = failure

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.ErrorIs(t, err, failure)
		require.ErrorContains(t, err, "unable to read the errors of the day")
		require.Nil(t, sealed)
	})

	t.Run("without the diagnostics the errors are not read", func(t *testing.T) {
		f := newFixture(t)
		f.facts.Diagnostics = false

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.NoError(t, err)
		require.NotContains(t, tree(t, sealed.Document), "diagnostics")
		require.Empty(t, f.counters.errorDays)
	})
}
