package report

import (
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/stretchr/testify/require"
)

func sampleReport() *Report {
	return &Report{
		Commit:  "abc1234",
		Dialect: schema.MySQL,
		Cases: []*CaseReport{
			{ID: "fixed", Priority: "P1", Title: "t", Outcomes: []*Outcome{
				{Engine: env.Benthos, Verdict: VerdictOK}, {Engine: env.Athanor, Verdict: VerdictOK},
			}},
			{ID: "known-gap", Priority: "P1", Title: "t", Outcomes: []*Outcome{
				{Engine: env.Benthos, Verdict: VerdictOK}, {Engine: env.Athanor, Verdict: VerdictGap, Gaps: 3},
			}},
		},
	}
}

func Test_Baseline_Regressions(t *testing.T) {
	r := sampleReport()

	require.Equal(t, []string{"known-gap sur athanor : écart"}, Baseline{}.Regressions(r),
		"a case missing from the baseline must be OK")

	known := Baseline{schema.MySQL: {"known-gap": {env.Athanor: VerdictGap}}}
	require.Empty(t, known.Regressions(r))

	wasOK := Baseline{schema.MySQL: {"known-gap": {env.Athanor: VerdictOK}}}
	require.Len(t, wasOK.Regressions(r), 1)

	changedNature := Baseline{schema.MySQL: {"known-gap": {env.Athanor: VerdictRunFailed}}}
	require.Empty(t, changedNature.Regressions(r))

	otherDatabase := Baseline{"postgres": {"known-gap": {env.Athanor: VerdictGap}}}
	require.Len(t, otherDatabase.Regressions(r), 1,
		"a gap known on another database says nothing about this one")
}

func Test_Baseline_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	empty, err := ReadBaseline(path)
	require.NoError(t, err)
	require.Empty(t, empty)

	kept := Baseline{
		schema.MySQL: {"other-case": {env.Benthos: VerdictGap}},
		"postgres":   {"known-gap": {env.Athanor: VerdictRunFailed}},
	}
	kept.Merge(sampleReport())
	require.NoError(t, kept.Write(path))

	read, err := ReadBaseline(path)
	require.NoError(t, err)
	require.Equal(t, VerdictGap, read[schema.MySQL]["other-case"][env.Benthos],
		"cases the report did not run are kept")
	require.Equal(t, VerdictGap, read[schema.MySQL]["known-gap"][env.Athanor])
	require.Equal(t, VerdictOK, read[schema.MySQL]["fixed"][env.Athanor])
	require.Equal(t, VerdictRunFailed, read["postgres"]["known-gap"][env.Athanor],
		"the databases the report does not concern are kept")
}

func Test_Report_Markdown(t *testing.T) {
	md := sampleReport().Markdown()
	require.Contains(t, md, "| `known-gap` | P1 | OK | écart (3) |")
	require.Contains(t, md, "### `known-gap` (P1)")
	require.NotContains(t, md, "### `fixed`")
}
