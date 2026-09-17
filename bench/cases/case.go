// Package cases holds the tricky cases of the engine test bench.
//
// A case is a small set of tables, the rows that trigger one difficulty, the job that
// syncs them and what a correct engine must produce. Each case runs in its own job, once
// per engine, so a case that fails its run never hides the others. Engines are checked
// against the expectation, not only against each other: a defect they share must show.
package cases

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// Priority ranks what a failing case costs.
type Priority int

const (
	// P1: data loss or leak, silent corruption.
	P1 Priority = iota + 1
	// P2: clear failure or behavior gap.
	P2
	// P3: robustness and performance.
	P3
)

func (p Priority) String() string { return fmt.Sprintf("P%d", int(p)) }

// Verdict tells whether a source row must reach the destination.
type Verdict string

const (
	VerdictKept    Verdict = "kept"
	VerdictDropped Verdict = "dropped"
)

// RowExpect is what a correct engine does with one source row.
type RowExpect struct {
	Verdict Verdict
	// NullColumns are the columns a kept row must hold NULL in at the destination,
	// whatever their source value (nullable foreign keys to rows out of the subset).
	NullColumns []string
}

// Kept expects the row at the destination, with the given columns set to NULL.
func Kept(nullColumns ...string) RowExpect {
	return RowExpect{Verdict: VerdictKept, NullColumns: nullColumns}
}

// Dropped expects the row to be absent from the destination.
func Dropped() RowExpect { return RowExpect{Verdict: VerdictDropped} }

// Rule is what a destination column must satisfy.
type Rule string

const (
	// RuleUnchanged: same value as the source row. Default of every passthrough column.
	RuleUnchanged Rule = "unchanged"
	// RuleNull: always NULL.
	RuleNull Rule = "null"
	// RuleNotInSourceSet: no destination value appears in the source column (no personal
	// data copied as is).
	RuleNotInSourceSet Rule = "not_in_source_set"
	// RuleUnique: no two destination rows share a value.
	RuleUnique Rule = "unique"
	// RuleFollowsParent: the column is a single-column foreign key whose parent key is
	// transformed; its destination value must reference the same parent row as in the
	// source, whatever the new key of that parent is.
	RuleFollowsParent Rule = "follows_parent"
)

// ColumnSpec configures one column of the job: its transformer and the rules its
// destination values must satisfy. A column without spec is passed through unchanged.
type ColumnSpec struct {
	Transformer *mgmtv1alpha1.TransformerConfig
	Rules       []Rule
}

// Job is the part of the job configuration a case controls.
type Job struct {
	// Where holds the subset clause of each filtered table.
	Where map[string]string
	// SubsetByForeignKeys propagates the subset along foreign keys.
	SubsetByForeignKeys bool
	// SkipForeignKeyViolations lets the engine drop or null what would break a foreign
	// key; without it the run must fail on the first violation.
	SkipForeignKeyViolations bool
	// Columns configures the columns that are not plain passthrough: table, then column.
	Columns map[string]map[string]ColumnSpec
	// TruncateBeforeInsert has the run empty the destination tables first. Every row of the
	// destination then comes from the run, which lets it repair the orphans it finds.
	TruncateBeforeInsert bool
	// OnConflictUpdate writes with "on conflict do update" instead of a plain insert.
	OnConflictUpdate bool
	// ExcludedTables exist in the source and in the destination but are left out of the
	// job, the way a user skips a reference table filled by other means.
	ExcludedTables []string
	// BatchCount is how many rows a destination writes at once; zero keeps the default of
	// the product. It is what tells a difference of engine from a difference of setting.
	BatchCount uint32
	// SyncAttempts is the number of attempts a table sync gets. Default 1: a retry rewrites
	// its page with "do nothing", which would hide the very errors the bench is after.
	// Cases about retries ask for more.
	SyncAttempts int32
}

// Params are the bench settings a case sizes its rows from.
type Params struct {
	// PageLimit is the page size of the worker (MAX_TABLE_SYNC_PAGE_LIMIT).
	PageLimit int
	// Scale multiplies the filler rows of the cases that have some.
	Scale int
}

// Emitter receives the source rows of a case.
//
// Values follow the column order of the table and are limited to nil, int64, uint64,
// string and []byte. Decimals, floats, dates and times are strings written the way the
// database prints them ("1.10", "2024-01-31 10:00:00.123456"): the bench compares rows as
// the text the database returns, so nothing is rounded by a Go type on the way.
type Emitter interface {
	Row(table string, values []any, expect RowExpect)
}

// Case is one tricky case.
type Case struct {
	// ID names the case and its database (bench_<id>, dashes turned into underscores).
	ID       string
	Priority Priority
	// Title says, in one line of the report, what the case checks.
	Title  string
	Tables []*schema.Table
	Seed   func(p Params, emit Emitter)
	Job    Job
	// Identity gives, per table, the columns that identify a row at the destination.
	// Default: the primary key, or every unchanged column when the table has none.
	Identity map[string][]string
	// SourceSQLMode, when set, is the sql_mode the source rows are loaded with: legacy
	// values (id 0 in an AUTO_INCREMENT column, zero dates) need the mode that let them in.
	SourceSQLMode string
	// DestinationSetup statements run on each destination once its empty tables exist:
	// what a real destination holds beyond the tables (triggers, extra columns). {db} is
	// replaced by the database of the case.
	DestinationSetup []string
	// DestinationGrants, when set, makes the job write with a restricted account holding
	// only these privileges instead of root. {db} is replaced by the database of the case
	// and {user} by the account.
	DestinationGrants []string
	// MinPageLimit is the smallest worker page size the case makes sense with; below it
	// the case is reported as not exercised. Retry cases need pages larger than a write
	// batch, so that a page can fail half written.
	MinPageLimit int
	// ExpectRunError, when set, expects the run to fail with a message containing it.
	ExpectRunError string
}

// Database returns the name of the database holding the tables of the case, on the
// source and on every destination.
func (c *Case) Database() string {
	return "bench_" + strings.ReplaceAll(c.ID, "-", "_")
}

// Table returns the named table of the case, or nil.
func (c *Case) Table(name string) *schema.Table {
	for _, t := range c.Tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// IsExcluded reports whether the table is left out of the job.
func (c *Case) IsExcluded(table string) bool {
	return slices.Contains(c.Job.ExcludedTables, table)
}

// Spec returns the job spec of a column; ok is false for a plain passthrough column.
func (c *Case) Spec(table, column string) (spec ColumnSpec, ok bool) {
	spec, ok = c.Job.Columns[table][column]
	return spec, ok
}

// ColumnRules returns the rules of every column of the case: the ones of its spec, and
// RuleUnchanged for the columns passed through.
func (c *Case) ColumnRules() map[string]map[string][]Rule {
	rules := make(map[string]map[string][]Rule, len(c.Tables))
	for _, t := range c.Tables {
		rules[t.Name] = make(map[string][]Rule, len(t.Columns))
		for i := range t.Columns {
			column := t.Columns[i].Name
			if spec, ok := c.Spec(t.Name, column); ok {
				rules[t.Name][column] = spec.Rules
			} else {
				rules[t.Name][column] = []Rule{RuleUnchanged}
			}
		}
	}
	return rules
}

// IdentityColumns returns the columns identifying a row of the table at the destination:
// the declared identity, else the primary key, else every column passed through.
func (c *Case) IdentityColumns(table string) []string {
	if columns, ok := c.Identity[table]; ok {
		return columns
	}
	t := c.Table(table)
	if t == nil {
		return nil
	}
	if len(t.PrimaryKey) > 0 {
		return t.PrimaryKey
	}
	var columns []string
	for i := range t.Columns {
		if _, transformed := c.Spec(table, t.Columns[i].Name); !transformed {
			columns = append(columns, t.Columns[i].Name)
		}
	}
	return columns
}

// Validate reports the mistakes of a case definition the compiler cannot see.
func (c *Case) Validate() error {
	if c.ID == "" || c.Title == "" || c.Seed == nil || len(c.Tables) == 0 {
		return fmt.Errorf("cases: %q: id, title, tables and seed are required", c.ID)
	}
	if c.Priority < P1 || c.Priority > P3 {
		return fmt.Errorf("cases: %s: priority out of range", c.ID)
	}
	for _, t := range c.Tables {
		for _, name := range t.PrimaryKey {
			if t.Column(name) == nil {
				return fmt.Errorf("cases: %s: %s: unknown primary key column %q", c.ID, t.Name, name)
			}
		}
		for _, fk := range t.ForeignKeys {
			ref := c.Table(fk.RefTable)
			if ref == nil {
				return fmt.Errorf("cases: %s: %s: foreign key %s references unknown table %q",
					c.ID, t.Name, fk.Name, fk.RefTable)
			}
			if len(fk.Columns) == 0 || len(fk.Columns) != len(fk.RefColumns) {
				return fmt.Errorf("cases: %s: %s: foreign key %s has mismatched columns", c.ID, t.Name, fk.Name)
			}
			for i := range fk.Columns {
				if t.Column(fk.Columns[i]) == nil || ref.Column(fk.RefColumns[i]) == nil {
					return fmt.Errorf("cases: %s: %s: foreign key %s uses an unknown column", c.ID, t.Name, fk.Name)
				}
			}
		}
	}
	for table := range c.Job.Where {
		if c.Table(table) == nil {
			return fmt.Errorf("cases: %s: where clause on unknown table %q", c.ID, table)
		}
	}
	for table, columns := range c.Job.Columns {
		t := c.Table(table)
		if t == nil {
			return fmt.Errorf("cases: %s: column specs on unknown table %q", c.ID, table)
		}
		for column := range columns {
			if t.Column(column) == nil {
				return fmt.Errorf("cases: %s: column spec on unknown column %s.%s", c.ID, table, column)
			}
		}
	}
	for table, columns := range c.Identity {
		t := c.Table(table)
		if t == nil {
			return fmt.Errorf("cases: %s: identity on unknown table %q", c.ID, table)
		}
		for _, column := range columns {
			if t.Column(column) == nil {
				return fmt.Errorf("cases: %s: identity on unknown column %s.%s", c.ID, table, column)
			}
		}
	}
	return nil
}

// All returns every case, sorted by priority then id.
func All() []*Case {
	var all []*Case
	all = append(all, paginationCases()...)
	all = append(all, whereClauseCases()...)
	all = append(all, subsetForeignKeyCases()...)
	all = append(all, subsetRootsCases()...)
	all = append(all, legacyForeignKeyCases()...)
	all = append(all, typeCases()...)
	all = append(all, identifierCases()...)
	all = append(all, columnCases()...)
	all = append(all, destinationCases()...)
	all = append(all, transformerCases()...)
	all = append(all, retryCases()...)
	all = append(all, rightsCases()...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Priority != all[j].Priority {
			return all[i].Priority < all[j].Priority
		}
		return all[i].ID < all[j].ID
	})
	return all
}
