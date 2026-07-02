package datasync_workflow

import (
	"testing"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	runconfigs "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGroupCompletionTracker(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.users.insert"},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.insert"},
				{Name: "public.referral_codes.insert"},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.update.1"},
			},
			DependsOnGroups: []string{"table:public.users"},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Verify tracker initialized correctly
	assert.NotNil(t, tracker)
	assert.Equal(t, 2, len(tracker.groups))
	assert.Equal(t, 4, len(tracker.configToGroup)) // 4 total configs

	// Check users group state
	usersState := tracker.groups["table:public.users"]
	require.NotNil(t, usersState)
	assert.Equal(t, 1, usersState.TotalConfigs)
	assert.Equal(t, 0, usersState.CompletedCount)
	assert.True(t, usersState.InsertPhaseComplete == false) // Has inserts, not complete yet
	assert.True(t, usersState.UpdatePhaseComplete)          // No updates, so complete by default

	// Check cycle group state
	cycleState := tracker.groups["cycle:public.store_customers_public.referral_codes"]
	require.NotNil(t, cycleState)
	assert.Equal(t, 3, cycleState.TotalConfigs)
	assert.Equal(t, 0, cycleState.CompletedCount)
	assert.False(t, cycleState.InsertPhaseComplete)
	assert.False(t, cycleState.UpdatePhaseComplete)
}

func TestMarkConfigComplete_SimpleFlow(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.users.insert"},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Mark config complete
	err := tracker.MarkConfigComplete("public.users.insert")
	require.NoError(t, err)

	// Check state
	state := tracker.groups["table:public.users"]
	assert.Equal(t, 1, state.CompletedCount)
	assert.True(t, state.InsertPhaseComplete)
	assert.True(t, tracker.IsGroupComplete("table:public.users"))
}

func TestMarkConfigComplete_PhaseTransition(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.insert"},
				{Name: "public.referral_codes.insert"},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.update.1"},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	groupID := "cycle:public.store_customers_public.referral_codes"

	// Initially INSERT phase not complete
	assert.False(t, tracker.IsInsertPhaseComplete(groupID))

	// Complete first INSERT
	err := tracker.MarkConfigComplete("public.store_customers.insert")
	require.NoError(t, err)
	assert.False(t, tracker.IsInsertPhaseComplete(groupID)) // Still one more INSERT

	// Complete second INSERT - should trigger INSERT phase completion
	err = tracker.MarkConfigComplete("public.referral_codes.insert")
	require.NoError(t, err)
	assert.True(t, tracker.IsInsertPhaseComplete(groupID))

	// Complete UPDATE - should trigger full group completion
	err = tracker.MarkConfigComplete("public.store_customers.update.1")
	require.NoError(t, err)
	assert.True(t, tracker.IsGroupComplete(groupID))
}

func TestMarkConfigComplete_Duplicate(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.users.insert"},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Mark complete once
	err := tracker.MarkConfigComplete("public.users.insert")
	require.NoError(t, err)
	state := tracker.groups["table:public.users"]
	assert.Equal(t, 1, state.CompletedCount)

	// Mark complete again - should be idempotent
	err = tracker.MarkConfigComplete("public.users.insert")
	require.NoError(t, err)
	assert.Equal(t, 1, state.CompletedCount) // Count should not increase
}

func TestMarkConfigComplete_UnknownConfig(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:            "table:public.users",
			Tables:        []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{},
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Try to mark unknown config
	err := tracker.MarkConfigComplete("public.unknown.insert")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config not found in any group")
}

func TestCanConfigStart_SimpleIndependentTable(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.users.insert",
					TableSchema: "public",
					TableName:   "users",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn:   []*runconfigs.DependsOn{},
				},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	cfg := groups[0].InsertConfigs[0]

	// Should be able to start immediately (no dependencies)
	assert.True(t, tracker.CanConfigStart(cfg))
}

func TestCanConfigStart_InterGroupDependency(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.users.insert",
					TableSchema: "public",
					TableName:   "users",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn:   []*runconfigs.DependsOn{},
				},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
		{
			ID:     "table:public.orders",
			Tables: []string{"public.orders"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.orders.insert",
					TableSchema: "public",
					TableName:   "orders",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.users", Columns: []string{"id"}},
					},
				},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{"table:public.users"},
			IsInCycle:       false,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	ordersConfig := groups[1].InsertConfigs[0]

	// Orders should be blocked until users completes
	assert.False(t, tracker.CanConfigStart(ordersConfig))

	// Complete users
	err := tracker.MarkConfigComplete("public.users.insert")
	require.NoError(t, err)

	// Now orders should be able to start
	assert.True(t, tracker.CanConfigStart(ordersConfig))
}

func TestCanConfigStart_UpdateWaitsForInsertPhase(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.store_customers.insert",
					TableSchema: "public",
					TableName:   "store_customers",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn:   []*runconfigs.DependsOn{},
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
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.store_customers.update.1",
					TableSchema: "public",
					TableName:   "store_customers",
					RunType:     runconfigs.RunTypeUpdate,
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.referral_codes", Columns: []string{"id"}},
					},
				},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	updateConfig := groups[0].UpdateConfigs[0]

	// UPDATE should be blocked until INSERT phase completes
	assert.False(t, tracker.CanConfigStart(updateConfig))

	// Complete first INSERT
	err := tracker.MarkConfigComplete("public.store_customers.insert")
	require.NoError(t, err)
	assert.False(t, tracker.CanConfigStart(updateConfig)) // Still blocked

	// Complete second INSERT - INSERT phase now complete
	err = tracker.MarkConfigComplete("public.referral_codes.insert")
	require.NoError(t, err)

	// Now UPDATE should be able to start
	assert.True(t, tracker.CanConfigStart(updateConfig))
}

func TestCanConfigStart_IntraGroupDependency(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.store_customers.insert",
					TableSchema: "public",
					TableName:   "store_customers",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn:   []*runconfigs.DependsOn{},
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
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	storeCustomersConfig := groups[0].InsertConfigs[0]
	referralCodesConfig := groups[0].InsertConfigs[1]

	// store_customers has no dependencies, should start
	assert.True(t, tracker.CanConfigStart(storeCustomersConfig))

	// referral_codes depends on store_customers (intra-group), should be blocked
	assert.False(t, tracker.CanConfigStart(referralCodesConfig))

	// Complete store_customers
	err := tracker.MarkConfigComplete("public.store_customers.insert")
	require.NoError(t, err)

	// Now referral_codes should be able to start
	assert.True(t, tracker.CanConfigStart(referralCodesConfig))
}

func TestCanConfigStart_CycleWithExternalDependency(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.regions",
			Tables: []string{"public.regions"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.regions.insert",
					TableSchema: "public",
					TableName:   "regions",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn:   []*runconfigs.DependsOn{},
				},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
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
					Name:        "public.referral_codes.insert",
					TableSchema: "public",
					TableName:   "referral_codes",
					RunType:     runconfigs.RunTypeInsert,
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.store_customers", Columns: []string{"id"}},
					},
				},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.store_customers.update.1",
					TableSchema: "public",
					TableName:   "store_customers",
					RunType:     runconfigs.RunTypeUpdate,
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.referral_codes", Columns: []string{"id"}},
					},
				},
			},
			DependsOnGroups: []string{"table:public.regions"},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	storeCustomersConfig := groups[1].InsertConfigs[0]

	// store_customers should be blocked by external dependency (regions)
	assert.False(t, tracker.CanConfigStart(storeCustomersConfig))

	// Complete regions
	err := tracker.MarkConfigComplete("public.regions.insert")
	require.NoError(t, err)

	// Now store_customers should be able to start
	assert.True(t, tracker.CanConfigStart(storeCustomersConfig))
}

func TestIsGroupComplete(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.users.insert"},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Initially not complete
	assert.False(t, tracker.IsGroupComplete("table:public.users"))

	// Mark complete
	err := tracker.MarkConfigComplete("public.users.insert")
	require.NoError(t, err)

	// Now complete
	assert.True(t, tracker.IsGroupComplete("table:public.users"))

	// Unknown group
	assert.False(t, tracker.IsGroupComplete("table:public.unknown"))
}

func TestIsInsertPhaseComplete(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.insert"},
				{Name: "public.referral_codes.insert"},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.update.1"},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)
	groupID := "cycle:public.store_customers_public.referral_codes"

	// Initially not complete
	assert.False(t, tracker.IsInsertPhaseComplete(groupID))

	// Complete first INSERT
	err := tracker.MarkConfigComplete("public.store_customers.insert")
	require.NoError(t, err)
	assert.False(t, tracker.IsInsertPhaseComplete(groupID))

	// Complete second INSERT
	err = tracker.MarkConfigComplete("public.referral_codes.insert")
	require.NoError(t, err)
	assert.True(t, tracker.IsInsertPhaseComplete(groupID))

	// Unknown group
	assert.False(t, tracker.IsInsertPhaseComplete("unknown"))
}

func TestGetCompletionStatus(t *testing.T) {
	groups := []*ExecutionGroup{
		{
			ID:     "table:public.users",
			Tables: []string{"public.users"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.users.insert"},
			},
			UpdateConfigs:   []*benthosbuilder.BenthosConfigResponse{},
			DependsOnGroups: []string{},
			IsInCycle:       false,
		},
		{
			ID:     "cycle:public.store_customers_public.referral_codes",
			Tables: []string{"public.store_customers", "public.referral_codes"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.insert"},
				{Name: "public.referral_codes.insert"},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{Name: "public.store_customers.update.1"},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Initial status
	status := tracker.GetCompletionStatus()
	assert.Equal(t, 2, len(status))
	assert.Contains(t, status["table:public.users"], "0/1")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "0/3")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "[INSERT:0/2]")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "[UPDATE:0/1]")

	// Complete one INSERT in cycle
	err := tracker.MarkConfigComplete("public.store_customers.insert")
	require.NoError(t, err)

	status = tracker.GetCompletionStatus()
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "1/3")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "[INSERT:1/2]")

	// Complete second INSERT in cycle
	err = tracker.MarkConfigComplete("public.referral_codes.insert")
	require.NoError(t, err)

	status = tracker.GetCompletionStatus()
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "2/3")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "[INSERT:DONE]")

	// Complete UPDATE
	err = tracker.MarkConfigComplete("public.store_customers.update.1")
	require.NoError(t, err)

	status = tracker.GetCompletionStatus()
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "3/3")
	assert.Contains(t, status["cycle:public.store_customers_public.referral_codes"], "[UPDATE:DONE]")
}

func TestFindConfigByTable(t *testing.T) {
	group := &ExecutionGroup{
		ID:     "test",
		Tables: []string{"public.users", "public.orders"},
		InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
			{
				Name:        "public.users.insert",
				TableSchema: "public",
				TableName:   "users",
			},
			{
				Name:        "public.orders.insert",
				TableSchema: "public",
				TableName:   "orders",
			},
		},
		UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
			{
				Name:        "public.users.update.1",
				TableSchema: "public",
				TableName:   "users",
			},
		},
	}

	// Find in INSERT configs
	assert.Equal(t, "public.users.insert", findConfigByTable(group, "public.users"))
	assert.Equal(t, "public.orders.insert", findConfigByTable(group, "public.orders"))

	// Table with no schema
	groupNoSchema := &ExecutionGroup{
		InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
			{
				Name:        "users.insert",
				TableSchema: "",
				TableName:   "users",
			},
		},
	}
	assert.Equal(t, "users.insert", findConfigByTable(groupNoSchema, "users"))

	// Not found
	assert.Equal(t, "", findConfigByTable(group, "public.unknown"))
}

func TestCanConfigStart_IntraGroupDependencies(t *testing.T) {
	// Test that configs with intra-group dependencies wait for their dependencies
	// This mimics the mysql/complex scenario where mission.insert depends on spacecraft.insert
	// within the same cycle group
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:complex.astronaut_complex.mission_complex.spacecraft",
			Tables: []string{"complex.astronaut", "complex.mission", "complex.spacecraft"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "complex.astronaut.insert",
					TableSchema: "complex",
					TableName:   "astronaut",
					RunType:     "insert",
					DependsOn:   []*runconfigs.DependsOn{},
				},
				{
					Name:        "complex.spacecraft.insert",
					TableSchema: "complex",
					TableName:   "spacecraft",
					RunType:     "insert",
					DependsOn:   []*runconfigs.DependsOn{},
				},
				{
					Name:        "complex.mission.insert",
					TableSchema: "complex",
					TableName:   "mission",
					RunType:     "insert",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "complex.spacecraft", Columns: []string{"id"}}, // NOT NULL dependency
					},
				},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "complex.astronaut.update.1",
					TableSchema: "complex",
					TableName:   "astronaut",
					RunType:     "update",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "complex.mission", Columns: []string{"id"}},
					},
				},
				{
					Name:        "complex.spacecraft.update.1",
					TableSchema: "complex",
					TableName:   "spacecraft",
					RunType:     "update",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "complex.mission", Columns: []string{"id"}},
					},
				},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Initially, astronaut and spacecraft can start (no dependencies)
	assert.True(t, tracker.CanConfigStart(groups[0].InsertConfigs[0]), "astronaut.insert should be able to start")
	assert.True(t, tracker.CanConfigStart(groups[0].InsertConfigs[1]), "spacecraft.insert should be able to start")

	// But mission.insert CANNOT start yet (depends on spacecraft.insert)
	assert.False(t, tracker.CanConfigStart(groups[0].InsertConfigs[2]), "mission.insert should wait for spacecraft.insert")

	// Complete spacecraft.insert
	err := tracker.MarkConfigComplete("complex.spacecraft.insert")
	require.NoError(t, err)

	// Now mission.insert can start
	assert.True(t, tracker.CanConfigStart(groups[0].InsertConfigs[2]), "mission.insert should be able to start after spacecraft completes")

	// But UPDATE configs still cannot start (INSERT phase not complete)
	assert.False(t, tracker.CanConfigStart(groups[0].UpdateConfigs[0]), "UPDATE should wait for INSERT phase")
	assert.False(t, tracker.CanConfigStart(groups[0].UpdateConfigs[1]), "UPDATE should wait for INSERT phase")

	// Complete remaining INSERT configs
	err = tracker.MarkConfigComplete("complex.astronaut.insert")
	require.NoError(t, err)
	err = tracker.MarkConfigComplete("complex.mission.insert")
	require.NoError(t, err)

	// Now INSERT phase is complete, UPDATE configs can start
	assert.True(t, tracker.CanConfigStart(groups[0].UpdateConfigs[0]), "UPDATE should start after INSERT phase completes")
	assert.True(t, tracker.CanConfigStart(groups[0].UpdateConfigs[1]), "UPDATE should start after INSERT phase completes")
}

func TestCanConfigStart_MultipleSharedTableCycles(t *testing.T) {
	// Test the scenario where a table (mission) appears in multiple cycles
	// and gets merged into a single group
	groups := []*ExecutionGroup{
		{
			ID:     "cycle:public.astronaut_public.mission_public.spacecraft",
			Tables: []string{"public.astronaut", "public.mission", "public.spacecraft"},
			InsertConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.astronaut.insert",
					TableSchema: "public",
					TableName:   "astronaut",
					RunType:     "insert",
					DependsOn:   []*runconfigs.DependsOn{},
				},
				{
					Name:        "public.spacecraft.insert",
					TableSchema: "public",
					TableName:   "spacecraft",
					RunType:     "insert",
					DependsOn:   []*runconfigs.DependsOn{},
				},
				{
					Name:        "public.mission.insert",
					TableSchema: "public",
					TableName:   "mission",
					RunType:     "insert",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.spacecraft", Columns: []string{"id"}},
					},
				},
			},
			UpdateConfigs: []*benthosbuilder.BenthosConfigResponse{
				{
					Name:        "public.astronaut.update.1",
					TableSchema: "public",
					TableName:   "astronaut",
					RunType:     "update",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.mission", Columns: []string{"id"}},
					},
				},
				{
					Name:        "public.spacecraft.update.1",
					TableSchema: "public",
					TableName:   "spacecraft",
					RunType:     "update",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.mission", Columns: []string{"id"}},
					},
				},
				{
					Name:        "public.mission.update.1",
					TableSchema: "public",
					TableName:   "mission",
					RunType:     "update",
					DependsOn: []*runconfigs.DependsOn{
						{Table: "public.astronaut", Columns: []string{"id"}},
					},
				},
			},
			DependsOnGroups: []string{},
			IsInCycle:       true,
		},
	}

	tracker := NewGroupCompletionTracker(groups)

	// Complete INSERT phase
	err := tracker.MarkConfigComplete("public.astronaut.insert")
	require.NoError(t, err)
	err = tracker.MarkConfigComplete("public.spacecraft.insert")
	require.NoError(t, err)
	err = tracker.MarkConfigComplete("public.mission.insert")
	require.NoError(t, err)

	// Verify INSERT phase is complete
	assert.True(t, tracker.IsInsertPhaseComplete("cycle:public.astronaut_public.mission_public.spacecraft"))

	// All three UPDATE configs should be able to start now
	assert.True(t, tracker.CanConfigStart(groups[0].UpdateConfigs[0]))
	assert.True(t, tracker.CanConfigStart(groups[0].UpdateConfigs[1]))
	assert.True(t, tracker.CanConfigStart(groups[0].UpdateConfigs[2]))

	// Complete UPDATE phase
	err = tracker.MarkConfigComplete("public.astronaut.update.1")
	require.NoError(t, err)
	err = tracker.MarkConfigComplete("public.spacecraft.update.1")
	require.NoError(t, err)
	err = tracker.MarkConfigComplete("public.mission.update.1")
	require.NoError(t, err)

	// Verify group is fully complete
	assert.True(t, tracker.IsGroupComplete("cycle:public.astronaut_public.mission_public.spacecraft"))
}
