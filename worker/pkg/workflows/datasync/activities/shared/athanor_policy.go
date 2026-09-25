package shared

import (
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// AthanorPolicy decides, per job, whether Athanor runs it instead of Benthos: the engine
// the job names, and when it names none the default of the deployment
// (ENABLE_ATHANOR_ENGINE), which lists of job ids can override. It is what lets a job
// move to Athanor on its own, and the same job be run by both engines for a comparison.
//
// Every activity that behaves differently by engine asks this policy, so that they all
// agree on which engine a run is on: the privilege check at the start of a run must not
// require of Benthos what only Athanor needs.
type AthanorPolicy struct {
	defaultEnabled bool
	overrides      map[string]bool // job id -> forced on or off
}

// NewAthanorPolicy builds the policy. enabledJobs and disabledJobs are comma-separated job
// ids that force Athanor on or off for those jobs, whatever the default. A job listed in
// both is turned on: an explicit enable wins.
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

// EnabledFor is the default of the deployment for a job. An empty id falls back to the
// global default.
func (p AthanorPolicy) EnabledFor(jobID string) bool {
	if v, ok := p.overrides[jobID]; ok {
		return v
	}
	return p.defaultEnabled
}

// UsesAthanor reports whether Athanor runs the job: the engine the job names, else the
// default of the deployment for it.
func (p AthanorPolicy) UsesAthanor(job *mgmtv1alpha1.Job) bool {
	switch job.GetWorkflowOptions().GetEngine() {
	case mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR:
		return true
	case mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS:
		return false
	default:
		return p.EnabledFor(job.GetId())
	}
}

// AthanorRuns tells whether Athanor can run a job, and why not: it copies one source to
// one destination of the same database, MySQL or PostgreSQL. The run is stopped at its
// start on it, before the destination is emptied or anything is written; the table syncs
// used to find out after both.
func AthanorRuns(job *mgmtv1alpha1.Job) error {
	source := ""
	switch {
	case job.GetSource().GetOptions().GetMysql() != nil:
		source = "mysql"
	case job.GetSource().GetOptions().GetPostgres() != nil:
		source = "postgres"
	default:
		return unsupported("athanor copies a MySQL or PostgreSQL source, not this one: run the job with benthos")
	}
	destinations := job.GetDestinations()
	if len(destinations) != 1 {
		return unsupported("athanor copies a source to one destination, and the job has %d: "+
			"make one job per destination", len(destinations))
	}
	destination := ""
	switch {
	case destinations[0].GetOptions().GetMysqlOptions() != nil:
		destination = "mysql"
	case destinations[0].GetOptions().GetPostgresOptions() != nil:
		destination = "postgres"
	}
	if destination != source {
		return unsupported("athanor copies a %s source to a destination of the same database: run the job with benthos",
			source)
	}
	return nil
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

// unsupported builds the reason an engine cannot run a job.
func unsupported(format string, args ...any) error {
	return &EngineUnsupportedError{fmt.Sprintf(format, args...)}
}
