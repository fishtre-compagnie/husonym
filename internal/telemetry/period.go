package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MaxPeriodMonths is how many months a report for a period holds at most.
const MaxPeriodMonths = 36

// MonthLayout is how a month is written in the report for a period: YYYY-MM, UTC.
const MonthLayout = "2006-01"

// PeriodReport is the usage report of the instance for a period of months. It follows the rule
// of the report of a day: every value in it is a number, a boolean, a date or a member of a
// closed list of this package.
type PeriodReport struct {
	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"` // RFC 3339, UTC, in seconds
	// From and To are the first and the last month of the period, both included.
	From string `json:"from"`
	To   string `json:"to"`
	// Identification is the one of the license key in force when the report is made.
	Identification Identification `json:"identification"`
	Months         []MonthReport  `json:"months"`
}

// MonthReport is what the instance tells of a month. Runs, Refusals and State belong to the
// diagnostic: they are absent when it is switched off.
type MonthReport struct {
	Month string `json:"month"`
	// DaysReported is how many days of the month have a report of the day.
	DaysReported int `json:"days_reported"`
	// Version is the one of the last report of the month, nil for a month that has none.
	Version *Version `json:"version,omitempty"`
	// Sources is the highest count the reports of the month hold, zero for a month that has none.
	Sources Sources `json:"sources"`
	// Runs counts the runs whose end the instance recorded during the month.
	Runs *Runs `json:"runs,omitempty"`
	// Refusals counts the refusals of the month. They are written with Runs, as an empty array
	// when no gate refused, and never without it.
	Refusals []GateCount `json:"refusals,omitzero"`
	// State is the state the last report of the month tells, nil for a month that has none.
	State *MonthState `json:"state,omitempty"`
}

// MonthState is the blocks of a diagnostic that tell a state, as against what is counted over a
// day.
type MonthState struct {
	Installation  Installation      `json:"installation"`
	Configuration Configuration     `json:"configuration"`
	Connections   []ConnectionCount `json:"connections"`
	Jobs          Jobs              `json:"jobs"`
	Transformers  Transformers      `json:"transformers"`
	ColumnTypes   []ColumnTypeCount `json:"column_types"`
	Features      []FeatureUse      `json:"features"`
	Users         Users             `json:"users"`
	Unread        Unread            `json:"unread"`
}

// StateOf is the state a diagnostic tells.
func StateOf(d *Diagnostics) *MonthState {
	return &MonthState{
		Installation:  d.Installation,
		Configuration: d.Configuration,
		Connections:   d.Connections,
		Jobs:          d.Jobs,
		Transformers:  d.Transformers,
		ColumnTypes:   d.ColumnTypes,
		Features:      d.Features,
		Users:         d.Users,
		Unread:        d.Unread,
	}
}

// Marshal is the document as JSON, with no trailing newline. The months are in order, every
// array is sorted the way Report.Marshal sorts it and none is null, so one state gives one
// sequence of bytes. The report is left as it was.
func (r *PeriodReport) Marshal() ([]byte, error) {
	sorted := *r
	sorted.Months = sortBy(r.Months, func(a, b MonthReport) int { return strings.Compare(a.Month, b.Month) })
	for i := range sorted.Months {
		month := &sorted.Months[i]
		switch {
		case month.Runs != nil:
			runs := *month.Runs
			runs.ByStatus = sortedRunCounts(runs.ByStatus)
			month.Runs = &runs
			month.Refusals = sortedRefusals(month.Refusals)
		case month.Refusals != nil:
			return nil, errors.New("marshaling the usage report for a period: a month tells its refusals without its runs")
		}
		if month.State != nil {
			state := *month.State
			state.Connections = sortedConnections(state.Connections)
			state.Jobs.ByKind = sortedJobKinds(state.Jobs.ByKind)
			state.Transformers.System = sortedTransformers(state.Transformers.System)
			state.ColumnTypes = sortedColumnTypes(state.ColumnTypes)
			state.Features = sortedFeatures(state.Features)
			state.Users.ByRole = sortedRoles(state.Users.ByRole)
			month.State = &state
		}
	}
	document, err := json.Marshal(&sorted)
	if err != nil {
		return nil, fmt.Errorf("marshaling the usage report for a period: %w", err)
	}
	return document, nil
}
