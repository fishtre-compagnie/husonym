package piidetect

import "github.com/fishtre-compagnie/husonym/internal/license"

// Registry is what Register needs of a worker or of a test environment.
type Registry interface {
	RegisterWorkflow(w any)
	RegisterActivity(a any)
}

// Register registers the two workflows and their eight activities under the names of
// their functions.
func Register(r Registry, lic license.EEInterface, activities *Activities, cfg *Config) {
	r.RegisterWorkflow(NewJobWorkflow(lic, cfg.TablesAtOnce).JobPiiDetect)
	r.RegisterWorkflow(TablePiiDetect)

	r.RegisterActivity(activities.GetPiiDetectJobDetails)
	r.RegisterActivity(activities.GetLastSuccessfulWorkflowId)
	r.RegisterActivity(activities.GetTablesToPiiScan)
	r.RegisterActivity(activities.SaveJobPiiDetectReport)

	r.RegisterActivity(activities.GetColumnData)
	r.RegisterActivity(activities.DetectPiiRegex)
	r.RegisterActivity(activities.DetectPiiLLM)
	r.RegisterActivity(activities.SaveTablePiiDetectReport)
}
