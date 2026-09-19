package sync_activity

import "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"

// AthanorConfig groups the deployment settings of the Athanor engine.
type AthanorConfig struct {
	// Policy decides which jobs Athanor runs; the privilege check at the start of a run
	// asks the same one.
	Policy shared.AthanorPolicy
	// ConsistencyKey is the secret deterministic consistency derives from
	// (ATHANOR_CONSISTENCY_KEY). Athanor refuses to run a job without it.
	ConsistencyKey string
}
