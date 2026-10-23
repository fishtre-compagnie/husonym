package piidetect

import (
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"go.temporal.io/sdk/activity"
)

// Registry is what Register needs of a worker or of a test environment.
type Registry interface {
	RegisterWorkflow(w any)
	RegisterActivity(a any)
	RegisterActivityWithOptions(a any, options activity.RegisterOptions)
}

// Register registers the two workflows and their nine activities under the names of
// their functions, and the two activities through which a run tells the API its start and
// its end.
func Register(r Registry, lic license.EEInterface, activities *Activities, usage *runusage.Activities, cfg *Config) {
	r.RegisterWorkflow(NewJobWorkflow(lic, cfg.TablesAtOnce).JobPiiDetect)
	r.RegisterWorkflow(TablePiiDetect)

	r.RegisterActivity(activities.GetPiiDetectJobDetails)
	r.RegisterActivity(activities.GetLastSuccessfulWorkflowId)
	r.RegisterActivity(activities.GetTablesToPiiScan)
	r.RegisterActivity(activities.SaveJobPiiDetectReport)

	r.RegisterActivity(activities.GetColumnData)
	r.RegisterActivity(activities.DetectPiiRegex)
	r.RegisterActivity(activities.DetectPiiLLM)
	r.RegisterActivity(activities.DetectPiiContent)
	r.RegisterActivity(activities.SaveTablePiiDetectReport)

	runusage.Register(r, usage)
}
