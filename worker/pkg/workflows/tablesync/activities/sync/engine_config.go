package sync_activity

import "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"

// EngineConfig groups the deployment settings of the anonymization engines.
type EngineConfig struct {
	// Policy decides which jobs Athanor runs; the privilege check at the start of a run
	// asks the same one.
	Policy shared.AthanorPolicy
	// ConsistencyKey is the secret deterministic consistency derives from
	// (ANONYMIZATION_CONSISTENCY_KEY). Both engines use it: Athanor refuses to run a job
	// without it, and Benthos derives from it the permutation of TransformPhoneNumber in
	// preserve_format — which is why it does not belong to either one of them.
	ConsistencyKey string
}
