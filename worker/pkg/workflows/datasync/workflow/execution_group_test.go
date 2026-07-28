package datasync_workflow

import (
	"testing"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	runconfigs "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildExecutionGroups_NoCycles(t *testing.T) {
	configs := []*benthosbuilder.BenthosConfigResponse{
		{
			Name:        "public.users.insert",
			TableSchema: "public",
			TableName:   "users",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		{
			Name:        "public.orders.insert",
			TableSchema: "public",
			TableName:   "orders",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.users", Columns: []string{"id"}},
			},
		},
	}

	groups := buildExecutionGroups(configs)

	// Should create 2 independent table groups
	assert.Equal(t, 2, len(groups))

	// Find users group
	var usersGroup *ExecutionGroup
	for _, g := range groups {
		if g.ID == "table:public.users" {
			usersGroup = g
			break
		}
	}
	require.NotNil(t, usersGroup)
	assert.False(t, usersGroup.IsInCycle)
	assert.Equal(t, 1, len(usersGroup.InsertConfigs))
	assert.Equal(t, 0, len(usersGroup.UpdateConfigs))
	assert.Equal(t, 0, len(usersGroup.DependsOnGroups))

	// Find orders group
	var ordersGroup *ExecutionGroup
	for _, g := range groups {
		if g.ID == "table:public.orders" {
			ordersGroup = g
			break
		}
	}
	require.NotNil(t, ordersGroup)
	assert.False(t, ordersGroup.IsInCycle)
	assert.Equal(t, 1, len(ordersGroup.InsertConfigs))
	assert.Equal(t, 0, len(ordersGroup.UpdateConfigs))
	assert.Equal(t, 1, len(ordersGroup.DependsOnGroups))
	assert.Contains(t, ordersGroup.DependsOnGroups, "table:public.users")
}

func TestBuildExecutionGroups_SimpleCycle(t *testing.T) {
	configs := []*benthosbuilder.BenthosConfigResponse{
		{
			Name:        "public.store_customers.insert",
			TableSchema: "public",
			TableName:   "store_customers",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		{
			Name:        "public.store_customers.update.1",
			TableSchema: "public",
			TableName:   "store_customers",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.referral_codes", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.referral_codes.insert",
			TableSchema: "public",
			TableName:   "referral_codes",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.store_customers", Columns: []string{"id"}},
			},
		},
	}

	groups := buildExecutionGroups(configs)

	// Should create 1 cycle group
	assert.Equal(t, 1, len(groups))

	cycleGroup := groups[0]
	assert.True(t, cycleGroup.IsInCycle)
	assert.Equal(t, 2, len(cycleGroup.Tables))
	assert.Contains(t, cycleGroup.Tables, "public.store_customers")
	assert.Contains(t, cycleGroup.Tables, "public.referral_codes")

	// Should have 2 INSERTs and 1 UPDATE
	assert.Equal(t, 2, len(cycleGroup.InsertConfigs))
	assert.Equal(t, 1, len(cycleGroup.UpdateConfigs))

	// Cycle should have no external dependencies
	assert.Equal(t, 0, len(cycleGroup.DependsOnGroups))
}

func TestBuildExecutionGroups_ThreeTableCycle(t *testing.T) {
	configs := []*benthosbuilder.BenthosConfigResponse{
		{
			Name:        "public.addresses.insert",
			TableSchema: "public",
			TableName:   "addresses",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		{
			Name:        "public.addresses.update.1",
			TableSchema: "public",
			TableName:   "addresses",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.orders", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.customers.insert",
			TableSchema: "public",
			TableName:   "customers",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.addresses", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.customers.update.1",
			TableSchema: "public",
			TableName:   "customers",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.addresses", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.orders.insert",
			TableSchema: "public",
			TableName:   "orders",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.customers", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.orders.update.1",
			TableSchema: "public",
			TableName:   "orders",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.customers", Columns: []string{"id"}},
			},
		},
	}

	groups := buildExecutionGroups(configs)

	// Should create 1 cycle group
	assert.Equal(t, 1, len(groups))

	cycleGroup := groups[0]
	assert.True(t, cycleGroup.IsInCycle)
	assert.Equal(t, 3, len(cycleGroup.Tables))
	assert.Contains(t, cycleGroup.Tables, "public.addresses")
	assert.Contains(t, cycleGroup.Tables, "public.customers")
	assert.Contains(t, cycleGroup.Tables, "public.orders")

	// Should have 3 INSERTs and 3 UPDATEs
	assert.Equal(t, 3, len(cycleGroup.InsertConfigs))
	assert.Equal(t, 3, len(cycleGroup.UpdateConfigs))
}

func TestBuildExecutionGroups_CycleWithExternalDependency(t *testing.T) {
	configs := []*benthosbuilder.BenthosConfigResponse{
		// Independent table
		{
			Name:        "public.regions.insert",
			TableSchema: "public",
			TableName:   "regions",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		// Cycle
		{
			Name:        "public.store_customers.insert",
			TableSchema: "public",
			TableName:   "store_customers",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.regions", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.store_customers.update.1",
			TableSchema: "public",
			TableName:   "store_customers",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.referral_codes", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.referral_codes.insert",
			TableSchema: "public",
			TableName:   "referral_codes",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.store_customers", Columns: []string{"id"}},
			},
		},
	}

	groups := buildExecutionGroups(configs)

	// Should create 2 groups: 1 for regions, 1 for cycle
	assert.Equal(t, 2, len(groups))

	// Find cycle group
	var cycleGroup *ExecutionGroup
	for _, g := range groups {
		if g.IsInCycle {
			cycleGroup = g
			break
		}
	}
	require.NotNil(t, cycleGroup)

	// Cycle should depend on regions group
	assert.Equal(t, 1, len(cycleGroup.DependsOnGroups))
	assert.Contains(t, cycleGroup.DependsOnGroups, "table:public.regions")
}

func TestMergeCyclesWithSharedTables_NoSharedTables(t *testing.T) {
	cycles := [][]string{
		{"public.users", "public.posts"},
		{"public.categories", "public.tags"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should remain separate since no tables are shared
	assert.Equal(t, 2, len(result))
}

func TestMergeCyclesWithSharedTables_SharedTable(t *testing.T) {
	cycles := [][]string{
		{"public.astronaut", "public.mission"},
		{"public.spacecraft", "public.mission"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should be merged into one cycle since they share "public.mission"
	assert.Equal(t, 1, len(result))
	assert.Equal(t, 3, len(result[0]))
	assert.Contains(t, result[0], "public.astronaut")
	assert.Contains(t, result[0], "public.mission")
	assert.Contains(t, result[0], "public.spacecraft")
}

func TestMergeCyclesWithSharedTables_TransitiveMerge(t *testing.T) {
	// A-B, B-C, C-D should all merge into one since they're transitively connected
	cycles := [][]string{
		{"A", "B"},
		{"B", "C"},
		{"C", "D"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should be merged into one cycle
	assert.Equal(t, 1, len(result))
	assert.Equal(t, 4, len(result[0]))
	assert.Contains(t, result[0], "A")
	assert.Contains(t, result[0], "B")
	assert.Contains(t, result[0], "C")
	assert.Contains(t, result[0], "D")
}

func TestMergeCyclesWithSharedTables_PartialMerge(t *testing.T) {
	// A-B and B-C should merge, but D-E should stay separate
	cycles := [][]string{
		{"A", "B"},
		{"B", "C"},
		{"D", "E"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should result in 2 cycles: one merged (A-B-C) and one separate (D-E)
	assert.Equal(t, 2, len(result))

	// Find the merged cycle
	var mergedCycle []string
	var separateCycle []string
	for _, cycle := range result {
		if len(cycle) == 3 {
			mergedCycle = cycle
		} else if len(cycle) == 2 {
			separateCycle = cycle
		}
	}

	require.NotNil(t, mergedCycle)
	require.NotNil(t, separateCycle)

	assert.Contains(t, mergedCycle, "A")
	assert.Contains(t, mergedCycle, "B")
	assert.Contains(t, mergedCycle, "C")

	assert.Contains(t, separateCycle, "D")
	assert.Contains(t, separateCycle, "E")
}

func TestMergeCyclesWithSharedTables_EmptyInput(t *testing.T) {
	result := mergeCyclesWithSharedTables([][]string{})
	assert.Equal(t, 0, len(result))
}

func TestMergeCyclesWithSharedTables_SingleCycle(t *testing.T) {
	cycles := [][]string{
		{"A", "B", "C"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	assert.Equal(t, 1, len(result))
	assert.Equal(t, 3, len(result[0]))
}

func TestMergeCyclesWithSharedTables_FourTableTransitiveMerge(t *testing.T) {
	// A-B, B-C, C-D should all merge into one since they're transitively connected
	cycles := [][]string{
		{"A", "B"},
		{"B", "C"},
		{"C", "D"},
		{"D", "E"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should be merged into one cycle
	assert.Equal(t, 1, len(result))
	assert.Equal(t, 5, len(result[0]))
	assert.Contains(t, result[0], "A")
	assert.Contains(t, result[0], "B")
	assert.Contains(t, result[0], "C")
	assert.Contains(t, result[0], "D")
	assert.Contains(t, result[0], "E")
}

func TestMergeCyclesWithSharedTables_MultipleSeparateMerges(t *testing.T) {
	// A-B-C should merge, D-E-F should merge, G-H should merge separately
	cycles := [][]string{
		{"A", "B"},
		{"B", "C"},
		{"D", "E"},
		{"E", "F"},
		{"G", "H"},
	}

	result := mergeCyclesWithSharedTables(cycles)

	// Should result in 3 cycles
	assert.Equal(t, 3, len(result))

	// Find each merged cycle
	var foundABC, foundDEF, foundGH bool
	for _, cycle := range result {
		if len(cycle) == 3 {
			if contains(cycle, "A") && contains(cycle, "B") && contains(cycle, "C") {
				foundABC = true
			} else if contains(cycle, "D") && contains(cycle, "E") && contains(cycle, "F") {
				foundDEF = true
			}
		} else if len(cycle) == 2 {
			if contains(cycle, "G") && contains(cycle, "H") {
				foundGH = true
			}
		}
	}

	assert.True(t, foundABC, "Should find merged A-B-C cycle")
	assert.True(t, foundDEF, "Should find merged D-E-F cycle")
	assert.True(t, foundGH, "Should find G-H cycle")
}

// TestBuildExecutionGroups_MergedCyclesWithExternalDependent reproduces the
// complex-schema scenario: two cycles that share a table (astronaut<->mission
// and mission<->spacecraft, merged into one group) plus an external table
// (research_project) that depends on a table inside the merged cycle.
//
// The merged cycle group must expose ALL merged tables in its Tables field so
// that the external dependent correctly depends on the cycle group. We loop to
// flush out any dependence on Go map iteration order.
func TestBuildExecutionGroups_MergedCyclesWithExternalDependent(t *testing.T) {
	configs := []*benthosbuilder.BenthosConfigResponse{
		// Cycle astronaut <-> mission
		{
			Name:        "public.astronaut.insert",
			TableSchema: "public",
			TableName:   "astronaut",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		{
			Name:        "public.astronaut.update.1",
			TableSchema: "public",
			TableName:   "astronaut",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.mission", Columns: []string{"id"}},
			},
		},
		{
			Name:        "public.mission.insert",
			TableSchema: "public",
			TableName:   "mission",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.astronaut", Columns: []string{"id"}},
				{Table: "public.spacecraft", Columns: []string{"id"}},
			},
		},
		// Cycle mission <-> spacecraft (shares "mission" with the cycle above)
		{
			Name:        "public.spacecraft.insert",
			TableSchema: "public",
			TableName:   "spacecraft",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn:   []*runconfigs.DependsOn{},
		},
		{
			Name:        "public.spacecraft.update.1",
			TableSchema: "public",
			TableName:   "spacecraft",
			RunType:     runconfigs.RunTypeUpdate,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.mission", Columns: []string{"id"}},
			},
		},
		// External dependent: research_project -> astronaut (inside the cycle)
		{
			Name:        "public.research_project.insert",
			TableSchema: "public",
			TableName:   "research_project",
			RunType:     runconfigs.RunTypeInsert,
			DependsOn: []*runconfigs.DependsOn{
				{Table: "public.astronaut", Columns: []string{"id"}},
			},
		},
	}

	// Loop to defeat per-call map-iteration randomness.
	for i := 0; i < 200; i++ {
		groups := buildExecutionGroups(configs)

		var cycleGroup, projectGroup *ExecutionGroup
		for _, g := range groups {
			if g.IsInCycle {
				cycleGroup = g
			}
			if g.ID == "table:public.research_project" {
				projectGroup = g
			}
		}
		require.NotNil(t, cycleGroup, "iter %d: expected a cycle group", i)
		require.NotNil(t, projectGroup, "iter %d: expected research_project group", i)

		// The merged cycle must contain all three tables.
		assert.ElementsMatch(t,
			[]string{"public.astronaut", "public.mission", "public.spacecraft"},
			cycleGroup.Tables,
			"iter %d: merged cycle group missing tables", i)

		// research_project depends on astronaut, which lives in the cycle group,
		// so its group must depend on the cycle group — otherwise it would sync
		// before astronaut exists and hit a foreign-key violation.
		assert.Contains(t, projectGroup.DependsOnGroups, cycleGroup.ID,
			"iter %d: research_project group must depend on the cycle group", i)
	}
}

func TestBuildTableName(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		expected string
	}{
		{
			name:     "with schema",
			schema:   "public",
			table:    "users",
			expected: "public.users",
		},
		{
			name:     "without schema",
			schema:   "",
			table:    "users",
			expected: "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTableName(tt.schema, tt.table)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function for testing
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
