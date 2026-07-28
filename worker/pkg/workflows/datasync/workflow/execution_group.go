package datasync_workflow

import (
	"sort"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	runconfigs "github.com/fishtre-compagnie/husonym/internal/runconfigs"
)

// ExecutionGroup represents a logical unit of execution for table synchronization.
// It groups INSERT and UPDATE configs that should be orchestrated together.
//
// For tables in a circular dependency cycle:
//   - All INSERT configs execute together (Phase 1)
//   - All UPDATE configs execute after INSERTs complete (Phase 2)
//
// For independent tables:
//   - The group contains only INSERT configs (single phase)
type ExecutionGroup struct {
	ID              string                                  // Unique identifier (e.g., "cycle:table1_table2" or "table:table1")
	Tables          []string                                // Tables included in this group
	InsertConfigs   []*benthosbuilder.BenthosConfigResponse // INSERT configs to execute in phase 1
	UpdateConfigs   []*benthosbuilder.BenthosConfigResponse // UPDATE configs to execute in phase 2
	DependsOnGroups []string                                // IDs of groups that must complete before this one
	IsInCycle       bool                                    // Whether this group represents a circular dependency cycle
}

// buildExecutionGroups creates execution groups from Benthos configs.
// It detects circular dependencies and groups configs accordingly.
func buildExecutionGroups(configs []*benthosbuilder.BenthosConfigResponse) []*ExecutionGroup {
	// Build dependency graph to detect cycles
	graph := buildConfigDependencyGraph(configs)
	cycles := runconfigs.FindCircularDependencies(graph)

	// Merge cycles that share common tables (e.g., mission is in both astronaut↔mission and spacecraft↔mission)
	mergedCycles := mergeCyclesWithSharedTables(cycles)

	// Map tables to their cycle (if any)
	tableToCycle := make(map[string]int) // table -> cycle index (-1 if not in cycle)
	for i, cycle := range mergedCycles {
		for _, table := range cycle {
			tableToCycle[table] = i
		}
	}

	// Group configs by cycle or individual table
	cycleGroups := make(map[int]*ExecutionGroup)    // cycle index -> group
	tableGroups := make(map[string]*ExecutionGroup) // table -> group (for non-cycle tables)

	for _, cfg := range configs {
		tableName := buildTableName(cfg.TableSchema, cfg.TableName)

		if cycleIdx, inCycle := tableToCycle[tableName]; inCycle {
			// Part of a cycle - add to cycle group
			if cycleGroups[cycleIdx] == nil {
				cycleGroups[cycleIdx] = &ExecutionGroup{
					// cycleIdx indexes mergedCycles (via tableToCycle), so the
					// group must be built from mergedCycles — not the unmerged
					// cycles slice, whose length and ordering differ. Using the
					// wrong slice drops merged tables from Tables, which makes
					// external dependents miss their dependency on the cycle.
					ID:        buildCycleGroupID(mergedCycles[cycleIdx]),
					Tables:    mergedCycles[cycleIdx],
					IsInCycle: true,
				}
			}
			group := cycleGroups[cycleIdx]
			if cfg.RunType == runconfigs.RunTypeInsert {
				group.InsertConfigs = append(group.InsertConfigs, cfg)
			} else {
				group.UpdateConfigs = append(group.UpdateConfigs, cfg)
			}
		} else {
			// Not in a cycle - create individual table group
			if tableGroups[tableName] == nil {
				tableGroups[tableName] = &ExecutionGroup{
					ID:        "table:" + tableName,
					Tables:    []string{tableName},
					IsInCycle: false,
				}
			}
			group := tableGroups[tableName]
			if cfg.RunType == runconfigs.RunTypeInsert {
				group.InsertConfigs = append(group.InsertConfigs, cfg)
			} else {
				group.UpdateConfigs = append(group.UpdateConfigs, cfg)
			}
		}
	}

	// Combine all groups
	var groups []*ExecutionGroup
	for _, g := range cycleGroups {
		groups = append(groups, g)
	}
	for _, g := range tableGroups {
		groups = append(groups, g)
	}

	// Calculate inter-group dependencies
	for _, group := range groups {
		group.DependsOnGroups = calculateGroupDependencies(group, groups, tableToCycle)
	}

	// Sort groups by ID for deterministic ordering
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].ID < groups[j].ID
	})

	// Sort configs within each group for deterministic ordering
	for _, group := range groups {
		sort.Slice(group.InsertConfigs, func(i, j int) bool {
			return group.InsertConfigs[i].Name < group.InsertConfigs[j].Name
		})
		sort.Slice(group.UpdateConfigs, func(i, j int) bool {
			return group.UpdateConfigs[i].Name < group.UpdateConfigs[j].Name
		})
		sort.Strings(group.DependsOnGroups)
	}

	return groups
}

// buildConfigDependencyGraph builds a table-level dependency graph from configs
func buildConfigDependencyGraph(configs []*benthosbuilder.BenthosConfigResponse) map[string][]string {
	graph := make(map[string][]string)

	for _, cfg := range configs {
		tableName := buildTableName(cfg.TableSchema, cfg.TableName)

		for _, dep := range cfg.DependsOn {
			// Only add if it's a dependency to a different table
			if dep.Table != tableName {
				// Check if already added to avoid duplicates
				found := false
				for _, existing := range graph[tableName] {
					if existing == dep.Table {
						found = true
						break
					}
				}
				if !found {
					graph[tableName] = append(graph[tableName], dep.Table)
				}
			}
		}
	}

	return graph
}

// calculateGroupDependencies determines which groups this group depends on
func calculateGroupDependencies(
	group *ExecutionGroup,
	allGroups []*ExecutionGroup,
	tableToCycle map[string]int,
) []string {
	dependentGroupIDs := make(map[string]bool)

	// Check all configs in this group
	allConfigs := make([]*benthosbuilder.BenthosConfigResponse, 0, len(group.InsertConfigs)+len(group.UpdateConfigs))
	allConfigs = append(allConfigs, group.InsertConfigs...)
	allConfigs = append(allConfigs, group.UpdateConfigs...)
	for _, cfg := range allConfigs {
		currentTable := buildTableName(cfg.TableSchema, cfg.TableName)

		for _, dep := range cfg.DependsOn {
			// Skip self-references
			if dep.Table == currentTable {
				continue
			}

			// Skip dependencies within the same cycle
			if group.IsInCycle {
				currentCycleIdx := tableToCycle[currentTable]
				if depCycleIdx, ok := tableToCycle[dep.Table]; ok && depCycleIdx == currentCycleIdx {
					continue
				}
			}

			// Find which group contains this dependency
			for _, otherGroup := range allGroups {
				if otherGroup.ID == group.ID {
					continue
				}
				for _, table := range otherGroup.Tables {
					if table == dep.Table {
						dependentGroupIDs[otherGroup.ID] = true
						break
					}
				}
			}
		}
	}

	// Convert map to slice
	var deps []string
	for id := range dependentGroupIDs {
		deps = append(deps, id)
	}

	return deps
}

// mergeCyclesWithSharedTables merges cycles that share common tables.
// For example, if mission appears in both [astronaut, mission] and [spacecraft, mission],
// they will be merged into [astronaut, mission, spacecraft].
func mergeCyclesWithSharedTables(cycles [][]string) [][]string {
	if len(cycles) <= 1 {
		return cycles
	}

	// Build a map: table -> list of cycle indices containing that table
	tableToCycles := make(map[string][]int)
	for cycleIdx, cycle := range cycles {
		for _, table := range cycle {
			tableToCycles[table] = append(tableToCycles[table], cycleIdx)
		}
	}

	// Find cycles that need to be merged (using Union-Find approach)
	parent := make([]int, len(cycles))
	for i := range parent {
		parent[i] = i // Initially, each cycle is its own parent
	}

	// Find root with path compression
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	// Union two cycles
	union := func(x, y int) {
		rootX := find(x)
		rootY := find(y)
		if rootX != rootY {
			parent[rootY] = rootX
		}
	}

	// Merge cycles that share tables
	for _, cycleIndices := range tableToCycles {
		if len(cycleIndices) > 1 {
			// This table appears in multiple cycles, merge them
			for i := 1; i < len(cycleIndices); i++ {
				union(cycleIndices[0], cycleIndices[i])
			}
		}
	}

	// Group cycles by their root
	mergedGroups := make(map[int]map[string]bool)
	for cycleIdx := range cycles {
		root := find(cycleIdx)
		if mergedGroups[root] == nil {
			mergedGroups[root] = make(map[string]bool)
		}
		// Add all tables from this cycle to the merged group
		for _, table := range cycles[cycleIdx] {
			mergedGroups[root][table] = true
		}
	}

	// Convert back to slice format
	result := make([][]string, 0, len(mergedGroups))
	for _, tableSet := range mergedGroups {
		cycle := make([]string, 0, len(tableSet))
		for table := range tableSet {
			cycle = append(cycle, table)
		}
		result = append(result, cycle)
	}

	return result
}

// buildCycleGroupID creates a unique ID for a cycle group
func buildCycleGroupID(tables []string) string {
	// Sort tables for consistent ID
	sorted := make([]string, len(tables))
	copy(sorted, tables)
	// Simple bubble sort for small slices
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	id := "cycle:"
	for i, table := range sorted {
		if i > 0 {
			id += "_"
		}
		id += table
	}
	return id
}
