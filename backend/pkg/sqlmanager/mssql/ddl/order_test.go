package ddl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func module(id int64, schema, name string) *Module {
	return &Module{ObjectID: id, Schema: schema, Name: name, Type: TypeView, HasDefinition: true}
}

func names(modules []*Module) []string {
	out := make([]string, len(modules))
	for i, m := range modules {
		out[i] = m.Schema + "." + m.Name
	}
	return out
}

func Test_orderModules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		modules    []*Module
		references map[int64][]int64
		expected   []string
	}{
		{
			name:     "empty",
			expected: []string{},
		},
		{
			name:     "single",
			modules:  []*Module{module(1, "dbo", "a")},
			expected: []string{"dbo.a"},
		},
		{
			name:       "a chain given in reverse",
			modules:    []*Module{module(1, "dbo", "a"), module(2, "dbo", "b"), module(3, "dbo", "c")},
			references: map[int64][]int64{1: {2}, 2: {3}},
			expected:   []string{"dbo.c", "dbo.b", "dbo.a"},
		},
		{
			name:     "independent modules come by schema then name",
			modules:  []*Module{module(1, "s2", "a"), module(2, "s1", "z"), module(3, "s1", "B"), module(4, "s1", "a")},
			expected: []string{"s1.B", "s1.a", "s1.z", "s2.a"},
		},
		{
			name:       "a cycle comes last, by name",
			modules:    []*Module{module(1, "dbo", "p2"), module(2, "dbo", "p1"), module(3, "dbo", "z")},
			references: map[int64][]int64{1: {2}, 2: {1}},
			expected:   []string{"dbo.z", "dbo.p1", "dbo.p2"},
		},
		{
			name:       "a module that references itself",
			modules:    []*Module{module(1, "dbo", "a")},
			references: map[int64][]int64{1: {1}},
			expected:   []string{"dbo.a"},
		},
		{
			name: "a diamond",
			modules: []*Module{
				module(1, "dbo", "apex"), module(2, "dbo", "left"), module(3, "dbo", "right"), module(4, "dbo", "root"),
			},
			references: map[int64][]int64{1: {2, 3}, 2: {4}, 3: {4}},
			expected:   []string{"dbo.root", "dbo.left", "dbo.right", "dbo.apex"},
		},
		{
			name:       "a dependency outside the set",
			modules:    []*Module{module(1, "dbo", "a"), module(2, "dbo", "b")},
			references: map[int64][]int64{1: {99}, 2: {1}},
			expected:   []string{"dbo.a", "dbo.b"},
		},
		{
			name:       "across schemas",
			modules:    []*Module{module(1, "a", "v"), module(2, "z", "f")},
			references: map[int64][]int64{1: {2}},
			expected:   []string{"z.f", "a.v"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, names(orderModules(tc.modules, tc.references)))
		})
	}
}
