package telemetry

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// fullPeriod is a period of three months: one that was reported day after day, one where nothing
// ran and no report was kept, and the month the file is made in.
func fullPeriod() *PeriodReport {
	daily := fullReport()
	return &PeriodReport{
		SchemaVersion:  SchemaVersion,
		GeneratedAt:    "2026-10-07T00:05:12Z",
		From:           "2026-08",
		To:             "2026-10",
		Identification: daily.Identification,
		Months: []MonthReport{
			{
				Month: "2026-08", DaysReported: 31, Version: &Version{Husonym: "v0.3.0"}, Sources: &Sources{Count: 3},
				Runs:     &daily.Diagnostics.Runs,
				Refusals: daily.Diagnostics.Refusals,
				State:    StateOf(daily.Diagnostics),
			},
			{
				Month:    "2026-09",
				Runs:     &Runs{ByStatus: []RunCount{}, RowsRead: "lt_1k", RowsDiscarded: "lt_1k"},
				Refusals: []GateCount{},
			},
			{
				Month: "2026-10", DaysReported: 6, Version: &Version{Husonym: "v0.3.1"}, Sources: &Sources{Count: 4},
				Runs: &Runs{
					ByStatus: []RunCount{{Kind: "sync", Status: "completed", Count: 2}},
					RowsRead: "lt_10k", RowsDiscarded: "lt_1k",
				},
				Refusals: []GateCount{},
				State:    StateOf(daily.Diagnostics),
			},
		},
	}
}

func periodTree(t *testing.T, r *PeriodReport) map[string]any {
	t.Helper()
	document, err := r.Marshal()
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(document, &tree))
	return tree
}

func monthOf(tree map[string]any, i int) map[string]any {
	return tree["months"].([]any)[i].(map[string]any)
}

func Test_PeriodReport_FullPassesValidatePeriod(t *testing.T) {
	document, err := fullPeriod().Marshal()
	require.NoError(t, err)
	require.NoError(t, ValidatePeriod(document))
	require.NotEqual(t, byte('\n'), document[len(document)-1])
}

// The two documents are not one another: each is refused by the schema of the other.
func Test_ValidatePeriod_IsNotValidate(t *testing.T) {
	period, err := fullPeriod().Marshal()
	require.NoError(t, err)
	daily, err := fullReport().Marshal()
	require.NoError(t, err)
	require.Error(t, Validate(period))
	require.Error(t, ValidatePeriod(daily))
	require.Error(t, ValidatePeriod([]byte(`not json`)))
	require.Error(t, ValidatePeriod([]byte(`{}`)))
}

// The state of a month is the diagnostic of a day without what is counted over the day.
func Test_StateOf_KeepsTheStateBlocksOfADiagnostic(t *testing.T) {
	d := fullReport().Diagnostics
	state := StateOf(d)
	require.Equal(t, &MonthState{
		Installation: d.Installation, Configuration: d.Configuration, Connections: d.Connections,
		Jobs: d.Jobs, Transformers: d.Transformers, ColumnTypes: d.ColumnTypes, Features: d.Features,
		Users: d.Users, Unread: d.Unread,
	}, state)

	tree := periodTree(t, fullPeriod())
	written := monthOf(tree, 0)["state"].(map[string]any)
	require.Len(t, written, len(monthStateBlocks()))
	for _, block := range monthStateBlocks() {
		require.Contains(t, written, block)
	}
}

// A month nothing was kept of has no version, no sources and no state: it says nothing of what
// it does not know, and zero days reported says why. The diagnostic switched off leaves the
// month, its days, its version and its sources.
func Test_PeriodReport_OmitsWhatAMonthHasNot(t *testing.T) {
	r := fullPeriod()
	tree := periodTree(t, r)
	require.Equal(t, map[string]any{
		"month": "2026-09", "days_reported": float64(0),
		"runs": map[string]any{
			"by_status": []any{}, "rows_read": "lt_1k", "rows_discarded": "lt_1k",
			"retries": float64(0), "with_uncounted_rows": float64(0),
		},
		"refusals": []any{},
	}, monthOf(tree, 1))

	for i := range r.Months {
		r.Months[i].Runs, r.Months[i].Refusals, r.Months[i].State = nil, nil, nil
	}
	document, err := r.Marshal()
	require.NoError(t, err)
	require.NoError(t, ValidatePeriod(document))
	tree = periodTree(t, r)
	require.Equal(t, map[string]any{
		"month": "2026-08", "days_reported": float64(31), "version": map[string]any{"husonym": "v0.3.0"},
		"sources": map[string]any{"count": float64(3)},
	}, monthOf(tree, 0))
	require.Equal(t, map[string]any{"month": "2026-09", "days_reported": float64(0)}, monthOf(tree, 1))
	require.NotContains(t, string(document), `"count":0`)
}

// The refusals of a month are written with its runs: an empty array when no gate refused, and
// never without the runs.
func Test_PeriodReport_TellsTheRefusalsWithTheRuns(t *testing.T) {
	r := fullPeriod()
	r.Months[1].Refusals = nil
	require.Equal(t, []any{}, monthOf(periodTree(t, r), 1)["refusals"])

	r.Months[1].Runs, r.Months[1].Refusals = nil, []GateCount{{Gate: "subsetting", Count: 1}}
	_, err := r.Marshal()
	require.Error(t, err)

	// The schema holds the same: one is not there without the other.
	for _, alone := range []string{"runs", "refusals"} {
		tree := periodTree(t, fullPeriod())
		delete(monthOf(tree, 0), alone)
		document, _ := json.Marshal(tree)
		require.Error(t, ValidatePeriod(document), alone)
	}
}

func Test_PeriodMarshal_GivesTheSameBytesWhateverTheOrder(t *testing.T) {
	want, err := fullPeriod().Marshal()
	require.NoError(t, err)

	shuffled := fullPeriod()
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffleRows(rng, shuffled.Months)
		for i := range shuffled.Months {
			month := &shuffled.Months[i]
			shuffleRows(rng, month.Refusals)
			shuffleRows(rng, month.Runs.ByStatus)
			if month.State == nil {
				continue
			}
			shuffleRows(rng, month.State.Connections)
			shuffleRows(rng, month.State.Jobs.ByKind)
			shuffleRows(rng, month.State.Transformers.System)
			shuffleRows(rng, month.State.ColumnTypes)
			shuffleRows(rng, month.State.Features)
			shuffleRows(rng, month.State.Users.ByRole)
		}
		got, err := shuffled.Marshal()
		require.NoError(t, err)
		require.Equal(t, string(want), string(got))
	}
}

func Test_PeriodMarshal_SortsAndLeavesTheReportAlone(t *testing.T) {
	r := fullPeriod()
	r.Months[0], r.Months[2] = r.Months[2], r.Months[0]
	first := r.Months[0].Month
	connections := r.Months[0].State.Connections[0]

	tree := periodTree(t, r)
	require.Equal(t, "2026-10", first)
	require.Equal(t, first, r.Months[0].Month)
	require.Equal(t, connections, r.Months[0].State.Connections[0])
	require.Equal(t, "2026-08", monthOf(tree, 0)["month"])
	require.Equal(t, "2026-10", monthOf(tree, 2)["month"])
	state := monthOf(tree, 0)["state"].(map[string]any)
	require.Equal(t, "mysql", state["connections"].([]any)[0].(map[string]any)["type"])
	require.Equal(t, "generate", state["jobs"].(map[string]any)["by_kind"].([]any)[0].(map[string]any)["kind"])
}

// An array nobody filled is written as an empty one, never as null.
func Test_PeriodMarshal_WritesNoNullArray(t *testing.T) {
	r := &PeriodReport{
		SchemaVersion: SchemaVersion, GeneratedAt: "2026-10-07T00:05:12Z", From: "2026-10", To: "2026-10",
		Identification: fullReport().Identification,
		Months: []MonthReport{{
			Month: "2026-10", Runs: &Runs{RowsRead: "lt_1k", RowsDiscarded: "lt_1k"},
			State: &MonthState{
				Installation:  Installation{Kind: "other", OS: "other", Arch: "other"},
				Configuration: Configuration{RunLogs: "none"},
			},
		}},
	}
	document, err := r.Marshal()
	require.NoError(t, err)
	require.NotContains(t, string(document), "null")
	require.NoError(t, ValidatePeriod(document))

	document, err = (&PeriodReport{}).Marshal()
	require.NoError(t, err)
	require.Contains(t, string(document), `"months":[]`)
}

func Test_ValidatePeriod_RefusesWhatTheSchemaDoesNotKnow(t *testing.T) {
	for name, mutate := range map[string]func(tree map[string]any){
		"a field of the period": func(tree map[string]any) { tree["job_name"] = "nightly" },
		"a field of a month":    func(tree map[string]any) { monthOf(tree, 0)["account"] = "acme" },
		"a field of the state": func(tree map[string]any) {
			monthOf(tree, 0)["state"].(map[string]any)["source_engines"] = []any{}
		},
		"a field of a block": func(tree map[string]any) {
			monthOf(tree, 0)["state"].(map[string]any)["jobs"].(map[string]any)["names"] = []any{"x"}
		},
		"the errors of a day": func(tree map[string]any) { monthOf(tree, 0)["errors"] = []any{} },
		"the day of a report": func(tree map[string]any) { tree["day"] = "2026-10-06" },
		"a state without one of its blocks": func(tree map[string]any) {
			delete(monthOf(tree, 0)["state"].(map[string]any), "unread")
		},
		"sources without a count":  func(tree map[string]any) { monthOf(tree, 0)["sources"] = map[string]any{} },
		"a month without its days": func(tree map[string]any) { delete(monthOf(tree, 1), "days_reported") },
		"a period without its key": func(tree map[string]any) { delete(tree, "identification") },
		"a period without months":  func(tree map[string]any) { tree["months"] = []any{} },
	} {
		t.Run(name, func(t *testing.T) {
			tree := periodTree(t, fullPeriod())
			mutate(tree)
			document, err := json.Marshal(tree)
			require.NoError(t, err)
			require.Error(t, ValidatePeriod(document))
		})
	}
}

func Test_ValidatePeriod_RefusesAValueOutsideOfItsList(t *testing.T) {
	for name, mutate := range map[string]func(r *PeriodReport){
		"from":            func(r *PeriodReport) { r.From = "2026-13" },
		"to":              func(r *PeriodReport) { r.To = "2026-10-07" },
		"month":           func(r *PeriodReport) { r.Months[0].Month = "August" },
		"generated_at":    func(r *PeriodReport) { r.GeneratedAt = "today" },
		"schema version":  func(r *PeriodReport) { r.SchemaVersion = 2 },
		"license state":   func(r *PeriodReport) { r.Identification.LicenseState = "bogus" },
		"license id":      func(r *PeriodReport) { r.Identification.LicenseID = "contract-42" },
		"days reported":   func(r *PeriodReport) { r.Months[0].DaysReported = 32 },
		"negative days":   func(r *PeriodReport) { r.Months[0].DaysReported = -1 },
		"husonym version": func(r *PeriodReport) { r.Months[0].Version.Husonym = "my build" },
		"negative count":  func(r *PeriodReport) { r.Months[0].Sources.Count = -1 },
		"run kind":        func(r *PeriodReport) { r.Months[2].Runs.ByStatus[0].Kind = "bogus" },
		"run status":      func(r *PeriodReport) { r.Months[2].Runs.ByStatus[0].Status = "bogus" },
		"rows read":       func(r *PeriodReport) { r.Months[2].Runs.RowsRead = "12345" },
		"gate":            func(r *PeriodReport) { r.Months[2].Refusals = []GateCount{{Gate: "bogus", Count: 1}} },
		"install kind":    func(r *PeriodReport) { r.Months[0].State.Installation.Kind = "bogus" },
		"auth provider":   func(r *PeriodReport) { r.Months[0].State.Configuration.AuthProvider = "bogus" },
		"connection type": func(r *PeriodReport) { r.Months[0].State.Connections[0].Type = "bogus" },
		"job kind":        func(r *PeriodReport) { r.Months[0].State.Jobs.ByKind[0].Kind = "bogus" },
		"transformer":     func(r *PeriodReport) { r.Months[0].State.Transformers.System[0].Name = "bogus" },
		"column family":   func(r *PeriodReport) { r.Months[0].State.ColumnTypes[0].Family = "bogus" },
		"feature":         func(r *PeriodReport) { r.Months[0].State.Features[0].Name = "bogus" },
		"role":            func(r *PeriodReport) { r.Months[0].State.Users.ByRole[0].Role = "bogus" },
	} {
		t.Run(name, func(t *testing.T) {
			// The months of fullPeriod share the rows of one report: each test alters a copy.
			var r PeriodReport
			document, err := fullPeriod().Marshal()
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(document, &r))
			mutate(&r)
			document, err = r.Marshal()
			require.NoError(t, err)
			require.Error(t, ValidatePeriod(document))
		})
	}
}

// A period holds MaxPeriodMonths months at most, and the schema says the same number.
func Test_ValidatePeriod_RefusesMoreMonthsThanAPeriodHolds(t *testing.T) {
	months := func(n int) []byte {
		r := fullPeriod()
		r.From, r.To = "2024-01", "2027-12"
		r.Months = nil
		for i := range n {
			r.Months = append(r.Months, MonthReport{Month: fmt.Sprintf("%04d-%02d", 2024+i/12, 1+i%12)})
		}
		document, err := r.Marshal()
		require.NoError(t, err)
		return document
	}
	require.Equal(t, 24, MaxPeriodMonths)
	require.NoError(t, ValidatePeriod(months(MaxPeriodMonths)))
	require.Error(t, ValidatePeriod(months(MaxPeriodMonths+1)))
}
