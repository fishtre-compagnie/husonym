// Package verify compares what an engine wrote with what the case expects.
//
// Rows are compared as the text the database prints (see oracle.CanonicalValue), read
// the same way from the source and from the destination, so no Go type rounds a value
// between the two.
package verify

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/oracle"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

const maxSamples = 5

// TableResult counts the gaps of one destination table.
type TableResult struct {
	Table           string `json:"table"`
	SourceRows      int    `json:"sourceRows"`
	ExpectedRows    int    `json:"expectedRows"`
	DestinationRows int    `json:"destinationRows"`
	// Missing rows were expected and are absent: data loss.
	Missing int `json:"missing"`
	// Leaked rows had to stay out of the destination: data leak.
	Leaked int `json:"leaked"`
	// Unexpected rows match no source row: their identity was altered.
	Unexpected int `json:"unexpected"`
	// Duplicated rows are present more times than in the source.
	Duplicated int `json:"duplicated"`
	// RuleViolations counts the values breaking a column rule, by "column: rule".
	RuleViolations map[string]int `json:"ruleViolations,omitempty"`
	// Samples show a few gaps, to start an investigation from the report.
	Samples []string `json:"samples,omitempty"`
}

// Gaps is the number of rows or values off the expectation.
func (r *TableResult) Gaps() int {
	total := r.Missing + r.Leaked + r.Unexpected + r.Duplicated
	for _, n := range r.RuleViolations {
		total += n
	}
	return total
}

func (r *TableResult) samplef(format string, args ...any) {
	if len(r.Samples) < maxSamples {
		r.Samples = append(r.Samples, fmt.Sprintf(format, args...))
	}
}

func (r *TableResult) violationf(column string, rule cases.Rule, format string, args ...any) {
	if r.RuleViolations == nil {
		r.RuleViolations = map[string]int{}
	}
	r.RuleViolations[column+": "+string(rule)]++
	r.samplef(format, args...)
}

// OrphanResult counts the destination rows of a foreign key, virtual ones included,
// that reference a missing parent.
type OrphanResult struct {
	Table      string `json:"table"`
	ForeignKey string `json:"foreignKey"`
	Orphans    int    `json:"orphans"`
}

// Result is the verification of one case written by one engine.
type Result struct {
	Tables  []*TableResult  `json:"tables"`
	Orphans []*OrphanResult `json:"orphans,omitempty"`
}

// Gaps is the total number of gaps of the case, orphans included.
func (r *Result) Gaps() int {
	total := 0
	for _, t := range r.Tables {
		total += t.Gaps()
	}
	for _, o := range r.Orphans {
		total += o.Orphans
	}
	return total
}

// Case verifies the destination of one case against its expectation.
func Case(ctx context.Context, source, destination *sql.DB, r schema.Renderer, c *cases.Case) (*Result, error) {
	// Every table is read first: a rule on a foreign key needs the rows of its parent.
	data := map[string]*tableRows{}
	for _, t := range c.Tables {
		if c.IsExcluded(t.Name) {
			continue
		}
		rows, err := readTable(ctx, source, destination, r, c, t)
		if err != nil {
			return nil, fmt.Errorf("verify: %s.%s: %w", c.ID, t.Name, err)
		}
		data[t.Name] = rows
	}

	result := &Result{}
	for _, t := range c.Tables {
		if c.IsExcluded(t.Name) {
			continue
		}
		rows := data[t.Name]
		tableResult, err := compareTable(t, rows.expected, rows.rules, rows.source, rows.dest)
		if err != nil {
			return nil, fmt.Errorf("verify: %s.%s: %w", c.ID, t.Name, err)
		}
		for column, rules := range rows.rules {
			if slices.Contains(rules, cases.RuleFollowsParent) {
				if err := checkFollowsParent(tableResult, c, t, column, data); err != nil {
					return nil, fmt.Errorf("verify: %s.%s: %w", c.ID, t.Name, err)
				}
			}
		}
		result.Tables = append(result.Tables, tableResult)

		for i := range t.ForeignKeys {
			fk := &t.ForeignKeys[i]
			orphans, err := countOrphans(ctx, destination, r, c.Database(), t, fk)
			if err != nil {
				return nil, fmt.Errorf("verify: %s.%s: %s: %w", c.ID, t.Name, fk.Name, err)
			}
			if orphans > 0 {
				result.Orphans = append(result.Orphans, &OrphanResult{Table: t.Name, ForeignKey: fk.Name, Orphans: orphans})
			}
		}
	}
	return result, nil
}

// row is a table row in canonical text, in column order.
type row []string

// tableRows is what verifying a table needs: its expectation and its rows on both
// sides, grouped by oracle key.
type tableRows struct {
	expected     map[string]*oracle.ExpectedRow
	rules        map[string][]cases.Rule
	source, dest map[string][]row
}

func readTable(
	ctx context.Context,
	source, destination *sql.DB,
	r schema.Renderer,
	c *cases.Case,
	t *schema.Table,
) (*tableRows, error) {
	var rows tableRows
	var err error
	if rows.expected, err = oracle.ReadRows(ctx, source, c.ID, t.Name); err != nil {
		return nil, err
	}
	if rows.rules, err = oracle.ReadColumnRules(ctx, source, c.ID, t.Name); err != nil {
		return nil, err
	}
	identity := columnIndexes(t, c.IdentityColumns(t.Name))
	if rows.source, err = readRows(ctx, source, r, c.Database(), t, identity); err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if rows.dest, err = readRows(ctx, destination, r, c.Database(), t, identity); err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}
	return &rows, nil
}

// checkFollowsParent verifies a foreign key whose parent key is transformed: each
// destination row must reference the parent row its source row references, identified by
// the identity columns of the parent rather than by its changed key.
func checkFollowsParent(result *TableResult, c *cases.Case, t *schema.Table, column string, data map[string]*tableRows) error {
	var fk *schema.ForeignKey
	for i := range t.ForeignKeys {
		if len(t.ForeignKeys[i].Columns) == 1 && t.ForeignKeys[i].Columns[0] == column {
			fk = &t.ForeignKeys[i]
		}
	}
	if fk == nil {
		return fmt.Errorf("column %s: rule %s needs a single-column foreign key", column, cases.RuleFollowsParent)
	}
	parent, parentRows := c.Table(fk.RefTable), data[fk.RefTable]
	if parent == nil || parentRows == nil {
		return fmt.Errorf("column %s: parent table %s is not verified", column, fk.RefTable)
	}
	refIndex := columnIndexes(parent, fk.RefColumns)[0]
	parentKeyOf := func(rowsByKey map[string][]row) map[string]string {
		byRef := map[string]string{}
		for key, rows := range rowsByKey {
			for _, parentRow := range rows {
				byRef[parentRow[refIndex]] = key
			}
		}
		return byRef
	}
	sourceParents, destParents := parentKeyOf(parentRows.source), parentKeyOf(parentRows.dest)

	columnIndex := columnIndexes(t, []string{column})[0]
	rows := data[t.Name]
	for _, key := range sortedKeys(rows.dest) {
		if len(rows.source[key]) == 0 {
			continue // already counted as unexpected
		}
		sourceValue := rows.source[key][0][columnIndex]
		for _, dest := range rows.dest[key] {
			destValue := dest[columnIndex]
			switch {
			case sourceValue == nullText && destValue == nullText:
			case sourceValue == nullText || destValue == nullText:
				result.violationf(column, cases.RuleFollowsParent, "%s de %s : %s alors que la source a %s",
					column, readable(key), display(destValue), display(sourceValue))
			case destParents[destValue] == "" || destParents[destValue] != sourceParents[sourceValue]:
				result.violationf(column, cases.RuleFollowsParent, "%s de %s : référence %s au lieu du parent %s",
					column, readable(key), display(destValue), readable(sourceParents[sourceValue]))
			}
		}
	}
	return nil
}

// compareTable counts the gaps between the destination rows of a table and its
// expectation. Rows are grouped by oracle key.
func compareTable(
	t *schema.Table,
	expected map[string]*oracle.ExpectedRow,
	rules map[string][]cases.Rule,
	sourceRows, destRows map[string][]row,
) (*TableResult, error) {
	result := &TableResult{Table: t.Name}
	for _, rows := range sourceRows {
		result.SourceRows += len(rows)
	}
	for _, rows := range destRows {
		result.DestinationRows += len(rows)
	}

	for _, key := range sortedKeys(expected) {
		want := expected[key]
		got := len(destRows[key])
		if want.Verdict == cases.VerdictDropped {
			if got > 0 {
				result.Leaked += got
				result.samplef("ligne hors attendu présente : %s", readable(key))
			}
			continue
		}
		result.ExpectedRows += want.Occurrences
		switch {
		case got < want.Occurrences:
			result.Missing += want.Occurrences - got
			result.samplef("ligne attendue absente : %s", readable(key))
		case got > want.Occurrences:
			result.Duplicated += got - want.Occurrences
			result.samplef("ligne en double : %s (%d pour %d)", readable(key), got, want.Occurrences)
		}
	}
	for _, key := range sortedKeys(destRows) {
		if _, known := expected[key]; !known {
			result.Unexpected += len(destRows[key])
			result.samplef("ligne sans origine dans la source : %s", readable(key))
		}
	}

	if err := checkRules(result, t, rules, expected, sourceRows, destRows); err != nil {
		return nil, err
	}
	return result, nil
}

func checkRules(
	result *TableResult,
	t *schema.Table,
	rules map[string][]cases.Rule,
	expected map[string]*oracle.ExpectedRow,
	sourceRows, destRows map[string][]row,
) error {
	for i := range t.Columns {
		column := t.Columns[i].Name
		for _, rule := range rules[column] {
			switch rule {
			case cases.RuleUnchanged:
				for _, key := range sortedKeys(destRows) {
					want, known := expected[key]
					if !known || want.Verdict != cases.VerdictKept || len(sourceRows[key]) == 0 {
						continue // already counted as leaked or unexpected
					}
					wantValue := sourceRows[key][0][i]
					for _, name := range want.NullColumns {
						if name == column {
							wantValue = nullText
						}
					}
					for _, dest := range destRows[key] {
						if dest[i] != wantValue {
							result.violationf(column, rule, "%s de %s : %s au lieu de %s",
								column, readable(key), display(dest[i]), display(wantValue))
						}
					}
				}
			case cases.RuleNull:
				for _, key := range sortedKeys(destRows) {
					for _, dest := range destRows[key] {
						if dest[i] != nullText {
							result.violationf(column, rule, "%s de %s : %s au lieu de NULL", column, readable(key), display(dest[i]))
						}
					}
				}
			case cases.RuleUnique:
				seen := map[string]bool{}
				for _, key := range sortedKeys(destRows) {
					for _, dest := range destRows[key] {
						if dest[i] != nullText && seen[dest[i]] {
							result.violationf(column, rule, "%s : valeur %q en double", column, dest[i])
						}
						seen[dest[i]] = true
					}
				}
			case cases.RuleNotInSourceSet:
				inSource := map[string]bool{}
				for _, rows := range sourceRows {
					for _, src := range rows {
						inSource[src[i]] = true
					}
				}
				for _, key := range sortedKeys(destRows) {
					for _, dest := range destRows[key] {
						if dest[i] != nullText && inSource[dest[i]] {
							result.violationf(column, rule, "%s de %s : valeur source %q recopiée", column, readable(key), dest[i])
						}
					}
				}
			case cases.RuleFollowsParent:
				// Needs the rows of the parent table: see checkFollowsParent.
			default:
				return fmt.Errorf("column %s: rule %q is not verified by the bench", column, rule)
			}
		}
	}
	return nil
}

const nullText = `\N`

// readRows loads a table in canonical text, grouped by oracle key.
func readRows(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	database string,
	t *schema.Table,
	identity []int,
) (map[string][]row, error) {
	quoted := make([]string, len(t.Columns))
	for i := range t.Columns {
		quoted[i] = r.QuoteIdent(t.Columns[i].Name)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s.%s",
		strings.Join(quoted, ", "), r.QuoteIdent(database), r.QuoteIdent(t.Name)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	raw := make([]sql.RawBytes, len(t.Columns))
	dest := make([]any, len(t.Columns))
	for i := range raw {
		dest[i] = &raw[i]
	}
	byKey := map[string][]row{}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		values := make(row, len(t.Columns))
		for i := range raw {
			var v any
			if raw[i] != nil {
				v = []byte(raw[i])
			}
			values[i], err = oracle.CanonicalValue(v, t.Columns[i].IsBinary())
			if err != nil {
				return nil, err
			}
		}
		parts := make([]string, len(identity))
		for i, idx := range identity {
			parts[i] = values[idx]
		}
		key := oracle.RowKey(parts)
		byKey[key] = append(byKey[key], values)
	}
	return byKey, rows.Err()
}

// countOrphans counts the rows whose foreign key, fully set (MATCH SIMPLE), references
// no row of the parent table.
func countOrphans(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	database string,
	t *schema.Table,
	fk *schema.ForeignKey,
) (int, error) {
	var conditions, joins []string
	for i := range fk.Columns {
		conditions = append(conditions, "c."+r.QuoteIdent(fk.Columns[i])+" IS NOT NULL")
		joins = append(joins, "p."+r.QuoteIdent(fk.RefColumns[i])+" = c."+r.QuoteIdent(fk.Columns[i]))
	}
	if fk.Sentinel != "" {
		conditions = append(conditions, "c."+r.QuoteIdent(fk.Columns[0])+" <> "+fk.Sentinel)
	}
	//nolint:gosec // identifiers come from the case definitions and are quoted
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s c WHERE %s AND NOT EXISTS (SELECT 1 FROM %s.%s p WHERE %s)",
		r.QuoteIdent(database), r.QuoteIdent(t.Name), strings.Join(conditions, " AND "),
		r.QuoteIdent(database), r.QuoteIdent(fk.RefTable), strings.Join(joins, " AND "))
	var orphans int
	if err := db.QueryRowContext(ctx, query).Scan(&orphans); err != nil {
		return 0, err
	}
	return orphans, nil
}

func columnIndexes(t *schema.Table, names []string) []int {
	indexes := make([]int, 0, len(names))
	for _, name := range names {
		for i := range t.Columns {
			if t.Columns[i].Name == name {
				indexes = append(indexes, i)
			}
		}
	}
	return indexes
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// display shows a canonical value in a report.
func display(value string) string {
	if value == nullText {
		return "NULL"
	}
	return fmt.Sprintf("%q", value)
}

// readable shows a row key in a report: the unit separator between identity values
// becomes a visible one.
func readable(key string) string {
	return strings.ReplaceAll(key, "\x1f", " | ")
}
