// Package preflight describes what a run of a job will meet, found by computing its plan
// rather than by reading its tables.
//
// The plan of a run — which columns are read and written, how each table is paged, which
// references the subset clears — is computed by the generation of the configs, from the
// schemas of the connections alone. What in it will fail, or copy other than the person
// expects, is known at that point: the same computation reports it, so that the report
// cannot tell a different story from the run. The connection checks of the run are part
// of it too, each converted from internal/connection-checks.
//
// Each finding has a level: blocking when the run fails whatever the rows, a warning when
// it may fail or copy wrong depending on the rows, information when it is what the run
// does and the person should know. A run keeps the report of its start and stops on a
// blocking finding.
package preflight

import (
	"cmp"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
)

// Kind names what a finding is about.
type Kind = mgmtv1alpha1.PreflightFinding_Kind

// Level says what a finding does to a run.
type Level = mgmtv1alpha1.PreflightFinding_Level

const (
	Blocking    = mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING
	Warning     = mgmtv1alpha1.PreflightFinding_LEVEL_WARNING
	Information = mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION
)

// Finding is one thing a run of the job will meet.
type Finding struct {
	Kind  Kind
	Level Level
	// ConnectionID is the connection concerned, when the finding is about one.
	ConnectionID string
	// Table is the table concerned, as schema.table; empty for the job or a server.
	Table string
	// Columns are the columns concerned.
	Columns []string
	// Missing names what an account lacks: privileges, or the names of absent columns.
	Missing []string
	// Message says it in a sentence.
	Message string
	// Remedy is the statement that grants what is missing, when one does.
	Remedy string
}

// connectionCheckKinds gives the kind of each connection check.
var connectionCheckKinds = map[connectionchecks.Check]Kind{
	connectionchecks.CheckTableExists:          mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS,
	connectionchecks.CheckReadable:             mgmtv1alpha1.PreflightFinding_KIND_READABLE,
	connectionchecks.CheckServerWritable:       mgmtv1alpha1.PreflightFinding_KIND_SERVER_WRITABLE,
	connectionchecks.CheckWritable:             mgmtv1alpha1.PreflightFinding_KIND_WRITABLE,
	connectionchecks.CheckTruncate:             mgmtv1alpha1.PreflightFinding_KIND_TRUNCATE,
	connectionchecks.CheckTriggers:             mgmtv1alpha1.PreflightFinding_KIND_TRIGGERS,
	connectionchecks.CheckTriggerDefiner:       mgmtv1alpha1.PreflightFinding_KIND_TRIGGER_DEFINER,
	connectionchecks.CheckForeignKeySuspension: mgmtv1alpha1.PreflightFinding_KIND_FOREIGN_KEY_SUSPENSION,
}

// FromConnectionChecks converts what a connection check found on a connection.
func FromConnectionChecks(connectionID string, found []*connectionchecks.Finding) []*Finding {
	findings := make([]*Finding, 0, len(found))
	for _, f := range found {
		level := Blocking
		if f.Level == connectionchecks.Warning {
			level = Warning
		}
		findings = append(findings, &Finding{
			Kind:         connectionCheckKinds[f.Check],
			Level:        level,
			ConnectionID: connectionID,
			Table:        f.Table,
			Missing:      f.Missing,
			Message:      f.Message,
			Remedy:       f.Remedy,
		})
	}
	return findings
}

// BlockingOf returns the findings a run stops on.
func BlockingOf(findings []*Finding) []*Finding {
	var blocking []*Finding
	for _, f := range findings {
		if f.Level == Blocking {
			blocking = append(blocking, f)
		}
	}
	return blocking
}

// Messages gives the sentence of each finding.
func Messages(findings []*Finding) []string {
	messages := make([]string, 0, len(findings))
	for _, f := range findings {
		messages = append(messages, f.Message)
	}
	return messages
}

// Sort orders findings the way they are read: the most serious first, then by table, so
// that the same job reads the same way from one run to the next.
func Sort(findings []*Finding) {
	slices.SortStableFunc(findings, func(a, b *Finding) int {
		return cmp.Or(
			cmp.Compare(a.Level, b.Level),
			strings.Compare(a.Table, b.Table),
			cmp.Compare(a.Kind, b.Kind),
			strings.Compare(a.ConnectionID, b.ConnectionID),
			strings.Compare(a.Message, b.Message),
		)
	})
}

// Report builds the report of a run on an engine.
func Report(engine mgmtv1alpha1.JobEngine, findings []*Finding) *mgmtv1alpha1.PreflightReport {
	report := &mgmtv1alpha1.PreflightReport{Engine: engine}
	for _, f := range findings {
		finding := &mgmtv1alpha1.PreflightFinding{
			Kind:    f.Kind,
			Level:   f.Level,
			Table:   f.Table,
			Columns: f.Columns,
			Missing: f.Missing,
			Message: f.Message,
		}
		if f.ConnectionID != "" {
			finding.ConnectionId = &f.ConnectionID
		}
		if f.Remedy != "" {
			finding.Remedy = &f.Remedy
		}
		report.Findings = append(report.Findings, finding)
	}
	return report
}
