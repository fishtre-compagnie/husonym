package report

import (
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/stretchr/testify/require"
)

func sampleReport() *Report {
	return &Report{
		Commit: "abc1234",
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

	known := Baseline{"known-gap": {env.Athanor: VerdictGap}}
	require.Empty(t, known.Regressions(r))

	wasOK := Baseline{"known-gap": {env.Athanor: VerdictOK}}
	require.Len(t, wasOK.Regressions(r), 1)

	changedNature := Baseline{"known-gap": {env.Athanor: VerdictRunFailed}}
	require.Empty(t, changedNature.Regressions(r))
}

func Test_Baseline_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	empty, err := ReadBaseline(path)
	require.NoError(t, err)
	require.Empty(t, empty)

	kept := Baseline{"other-case": {env.Benthos: VerdictGap}}
	kept.Merge(sampleReport())
	require.NoError(t, kept.Write(path))

	read, err := ReadBaseline(path)
	require.NoError(t, err)
	require.Equal(t, VerdictGap, read["other-case"][env.Benthos], "cases the report did not run are kept")
	require.Equal(t, VerdictGap, read["known-gap"][env.Athanor])
	require.Equal(t, VerdictOK, read["fixed"][env.Athanor])
}

func Test_Report_Markdown(t *testing.T) {
	md := sampleReport().Markdown()
	require.Contains(t, md, "| `known-gap` | P1 | OK | écart (3) |")
	require.Contains(t, md, "### `known-gap` (P1)")
	require.NotContains(t, md, "### `fixed`")
}
