package telemetry

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// SchemaVersion is the version of the report this package writes. A version only ever gains
// optional fields.
const SchemaVersion = 1

// Report is the usage report of the instance for one day. Every value in it is a number, a
// boolean, a date or a member of a closed list of this package.
type Report struct {
	SchemaVersion  int            `json:"schema_version"`
	Day            string         `json:"day"`          // YYYY-MM-DD, UTC
	GeneratedAt    string         `json:"generated_at"` // RFC 3339, UTC, in seconds
	Identification Identification `json:"identification"`
	Version        Version        `json:"version"`
	Sources        Sources        `json:"sources"`
	// Diagnostics is nil when the diagnostic is switched off.
	Diagnostics *Diagnostics `json:"diagnostics,omitempty"`
}

// Identification says which instance and which license the report comes from. A report is
// never built without a license in force, so every field is always there.
type Identification struct {
	KeyFingerprint string `json:"key_fingerprint"`
	LicenseID      string `json:"license_id"`
	InstanceID     string `json:"instance_id"`
	LicenseState   string `json:"license_state"`
	DaysToExpiry   int    `json:"days_to_expiry"` // negative once the key has expired
}

// Version is the version of the software.
type Version struct {
	Husonym string `json:"husonym"`
}

// Sources is how many sources the instance counts.
type Sources struct {
	Count int `json:"count"`
}

// Diagnostics is the part of the report the operator can switch off. The counters of Runs,
// Refusals and SourceEngines are the ones of the day; the other blocks are the state when the
// report is prepared.
type Diagnostics struct {
	Installation  Installation      `json:"installation"`
	Configuration Configuration     `json:"configuration"`
	Connections   []ConnectionCount `json:"connections"`
	SourceEngines []SourceEngine    `json:"source_engines"`
	Jobs          Jobs              `json:"jobs"`
	Transformers  Transformers      `json:"transformers"`
	ColumnTypes   []ColumnTypeCount `json:"column_types"`
	Features      []FeatureUse      `json:"features"`
	Refusals      []GateCount       `json:"refusals"`
	Runs          Runs              `json:"runs"`
	Errors        []ErrorCount      `json:"errors"`
	Users         Users             `json:"users"`
	Unread        Unread            `json:"unread"`
}

// Installation is where the instance runs. What cannot be read is absent.
type Installation struct {
	Kind            string `json:"kind"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	PostgresMajor   *int   `json:"postgres_major,omitempty"`
	TemporalVersion string `json:"temporal_version,omitempty"`
	Workers         *int   `json:"workers,omitempty"`
}

// Configuration is how the instance is set up.
type Configuration struct {
	AuthEnabled          bool   `json:"auth_enabled"`
	AuthProvider         string `json:"auth_provider,omitempty"` // absent when authentication is off
	AccountOIDCProviders int    `json:"account_oidc_providers"`
	Presidio             bool   `json:"presidio"`
	RunLogs              string `json:"run_logs"`
}

// ConnectionCount is how many connections of a type play a role.
type ConnectionCount struct {
	Type  string `json:"type"`
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// SourceEngine is how many runs of the day read from a version of an engine.
type SourceEngine struct {
	Type  string `json:"type"`
	Major string `json:"major"`
	Runs  int    `json:"runs"`
}

// Jobs counts the jobs, the tables and the columns they map.
type Jobs struct {
	ByKind     []JobKindCount `json:"by_kind"`
	Tables     int            `json:"tables"`
	Columns    int            `json:"columns"`
	WithSubset int            `json:"with_subset"`
}

// JobKindCount is how many jobs of a kind are, or are not, scheduled.
type JobKindCount struct {
	Kind      string `json:"kind"`
	Scheduled bool   `json:"scheduled"`
	Count     int    `json:"count"`
}

// Transformers counts the columns by transformer.
type Transformers struct {
	System             []TransformerColumns `json:"system"`
	UserDefined        int                  `json:"user_defined"`
	UserDefinedColumns int                  `json:"user_defined_columns"`
}

// TransformerColumns is how many columns a system transformer is applied to.
type TransformerColumns struct {
	Name    string `json:"name"`
	Columns int    `json:"columns"`
}

// ColumnTypeCount is how many mapped columns belong to a type family.
type ColumnTypeCount struct {
	Family  string `json:"family"`
	Columns int    `json:"columns"`
}

// FeatureUse says whether a feature of the license is in use.
type FeatureUse struct {
	Name  string `json:"name"`
	InUse bool   `json:"in_use"`
}

// GateCount is how many times a gate refused during the day.
type GateCount struct {
	Gate  string `json:"gate"`
	Count int    `json:"count"`
}

// Runs counts the runs of the day.
type Runs struct {
	ByStatus []RunCount `json:"by_status"`
	// DurationSeconds is nil when no run that ended that day has an end time.
	DurationSeconds   *Durations `json:"duration_seconds,omitempty"`
	RowsRead          string     `json:"rows_read"`
	RowsDiscarded     string     `json:"rows_discarded"`
	Retries           int        `json:"retries"`
	WithUncountedRows int        `json:"with_uncounted_rows"`
}

// RunCount is how many runs of a kind ended in a status.
type RunCount struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// Durations are the median and the 95th percentile of the duration of the runs, in seconds.
type Durations struct {
	Median int64 `json:"median"`
	P95    int64 `json:"p95"`
}

// ErrorCount is how many failures fall under a category at a step of a run.
type ErrorCount struct {
	Category string `json:"category"`
	Step     string `json:"step"`
	Count    int    `json:"count"`
}

// Users counts the accounts and the users.
type Users struct {
	Accounts int `json:"accounts"`
	Users    int `json:"users"`
	// Active30d is nil when authentication is off, as nobody signs in.
	Active30d *int        `json:"active_30d,omitempty"`
	ByRole    []RoleCount `json:"by_role"`
}

// Unread counts what the instance holds and could not read: each is left out of the blocks that
// tell a state, so that a report made from part of the instance says so.
type Unread struct {
	Jobs        int `json:"jobs"`
	Connections int `json:"connections"`
	Accounts    int `json:"accounts"`
}

// RoleCount is how many users hold a role.
type RoleCount struct {
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// Marshal is the document as JSON, with no trailing newline. Every array is sorted by its keys
// and none is null, so one state gives one sequence of bytes; rows with the same keys are ordered by their count. The report is left as it was.
func (r *Report) Marshal() ([]byte, error) {
	sorted := *r
	if r.Diagnostics != nil {
		d := *r.Diagnostics
		d.Connections = sortBy(d.Connections, func(a, b ConnectionCount) int {
			return cmp.Or(strings.Compare(a.Type, b.Type), strings.Compare(a.Role, b.Role), cmp.Compare(a.Count, b.Count))
		})
		d.SourceEngines = sortBy(d.SourceEngines, func(a, b SourceEngine) int {
			return cmp.Or(strings.Compare(a.Type, b.Type), strings.Compare(a.Major, b.Major), cmp.Compare(a.Runs, b.Runs))
		})
		d.Jobs.ByKind = sortBy(d.Jobs.ByKind, func(a, b JobKindCount) int {
			return cmp.Or(strings.Compare(a.Kind, b.Kind), compareBool(a.Scheduled, b.Scheduled), cmp.Compare(a.Count, b.Count))
		})
		d.Transformers.System = sortBy(d.Transformers.System, func(a, b TransformerColumns) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), cmp.Compare(a.Columns, b.Columns))
		})
		d.ColumnTypes = sortBy(d.ColumnTypes, func(a, b ColumnTypeCount) int {
			return cmp.Or(strings.Compare(a.Family, b.Family), cmp.Compare(a.Columns, b.Columns))
		})
		d.Features = sortBy(d.Features, func(a, b FeatureUse) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), compareBool(a.InUse, b.InUse))
		})
		d.Refusals = sortBy(d.Refusals, func(a, b GateCount) int {
			return cmp.Or(strings.Compare(a.Gate, b.Gate), cmp.Compare(a.Count, b.Count))
		})
		d.Runs.ByStatus = sortBy(d.Runs.ByStatus, func(a, b RunCount) int {
			return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Status, b.Status), cmp.Compare(a.Count, b.Count))
		})
		d.Errors = sortBy(d.Errors, func(a, b ErrorCount) int {
			return cmp.Or(strings.Compare(a.Category, b.Category), strings.Compare(a.Step, b.Step), cmp.Compare(a.Count, b.Count))
		})
		d.Users.ByRole = sortBy(d.Users.ByRole, func(a, b RoleCount) int {
			return cmp.Or(strings.Compare(a.Role, b.Role), cmp.Compare(a.Count, b.Count))
		})
		sorted.Diagnostics = &d
	}
	document, err := json.Marshal(&sorted)
	if err != nil {
		return nil, fmt.Errorf("marshaling the usage report: %w", err)
	}
	return document, nil
}

// sortBy is a sorted copy of the rows, never nil, so that an empty array is written as [].
func sortBy[T any](rows []T, compare func(a, b T) int) []T {
	sorted := make([]T, len(rows))
	copy(sorted, rows)
	slices.SortStableFunc(sorted, compare)
	return sorted
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	default:
		return 1
	}
}
