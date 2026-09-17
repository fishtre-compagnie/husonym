package tableplan

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/require"
)

func TestTablePlan_RoundTrip(t *testing.T) {
	in := &TablePlan{
		Id:             "public.users.insert",
		Schema:         "public",
		Table:          "users",
		RunType:        runconfigs.RunTypeInsert,
		Query:          "SELECT id, email FROM users LIMIT 100",
		PageQuery:      "SELECT id, email FROM users WHERE id > ? LIMIT ?",
		PageLimit:      100,
		OrderByColumns: []string{"id"},
		Columns:        []string{"id", "email"},
	}
	data, err := in.Marshal()
	require.NoError(t, err)

	out, err := Unmarshal(data)
	require.NoError(t, err)
	require.Equal(t, in, out)
	require.True(t, out.IsPaged())
}

func TestTablePlan_IncompleteIsRejected(t *testing.T) {
	_, err := Unmarshal([]byte(`{"id":"public.users.insert","table":"users"}`))
	require.Error(t, err)
}

func TestTablePlan_NotPagedWithoutOrder(t *testing.T) {
	p := &TablePlan{PageQuery: "SELECT 1", PageLimit: 10}
	require.False(t, p.IsPaged())
}
