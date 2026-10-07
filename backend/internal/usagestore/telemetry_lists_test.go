package usagestore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// The usage report names job kinds and run statuses by closed lists of its own, which cannot
// import this package; they must be the values the table holds.
func Test_TheUsageReportLists_AreTheKindsAndStatusesOfTheStore(t *testing.T) {
	require.ElementsMatch(t, []string{
		string(JobKindSync), string(JobKindGenerate), string(JobKindAiGenerate), string(JobKindPiiDetect),
	}, telemetry.JobKinds)
	require.ElementsMatch(t, []string{
		string(StatusRunning), string(StatusCompleted), string(StatusFailed),
		string(StatusCanceled), string(StatusTerminated), string(StatusTimedOut),
	}, telemetry.RunStatuses)
}
