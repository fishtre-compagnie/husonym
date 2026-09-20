package sync_activity

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func jobWithScope(id, accountID string, scope mgmtv1alpha1.ConsistencyScope) *mgmtv1alpha1.Job {
	return &mgmtv1alpha1.Job{
		Id:              id,
		AccountId:       accountID,
		WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{ConsistencyScope: scope},
	}
}

// seedOf returns the seed the deriver gives a fixed value, to compare scopes.
func seedOf(t *testing.T, job *mgmtv1alpha1.Job, runID string) [32]byte {
	t.Helper()
	d, err := consistencyDeriver("clé-test", job, runID)
	require.NoError(t, err)
	return d.Domain("person.email").Seed("jean@exemple.fr")
}

func TestConsistencyDeriver_RequiresKey(t *testing.T) {
	_, err := consistencyDeriver("", jobWithScope("job-a", "acc-a", mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_RUN), "run-1")
	require.Error(t, err)
}

func TestConsistencyDeriver_Scopes(t *testing.T) {
	const (
		run   = mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_RUN
		job   = mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_JOB
		acct  = mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_ACCOUNT
		unset = mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_UNSPECIFIED
	)

	t.Run("run: stable within a run, different across runs", func(t *testing.T) {
		j := jobWithScope("job-a", "acc-a", run)
		require.Equal(t, seedOf(t, j, "run-1"), seedOf(t, j, "run-1"))
		require.NotEqual(t, seedOf(t, j, "run-1"), seedOf(t, j, "run-2"))
	})

	t.Run("unspecified behaves like run", func(t *testing.T) {
		require.Equal(t,
			seedOf(t, jobWithScope("job-a", "acc-a", run), "run-1"),
			seedOf(t, jobWithScope("job-a", "acc-a", unset), "run-1"))
	})

	t.Run("job: stable across runs, different across jobs", func(t *testing.T) {
		require.Equal(t,
			seedOf(t, jobWithScope("job-a", "acc-a", job), "run-1"),
			seedOf(t, jobWithScope("job-a", "acc-a", job), "run-2"))
		require.NotEqual(t,
			seedOf(t, jobWithScope("job-a", "acc-a", job), "run-1"),
			seedOf(t, jobWithScope("job-b", "acc-a", job), "run-1"))
	})

	t.Run("account: stable across jobs, different across accounts", func(t *testing.T) {
		require.Equal(t,
			seedOf(t, jobWithScope("job-a", "acc-a", acct), "run-1"),
			seedOf(t, jobWithScope("job-b", "acc-a", acct), "run-2"))
		require.NotEqual(t,
			seedOf(t, jobWithScope("job-a", "acc-a", acct), "run-1"),
			seedOf(t, jobWithScope("job-a", "acc-b", acct), "run-1"))
	})
}
