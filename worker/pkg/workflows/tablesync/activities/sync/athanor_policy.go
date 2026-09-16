package sync_activity

import "strings"

// AthanorPolicy décide, PAR JOB, si le moteur Athanor est utilisé (au lieu de
// Benthos). C'est l'opt-in par job : un défaut global (ENABLE_ATHANOR_ENGINE)
// surchargeable par des listes d'identifiants de jobs. Ça permet une migration
// progressive et la comparaison A/B (le même job en Benthos puis en Athanor),
// sans toucher au proto ni à l'UI.
type AthanorPolicy struct {
	defaultEnabled bool
	overrides      map[string]bool // jobID -> forcé activé/désactivé
}

// NewAthanorPolicy builds the policy. enabledJobs / disabledJobs are
// comma-separated job ID lists that respectively force Athanor on or off for those
// jobs, whatever the default is. A job listed in both is turned on: an explicit
// enable wins.
func NewAthanorPolicy(defaultEnabled bool, enabledJobs, disabledJobs string) AthanorPolicy {
	overrides := map[string]bool{}
	for _, id := range splitIDs(disabledJobs) {
		overrides[id] = false
	}
	for _, id := range splitIDs(enabledJobs) {
		overrides[id] = true
	}
	return AthanorPolicy{defaultEnabled: defaultEnabled, overrides: overrides}
}

// EnabledFor indique si Athanor doit traiter ce job. Un jobID vide (extraction
// impossible) retombe sur le défaut global.
func (p AthanorPolicy) EnabledFor(jobID string) bool {
	if v, ok := p.overrides[jobID]; ok {
		return v
	}
	return p.defaultEnabled
}

func splitIDs(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
