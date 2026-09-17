package cases

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	t      *testing.T
	c      *Case
	counts map[string]int
}

func (r *recorder) Row(table string, values []any, expect RowExpect) {
	tbl := r.c.Table(table)
	require.NotNil(r.t, tbl, "%s: unknown table %s", r.c.ID, table)
	require.Len(r.t, values, len(tbl.Columns), "%s: %s", r.c.ID, table)
	for _, column := range expect.NullColumns {
		col := tbl.Column(column)
		require.NotNil(r.t, col, "%s: %s.%s", r.c.ID, table, column)
		require.True(r.t, col.Nullable, "%s: %s.%s must be nullable", r.c.ID, table, column)
	}
	r.counts[table]++
}

func Test_All(t *testing.T) {
	ids := map[string]bool{}
	for _, c := range All() {
		require.NoError(t, c.Validate())
		require.False(t, ids[c.ID], "duplicate case id %s", c.ID)
		ids[c.ID] = true
		require.LessOrEqual(t, len(c.Database()), 64, "%s: database name too long for MySQL", c.ID)

		rec := &recorder{t: t, c: c, counts: map[string]int{}}
		c.Seed(Params{PageLimit: 100, Scale: 1}, rec)
		total := 0
		for _, n := range rec.counts {
			total += n
		}
		require.Positive(t, total, "%s: the seed emits no row", c.ID)
	}
}

func Test_Case_Validate(t *testing.T) {
	valid := func() *Case {
		return &Case{
			ID: "c", Priority: P1, Title: "t",
			Tables: []*schema.Table{{
				Name:       "T",
				Columns:    []schema.Column{{Name: "id", Type: schema.Int64()}},
				PrimaryKey: []string{"id"},
			}},
			Seed: func(Params, Emitter) {},
		}
	}
	require.NoError(t, valid().Validate())

	unknownParent := valid()
	unknownParent.Tables[0].ForeignKeys = []schema.ForeignKey{
		{Name: "fk", Columns: []string{"id"}, RefTable: "MISSING", RefColumns: []string{"id"}},
	}
	require.Error(t, unknownParent.Validate())

	unknownWhere := valid()
	unknownWhere.Job.Where = map[string]string{"MISSING": "id = 1"}
	require.Error(t, unknownWhere.Validate())

	unknownSpec := valid()
	unknownSpec.Job.Columns = map[string]map[string]ColumnSpec{"T": {"missing": {}}}
	require.Error(t, unknownSpec.Validate())
}

func Test_Case_IdentityColumns(t *testing.T) {
	c := &Case{
		Tables: []*schema.Table{
			{Name: "KEYED", Columns: []schema.Column{{Name: "id"}, {Name: "v"}}, PrimaryKey: []string{"id"}},
			{Name: "KEYLESS", Columns: []schema.Column{{Name: "a"}, {Name: "b"}}},
		},
		Job: Job{Columns: map[string]map[string]ColumnSpec{"KEYLESS": {"b": {Rules: []Rule{RuleUnique}}}}},
	}
	require.Equal(t, []string{"id"}, c.IdentityColumns("KEYED"))
	require.Equal(t, []string{"a"}, c.IdentityColumns("KEYLESS"), "transformed columns cannot identify a row")

	c.Identity = map[string][]string{"KEYED": {"v"}}
	require.Equal(t, []string{"v"}, c.IdentityColumns("KEYED"))
}
