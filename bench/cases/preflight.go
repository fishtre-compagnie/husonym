package cases

import mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"

// The levels a pre-flight finding has, as the cases expect them.
const (
	findingBlocking    = mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING
	findingWarning     = mgmtv1alpha1.PreflightFinding_LEVEL_WARNING
	findingInformation = mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION
)

// preflightStop is what a run stopped by its pre-flight check fails with.
const preflightStop = "pre-flight check stopped the run"

// expectFinding is a finding expected on every engine and database of the case.
func expectFinding(
	kind mgmtv1alpha1.PreflightFinding_Kind,
	level mgmtv1alpha1.PreflightFinding_Level,
	table string,
) ExpectedFinding {
	return ExpectedFinding{Kind: kind, Level: level, Table: table}
}

// on restricts a finding to one engine.
func (f ExpectedFinding) on(engine string) ExpectedFinding {
	f.Engine = engine
	return f
}
