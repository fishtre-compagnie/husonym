package preflight

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_FromConnectionChecks(t *testing.T) {
	findings := FromConnectionChecks("conn", []*connectionchecks.Finding{
		{Check: connectionchecks.CheckWritable, Level: connectionchecks.Blocking, Table: "public.a",
			Missing: []string{"INSERT"}, Message: "cannot write", Remedy: "GRANT INSERT ON public.a TO x"},
		{Check: connectionchecks.CheckForeignKeySuspension, Level: connectionchecks.Warning, Message: "may not suspend"},
	})
	require.Len(t, findings, 2)
	assert.Equal(t, &Finding{
		Kind: mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, Level: Blocking, ConnectionID: "conn",
		Table: "public.a", Missing: []string{"INSERT"}, Message: "cannot write", Remedy: "GRANT INSERT ON public.a TO x",
	}, findings[0])
	assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_FOREIGN_KEY_SUSPENSION, findings[1].Kind)
	assert.Equal(t, Warning, findings[1].Level)
}

// Every connection check has a kind of its own: a finding never reads as unspecified.
func Test_ConnectionCheckKinds_Complete(t *testing.T) {
	for _, check := range []connectionchecks.Check{
		connectionchecks.CheckTableExists, connectionchecks.CheckReadable, connectionchecks.CheckServerWritable,
		connectionchecks.CheckWritable, connectionchecks.CheckTruncate, connectionchecks.CheckTriggers,
		connectionchecks.CheckTriggerDefiner, connectionchecks.CheckForeignKeySuspension,
	} {
		kind, ok := connectionCheckKinds[check]
		assert.True(t, ok, check)
		assert.NotEqual(t, mgmtv1alpha1.PreflightFinding_KIND_UNSPECIFIED, kind, check)
	}
}

func Test_SortAndBlocking(t *testing.T) {
	findings := []*Finding{
		{Level: Information, Table: "public.a", Message: "i"},
		{Level: Warning, Table: "public.b", Message: "w"},
		{Level: Blocking, Table: "public.c", Message: "b2"},
		{Level: Blocking, Table: "public.a", Message: "b1"},
	}
	Sort(findings)
	assert.Equal(t, []string{"b1", "b2", "w", "i"}, Messages(findings))
	assert.Equal(t, []string{"b1", "b2"}, Messages(BlockingOf(findings)))
}

func Test_Report(t *testing.T) {
	report := Report(mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR, []*Finding{
		{Kind: mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, Level: Blocking, ConnectionID: "conn", Remedy: "GRANT"},
		{Kind: mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM, Level: Information, Table: "public.a"},
	})
	assert.Equal(t, mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR, report.GetEngine())
	require.Len(t, report.GetFindings(), 2)
	assert.Equal(t, "conn", report.GetFindings()[0].GetConnectionId())
	assert.Equal(t, "GRANT", report.GetFindings()[0].GetRemedy())
	assert.Nil(t, report.GetFindings()[1].ConnectionId)
	assert.Nil(t, report.GetFindings()[1].Remedy)
}
