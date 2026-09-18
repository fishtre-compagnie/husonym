package shared

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

func TestAthanorPolicy(t *testing.T) {
	const jobA, jobB, jobC = "job-a", "job-b", "job-c"

	cases := []struct {
		name           string
		defaultEnabled bool
		enabled        string
		disabled       string
		want           map[string]bool
	}{
		{
			name:           "défaut activé, aucun override",
			defaultEnabled: true,
			want:           map[string]bool{jobA: true, jobB: true, "": true},
		},
		{
			name:           "défaut désactivé, allowlist",
			defaultEnabled: false,
			enabled:        jobA + " , " + jobB, // espaces tolérés
			want:           map[string]bool{jobA: true, jobB: true, jobC: false, "": false},
		},
		{
			name:           "défaut activé, denylist",
			defaultEnabled: true,
			disabled:       jobA,
			want:           map[string]bool{jobA: false, jobB: true},
		},
		{
			name:           "présent dans les deux -> activation gagne",
			defaultEnabled: false,
			enabled:        jobA,
			disabled:       jobA,
			want:           map[string]bool{jobA: true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewAthanorPolicy(c.defaultEnabled, c.enabled, c.disabled)
			for job, want := range c.want {
				if got := p.EnabledFor(job); got != want {
					t.Errorf("EnabledFor(%q) = %v, voulu %v", job, got, want)
				}
			}
		})
	}
}

// The engine a job names wins over the default of the deployment; a job naming none
// follows it.
func TestAthanorPolicy_UsesAthanor(t *testing.T) {
	p := NewAthanorPolicy(false, "listed", "")
	job := func(id string, engine mgmtv1alpha1.JobEngine) *mgmtv1alpha1.Job {
		return &mgmtv1alpha1.Job{Id: id, WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{Engine: engine}}
	}
	for name, tc := range map[string]struct {
		job  *mgmtv1alpha1.Job
		want bool
	}{
		"names athanor":                {job("any", mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR), true},
		"names benthos, though listed": {job("listed", mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS), false},
		"names none, listed":           {job("listed", mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED), true},
		"names none, not listed":       {job("other", mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED), false},
	} {
		if got := p.UsesAthanor(tc.job); got != tc.want {
			t.Errorf("%s: UsesAthanor = %v, voulu %v", name, got, tc.want)
		}
	}
}
