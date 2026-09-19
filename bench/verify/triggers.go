package verify

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// Triggers reads the triggers a destination holds on the tables of a case, as the renderer
// describes them: one line per trigger, in a stable order.
func Triggers(ctx context.Context, db *sql.DB, r schema.Renderer, container string) ([]string, error) {
	rows, err := db.QueryContext(ctx, r.TriggerStateQuery(), container)
	if err != nil {
		return nil, fmt.Errorf("verify: triggers of %s: %w", container, err)
	}
	defer rows.Close()
	var triggers []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, fmt.Errorf("verify: triggers of %s: %w", container, err)
		}
		triggers = append(triggers, line)
	}
	return triggers, rows.Err()
}

// TriggerChanges tells what a run left different among the triggers of its destination. A
// run may take them out of its way, never leave them changed: a trigger is application
// logic the destination keeps running after the copy.
func TriggerChanges(before, after []string) []string {
	var changes []string
	for _, trigger := range before {
		if !slices.Contains(after, trigger) {
			changes = append(changes, "trigger absent ou modifié après le run : "+trigger)
		}
	}
	for _, trigger := range after {
		if !slices.Contains(before, trigger) {
			changes = append(changes, "trigger apparu ou modifié après le run : "+trigger)
		}
	}
	return changes
}
