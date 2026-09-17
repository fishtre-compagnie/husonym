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

	// GeneratedColumns are the columns the destination computes itself (GENERATED ALWAYS
	// AS …): they are read, since transformers may need them, and never written.
	GeneratedColumns []string `json:"generatedColumns,omitempty"`

	// PublishedKeys are the columns of the table that foreign keys of other tables
	// reference and that a transformer changes: each new value is published, under the
	// source one, for those foreign keys to follow.
	PublishedKeys []*PublishedKey `json:"publishedKeys,omitempty"`

	// ForeignKeys are the foreign keys of the table to tables of the job, virtual ones
	// included. The query already reads NULL for nullable keys to rows left out of the
	// subset; the engine checks the mandatory ones when it writes.
	ForeignKeys []*ForeignKey `json:"foreignKeys,omitempty"`
}

// PublishedKey is a referenced column whose transformed values are published.
type PublishedKey struct {
	Column string `json:"column"`
	// Store names where the values are published: a Redis hash, source value to new value.
	Store string `json:"store"`
}

// ForeignKey is one foreign key of a table, declared in the database or virtual.
type ForeignKey struct {
	Columns []string `json:"columns"`
	// NotNull tells, per column, whether it refuses NULL.
	NotNull       []bool   `json:"notNull"`
	ParentSchema  string   `json:"parentSchema"`
	ParentTable   string   `json:"parentTable"`
	ParentColumns []string `json:"parentColumns"`
	// ParentReduced is set when the job copies only part of the parent table: a row of
	// this table can then reference a parent row that is not copied.
	ParentReduced bool `json:"parentReduced"`
	// ParentKeyStores gives, per column, where the new values of the referenced column are
	// published when a transformer changes it, and "" when it is copied as it is.
	ParentKeyStores []string `json:"parentKeyStores,omitempty"`
	// NoParentValue is the value meaning "no parent" in a mandatory single-column key:
	// the default of a NOT NULL column (parent_id NOT NULL DEFAULT 0). Rows holding it
	// reference nothing on purpose and are legitimate.
	NoParentValue *string `json:"noParentValue,omitempty"`
}

// IsMandatory reports whether no column of the key accepts NULL: the key cannot be
// cleared, a row whose parent is missing cannot be written.
func (fk *ForeignKey) IsMandatory() bool {
	for _, notNull := range fk.NotNull {
		if !notNull {
			return false
		}
	}
	return len(fk.NotNull) > 0
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
