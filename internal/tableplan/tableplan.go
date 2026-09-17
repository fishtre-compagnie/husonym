// Package tableplan describes the work of one table sync independently of the engine
// that executes it.
//
// GenerateBenthosConfigs computes, once per run, what each table sync must do: which
// rows to read (subset included), in which order and page size, and whether the pass
// inserts rows or updates columns deferred by circular or nullable foreign keys. Benthos
// receives that work as a stream config; Athanor must not depend on that format, so the
// same result is also stored as a TablePlan. Both engines therefore run exactly the same
// work, which is what makes comparing them meaningful.
package tableplan

import (
	"encoding/json"
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
)

// TablePlan is the engine-neutral description of one table sync (one run config).
type TablePlan struct {
	// Id is the run config id, the same one TableSync receives.
	Id     string `json:"id"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	// RunType is insert (rows are created) or update (columns deferred from the insert
	// pass are filled in).
	RunType runconfigs.RunType `json:"runType"`

	// Query reads the first page: every selected column, subset joins and where clause
	// included, limited to PageLimit rows.
	Query string `json:"query"`
	// PageQuery reads the page following the last OrderByColumns values read.
	PageQuery      string   `json:"pageQuery,omitempty"`
	PageLimit      int      `json:"pageLimit,omitempty"`
	OrderByColumns []string `json:"orderByColumns,omitempty"`

	// Columns are the columns this pass writes: every inserted column for an insert,
	// the deferred columns for an update.
	Columns []string `json:"columns"`
}

// IsPaged reports whether the plan reads its table page by page.
func (p *TablePlan) IsPaged() bool {
	return p.PageQuery != "" && p.PageLimit > 0 && len(p.OrderByColumns) > 0
}

// Marshal encodes the plan for storage in the run context.
func (p *TablePlan) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

// Unmarshal decodes a plan stored in the run context.
func Unmarshal(data []byte) (*TablePlan, error) {
	var p TablePlan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("tableplan: unable to decode plan: %w", err)
	}
	if p.Id == "" || p.Table == "" || p.Query == "" {
		return nil, fmt.Errorf("tableplan: incomplete plan (id %q, table %q)", p.Id, p.Table)
	}
	return &p, nil
}
