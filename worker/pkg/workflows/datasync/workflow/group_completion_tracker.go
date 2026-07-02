package datasync_workflow

import (
	"fmt"
	"log/slog"
	"sync"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
)

// GroupCompletionTracker tracks the completion of execution groups.
// Each group must complete all its configs before being marked complete.
type GroupCompletionTracker struct {
	mu               sync.RWMutex
	groups           map[string]*GroupState // groupID -> state
	configToGroup    map[string]string      // configName -> groupID
	completedConfigs map[string]bool        // configName -> completed
}

// GroupState tracks the completion state of a single execution group
type GroupState struct {
	Group               *ExecutionGroup
	InsertPhaseComplete bool // All INSERT configs completed
	UpdatePhaseComplete bool // All UPDATE configs completed
	TotalConfigs        int  // Total configs in this group
	CompletedCount      int  // Number of completed configs
}

// NewGroupCompletionTracker creates a new tracker for execution groups
func NewGroupCompletionTracker(groups []*ExecutionGroup) *GroupCompletionTracker {
	tracker := &GroupCompletionTracker{
		groups:           make(map[string]*GroupState),
		configToGroup:    make(map[string]string),
		completedConfigs: make(map[string]bool),
	}

	for _, group := range groups {
		totalConfigs := len(group.InsertConfigs) + len(group.UpdateConfigs)
		tracker.groups[group.ID] = &GroupState{
			Group:               group,
			InsertPhaseComplete: len(group.InsertConfigs) == 0, // Empty means complete
			UpdatePhaseComplete: len(group.UpdateConfigs) == 0, // Empty means complete
			TotalConfigs:        totalConfigs,
			CompletedCount:      0,
		}

		// Map configs to their group
		for _, cfg := range group.InsertConfigs {
			tracker.configToGroup[cfg.Name] = group.ID
		}
		for _, cfg := range group.UpdateConfigs {
			tracker.configToGroup[cfg.Name] = group.ID
		}
	}

	return tracker
}

// MarkConfigComplete marks a config as completed and updates group state
func (t *GroupCompletionTracker) MarkConfigComplete(configName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already completed
	if t.completedConfigs[configName] {
		slog.Debug("GroupCompletionTracker: config already marked complete", "config", configName)
		return nil
	}

	// Find which group this config belongs to
	groupID, exists := t.configToGroup[configName]
	if !exists {
		return fmt.Errorf("config not found in any group: %s", configName)
	}

	state := t.groups[groupID]
	if state == nil {
		return fmt.Errorf("group state not found: %s", groupID)
	}

	// Mark config as completed
	t.completedConfigs[configName] = true
	state.CompletedCount++

	slog.Debug(
		"GroupCompletionTracker: marked config complete",
		"config", configName,
		"groupID", groupID,
		"completedCount", state.CompletedCount,
		"totalConfigs", state.TotalConfigs,
	)

	// Check if INSERT phase is complete
	if !state.InsertPhaseComplete {
		insertCompleteCount := 0
		for _, cfg := range state.Group.InsertConfigs {
			if t.completedConfigs[cfg.Name] {
				insertCompleteCount++
			}
		}
		if insertCompleteCount == len(state.Group.InsertConfigs) {
			state.InsertPhaseComplete = true
			slog.Info(
				"GroupCompletionTracker: INSERT phase complete",
				"groupID", groupID,
				"insertCount", len(state.Group.InsertConfigs),
			)
		}
	}

	// Check if UPDATE phase is complete
	if !state.UpdatePhaseComplete && state.InsertPhaseComplete {
		updateCompleteCount := 0
		for _, cfg := range state.Group.UpdateConfigs {
			if t.completedConfigs[cfg.Name] {
				updateCompleteCount++
			}
		}
		if updateCompleteCount == len(state.Group.UpdateConfigs) {
			state.UpdatePhaseComplete = true
			slog.Info(
				"GroupCompletionTracker: UPDATE phase complete",
				"groupID", groupID,
				"updateCount", len(state.Group.UpdateConfigs),
			)
		}
	}

	// Check if entire group is complete
	if state.CompletedCount == state.TotalConfigs {
		slog.Info(
			"GroupCompletionTracker: group fully complete",
			"groupID", groupID,
			"totalConfigs", state.TotalConfigs,
		)
	}

	return nil
}

// IsGroupComplete checks if all configs in a group have completed
func (t *GroupCompletionTracker) IsGroupComplete(groupID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.groups[groupID]
	if !exists {
		return false
	}

	return state.CompletedCount == state.TotalConfigs
}

// IsInsertPhaseComplete checks if all INSERT configs in a group have completed
func (t *GroupCompletionTracker) IsInsertPhaseComplete(groupID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.groups[groupID]
	if !exists {
		return false
	}

	return state.InsertPhaseComplete
}

// CanConfigStart checks if a config can start based on:
// 1. Group-level dependencies (dependent groups must be complete)
// 2. Phase ordering (UPDATE waits for INSERT phase in cycles)
// 3. Config-level dependencies (individual config dependencies must be satisfied)
func (t *GroupCompletionTracker) CanConfigStart(cfg *benthosbuilder.BenthosConfigResponse) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Find which group this config belongs to
	groupID, exists := t.configToGroup[cfg.Name]
	if !exists {
		return false
	}

	state := t.groups[groupID]
	if state == nil {
		return false
	}

	// Check if all dependent groups are complete
	for _, depGroupID := range state.Group.DependsOnGroups {
		depState, exists := t.groups[depGroupID]
		if !exists || depState.CompletedCount != depState.TotalConfigs {
			slog.Debug(
				"GroupCompletionTracker: config blocked by incomplete group dependency",
				"config", cfg.Name,
				"groupID", groupID,
				"dependsOn", depGroupID,
			)
			return false
		}
	}

	// For UPDATE configs in a cycle, wait for INSERT phase to complete
	if state.Group.IsInCycle && cfg.RunType == "update" {
		if !state.InsertPhaseComplete {
			slog.Debug(
				"GroupCompletionTracker: UPDATE config waiting for INSERT phase",
				"config", cfg.Name,
				"groupID", groupID,
			)
			return false
		}
	}

	// CRITICAL: Check individual config dependencies
	// Even within a cycle, configs may depend on each other
	// (e.g., referral_codes.insert depends on store_customers.insert)
	for _, dep := range cfg.DependsOn {
		// Check if this dependency config has completed
		depConfigName := findConfigByTable(state.Group, dep.Table)
		if depConfigName == "" {
			// Dependency is outside this group, should be handled by group dependencies
			continue
		}

		// Check if the dependent config in the same group is completed
		if !t.completedConfigs[depConfigName] {
			slog.Debug(
				"GroupCompletionTracker: config blocked by incomplete intra-group dependency",
				"config", cfg.Name,
				"groupID", groupID,
				"dependsOn", depConfigName,
			)
			return false
		}
	}

	return true
}

// findConfigByTable finds the INSERT config name for a given table in the group
// This is used to check intra-group dependencies
func findConfigByTable(group *ExecutionGroup, tableName string) string {
	// Check INSERT configs first (most common dependency)
	for _, cfg := range group.InsertConfigs {
		cfgTable := buildTableName(cfg.TableSchema, cfg.TableName)
		if cfgTable == tableName {
			return cfg.Name
		}
	}

	// Check UPDATE configs if not found in INSERT
	for _, cfg := range group.UpdateConfigs {
		cfgTable := buildTableName(cfg.TableSchema, cfg.TableName)
		if cfgTable == tableName {
			return cfg.Name
		}
	}

	return ""
}

// GetCompletionStatus returns current completion status for all groups (for debugging)
func (t *GroupCompletionTracker) GetCompletionStatus() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := make(map[string]string)
	for groupID, state := range t.groups {
		insertStatus := ""
		if state.InsertPhaseComplete {
			insertStatus = " [INSERT:DONE]"
		} else {
			insertStatus = fmt.Sprintf(
				" [INSERT:%d/%d]",
				countCompleted(state.Group.InsertConfigs, t.completedConfigs),
				len(state.Group.InsertConfigs),
			)
		}

		updateStatus := ""
		if len(state.Group.UpdateConfigs) > 0 {
			if state.UpdatePhaseComplete {
				updateStatus = " [UPDATE:DONE]"
			} else {
				updateStatus = fmt.Sprintf(
					" [UPDATE:%d/%d]",
					countCompleted(state.Group.UpdateConfigs, t.completedConfigs),
					len(state.Group.UpdateConfigs),
				)
			}
		}

		status[groupID] = fmt.Sprintf(
			"%d/%d%s%s",
			state.CompletedCount,
			state.TotalConfigs,
			insertStatus,
			updateStatus,
		)
	}

	return status
}

func countCompleted(
	configs []*benthosbuilder.BenthosConfigResponse,
	completedConfigs map[string]bool,
) int {
	count := 0
	for _, cfg := range configs {
		if completedConfigs[cfg.Name] {
			count++
		}
	}
	return count
}
