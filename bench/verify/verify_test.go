package verify

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/oracle"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/stretchr/testify/require"
)

func commandeTable() *schema.Table {
	return &schema.Table{
		Name: "COMMANDE",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Int64()},
			{Name: "groupe_id", Type: schema.Int64(), Nullable: true},
			{Name: "email", Type: schema.Varchar(40)},
		},
		PrimaryKey: []string{"id"},
	}
}

func Test_compareTable(t *testing.T) {
	expected := map[string]*oracle.ExpectedRow{
		"1": {Key: "1", Occurrences: 1, Verdict: cases.VerdictKept},
		"2": {Key: "2", Occurrences: 1, Verdict: cases.VerdictKept, NullColumns: []string{"groupe_id"}},
		"3": {Key: "3", Occurrences: 1, Verdict: cases.VerdictKept},
		"9": {Key: "9", Occurrences: 1, Verdict: cases.VerdictDropped},
	}
	source := map[string][]row{
		"1": {{"1", `\N`, "a@source"}},
		"2": {{"2", "9", "b@source"}},
		"3": {{"3", `\N`, "c@source"}},
		"9": {{"9", `\N`, "d@source"}},
	}
	rules := map[string][]cases.Rule{
		"id":        {cases.RuleUnchanged},
		"groupe_id": {cases.RuleUnchanged},
		"email":     {cases.RuleNotInSourceSet, cases.RuleUnique},
	}

	t.Run("a destination matching the expectation has no gap", func(t *testing.T) {
		dest := map[string][]row{
			"1": {{"1", `\N`, "x@dest"}},
			"2": {{"2", `\N`, "y@dest"}},
			"3": {{"3", `\N`, "z@dest"}},
		}
		result, err := compareTable(commandeTable(), expected, rules, source, dest)
		require.NoError(t, err)
		require.Zero(t, result.Gaps())
		require.Equal(t, 3, result.ExpectedRows)
		require.Equal(t, 3, result.DestinationRows)
	})

	t.Run("every kind of gap is counted", func(t *testing.T) {
		dest := map[string][]row{
			"1":  {{"1", `\N`, "x@dest"}, {"1", `\N`, "x@dest"}}, // duplicated, email not unique
			"2":  {{"2", "9", "b@source"}},                       // orphan kept, email copied
			"9":  {{"9", `\N`, "w@dest"}},                        // leaked
			"42": {{"42", `\N`, "v@dest"}},                       // no source row
		}
		result, err := compareTable(commandeTable(), expected, rules, source, dest)
		require.NoError(t, err)
		require.Equal(t, 1, result.Missing, "row 3")
		require.Equal(t, 1, result.Leaked)
		require.Equal(t, 1, result.Unexpected)
		require.Equal(t, 1, result.Duplicated)
		require.Equal(t, map[string]int{
			"groupe_id: unchanged":     1,
			"email: not_in_source_set": 1,
			"email: unique":            1,
		}, result.RuleViolations)
		require.Equal(t, 7, result.Gaps())
	})

	t.Run("a rule the bench does not verify is an error", func(t *testing.T) {
		_, err := compareTable(commandeTable(), expected,
			map[string][]cases.Rule{"email": {"stable_across_runs"}}, source, map[string][]row{})
		require.Error(t, err)
	})
}
