package verify

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/stretchr/testify/require"
)

func Test_PreflightChanges(t *testing.T) {
	const (
		blocking    = mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING
		warning     = mgmtv1alpha1.PreflightFinding_LEVEL_WARNING
		information = mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION
		oneStream   = mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM
		retry       = mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE
	)
	report := func(findings ...*mgmtv1alpha1.PreflightFinding) *mgmtv1alpha1.PreflightReport {
		return &mgmtv1alpha1.PreflightReport{Findings: findings}
	}
	expected := []cases.ExpectedFinding{{Kind: retry, Level: warning, Table: "JOURNAL"}}

	require.Empty(t, PreflightChanges(expected, "bench_x", report(
		&mgmtv1alpha1.PreflightFinding{Kind: retry, Level: warning, Table: "bench_x.JOURNAL"},
		// Information nobody lists is not checked.
		&mgmtv1alpha1.PreflightFinding{Kind: oneStream, Level: information, Table: "bench_x.JOURNAL"},
		// Nor what a connection lacks: the rights cases check the message of the run.
		&mgmtv1alpha1.PreflightFinding{Kind: mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, Level: blocking},
	)))

	require.Len(t, PreflightChanges(expected, "bench_x", report()), 1, "an expected finding missing")
	require.Len(t, PreflightChanges(expected, "bench_x", report(
		&mgmtv1alpha1.PreflightFinding{Kind: retry, Level: information, Table: "bench_x.JOURNAL"},
	)), 1, "a finding of another level is not the one expected")
	require.Len(t, PreflightChanges(nil, "bench_x", report(
		&mgmtv1alpha1.PreflightFinding{Kind: retry, Level: warning, Table: "bench_x.JOURNAL"},
	)), 1, "a warning the case does not expect")
	require.Len(t, PreflightChanges(nil, "bench_x", report(
		&mgmtv1alpha1.PreflightFinding{Kind: mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN, Level: blocking},
	)), 1, "a blocking finding the case does not expect")

	require.Empty(t, PreflightChanges(nil, "bench_x", nil), "no report, nothing expected")
	require.Len(t, PreflightChanges(expected, "bench_x", nil), 1, "no report where findings are expected")
}
