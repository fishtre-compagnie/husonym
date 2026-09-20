package pg_models

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

func TestWorkflowOptions_RoundTrip(t *testing.T) {
	timeout := int64(60)
	in := &mgmtv1alpha1.WorkflowOptions{
		RunTimeout:       &timeout,
		Engine:           mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
		ConsistencyScope: mgmtv1alpha1.ConsistencyScope_CONSISTENCY_SCOPE_JOB,
	}

	var stored WorkflowOptions
	stored.FromDto(in)
	out := stored.ToDto()

	if out.GetRunTimeout() != timeout || out.GetEngine() != in.GetEngine() ||
		out.GetConsistencyScope() != in.GetConsistencyScope() {
		t.Fatalf("options perdues à l'aller-retour : %v", out)
	}
}
