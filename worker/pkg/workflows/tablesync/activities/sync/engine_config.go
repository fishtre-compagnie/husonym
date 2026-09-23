package sync_activity

import (
	"github.com/fishtre-compagnie/husonym/worker/pkg/consistencykey"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
)

// EngineConfig groups the deployment settings of the anonymization engines.
type EngineConfig struct {
	// Policy decides which jobs Athanor runs; the privilege check at the start of a run
	// asks the same one.
	Policy shared.AthanorPolicy
	// Keys resolves the secret deterministic consistency derives from, for the account of
	// the run at hand — it is the account's, not the process's. Both engines use it:
	// Athanor refuses to run a job without it, and Benthos derives from it the permutation
	// of TransformPhoneNumber in preserve_format, which is why it belongs to neither.
	Keys *consistencykey.Resolver
}
