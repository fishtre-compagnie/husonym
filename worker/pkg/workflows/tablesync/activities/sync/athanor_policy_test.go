package sync_activity

import "testing"

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
