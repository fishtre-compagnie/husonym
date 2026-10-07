package usagestore

import (
	"encoding/json"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// KindOfJob says what the job does: detect PII, generate from the options of its source, and
// otherwise synchronize. A job whose type cannot be read is a synchronization, as the license
// counts it.
func KindOfJob(job *db_queries.HusonymApiJob) JobKind {
	config := &mgmtv1alpha1.JobTypeConfig{}
	if len(job.JobtypeConfig) > 0 {
		if err := json.Unmarshal(job.JobtypeConfig, config); err == nil && config.GetPiiDetect() != nil {
			return JobKindPiiDetect
		}
	}
	switch options := job.ConnectionOptions; {
	case options == nil:
		return JobKindSync
	case options.AiGenerateOptions != nil:
		return JobKindAiGenerate
	case options.GenerateOptions != nil:
		return JobKindGenerate
	}
	return JobKindSync
}
