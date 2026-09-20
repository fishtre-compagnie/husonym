// Package oracle stores what a correct engine must produce for each bench case.
//
// The expectation lives in its own database of the source server, outside every job, so
// the tables under test keep the exact shape of their case and the expectation can be
// read with plain SQL when a result needs explaining.
package oracle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// Schema is the name of the oracle schema on the source server.
const Schema = "bench_oracle"

const maxReadableKey = 190

// ddl creates the two oracle tables. Types are the ones every database spells the same
// way: the expectation is read back as text and decoded in Go, so nothing is gained by
// asking a database for a JSON or an enumerated column here.
func ddl(r schema.Renderer) []string {
	return []string{
		r.EnsureContainerStatement(Schema),
		"CREATE TABLE IF NOT EXISTS " + table(r, "expected_rows") + " (" +
			"case_id varchar(80) NOT NULL, table_name varchar(64) NOT NULL, " +
			"row_key varchar(255) COLLATE " + r.ExactCollation() + " NOT NULL, occurrences int NOT NULL, " +
			"verdict varchar(10) NOT NULL, null_columns text NOT NULL, " +
			"PRIMARY KEY (case_id, table_name, row_key))",
		"CREATE TABLE IF NOT EXISTS " + table(r, "expected_columns") + " (" +
			"case_id varchar(80) NOT NULL, table_name varchar(64) NOT NULL, " +
			"column_name varchar(64) NOT NULL, rule varchar(40) NOT NULL, " +
			"PRIMARY KEY (case_id, table_name, column_name, rule))",
	}
}

func table(r schema.Renderer, name string) string {
	return r.QuoteIdent(Schema) + "." + r.QuoteIdent(name)
}

// RowKey identifies a row from the canonical text of its identity values (see
// CanonicalValue). Long keys are hashed; short ones stay readable in reports.
func RowKey(values []string) string {
	key := strings.Join(values, "\x1f")
	if len(key) <= maxReadableKey {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CanonicalValue is the text form rows are compared in: what the database prints for the
// value, hexadecimal for raw bytes, \N for NULL. A seed value and the same value read
// back from a database yield the same text.
func CanonicalValue(r schema.Renderer, v any, binary bool) (string, error) {
	switch x := v.(type) {
	case nil:
		return `\N`, nil
	case bool:
		return r.BoolText(x), nil
	case []byte:
		if binary {
			return "x'" + hex.EncodeToString(x) + "'", nil
		}
		return string(x), nil
	case string:
		if binary {
			return "x'" + hex.EncodeToString([]byte(x)) + "'", nil
		}
		return x, nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	default:
		return "", fmt.Errorf("oracle: unsupported value type %T", v)
	}
}

// ExpectedRow is the expectation of the source rows sharing one key.
type ExpectedRow struct {
	Key string
	// Occurrences counts the source rows with this key: more than one only for tables
	// without a key holding identical rows.
	Occurrences int
	Verdict     cases.Verdict
	NullColumns []string
}

// Writer accumulates the expectation of one case and stores it.
type Writer struct {
	caseID string
	rows   map[string]map[string]*ExpectedRow // table → key → row
}

func NewWriter(caseID string) *Writer {
	return &Writer{caseID: caseID, rows: map[string]map[string]*ExpectedRow{}}
}

// Add records the expectation of one source row.
func (w *Writer) Add(table, key string, expect cases.RowExpect) error {
	byKey := w.rows[table]
	if byKey == nil {
		byKey = map[string]*ExpectedRow{}
		w.rows[table] = byKey
	}
	existing := byKey[key]
	if existing == nil {
		byKey[key] = &ExpectedRow{Key: key, Occurrences: 1, Verdict: expect.Verdict, NullColumns: expect.NullColumns}
		return nil
	}
	if existing.Verdict != expect.Verdict || strings.Join(existing.NullColumns, ",") != strings.Join(expect.NullColumns, ",") {
		return fmt.Errorf("oracle: %s: rows of %s sharing key %q expect different outcomes", w.caseID, table, key)
	}
	existing.Occurrences++
	return nil
}

// Store replaces the stored expectation of the case.
func (w *Writer) Store(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	columnRules map[string]map[string][]cases.Rule,
) error {
	for _, stmt := range ddl(r) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("oracle: %w\n%s", err, stmt)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("oracle: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, name := range []string{"expected_rows", "expected_columns"} {
		//nolint:gosec // constant table name, quoted by the renderer; the case id is bound
		stmt := "DELETE FROM " + table(r, name) + " WHERE case_id = " + r.Placeholder(1)
		if _, err := tx.ExecContext(ctx, stmt, w.caseID); err != nil {
			return fmt.Errorf("oracle: %w", err)
		}
	}
	const batch = 500
	const rowColumns = 6
	for tableName, byKey := range w.rows {
		args := make([]any, 0, batch*rowColumns)
		flush := func() error {
			if len(args) == 0 {
				return nil
			}
			//nolint:gosec // constant statement, only the number of placeholders varies
			query := "INSERT INTO " + table(r, "expected_rows") +
				" (case_id, table_name, row_key, occurrences, verdict, null_columns) VALUES " +
				tupleList(r, len(args)/rowColumns, rowColumns)
			_, err := tx.ExecContext(ctx, query, args...)
			args = args[:0]
			return err
		}
		for _, row := range byKey {
			nullColumns := row.NullColumns
			if nullColumns == nil {
				nullColumns = []string{}
			}
			encoded, err := json.Marshal(nullColumns)
			if err != nil {
				return fmt.Errorf("oracle: %w", err)
			}
			args = append(args, w.caseID, tableName, row.Key, row.Occurrences, string(row.Verdict), string(encoded))
			if len(args) == batch*rowColumns {
				if err := flush(); err != nil {
					return fmt.Errorf("oracle: %w", err)
				}
			}
		}
		if err := flush(); err != nil {
			return fmt.Errorf("oracle: %w", err)
		}
	}
	//nolint:gosec // constant table name, quoted by the renderer; every value is bound
	insertRule := "INSERT INTO " + table(r, "expected_columns") +
		" (case_id, table_name, column_name, rule) VALUES " + tupleList(r, 1, 4)
	for tableName, columns := range columnRules {
		for column, rules := range columns {
			for _, rule := range rules {
				if _, err := tx.ExecContext(ctx, insertRule, w.caseID, tableName, column, string(rule)); err != nil {
					return fmt.Errorf("oracle: %w", err)
				}
			}
		}
	}
	return tx.Commit()
}

// tupleList renders the VALUES tuples of an insert of rows rows of columns columns.
func tupleList(r schema.Renderer, rows, columns int) string {
	tuples := make([]string, rows)
	n := 0
	for i := range tuples {
		marks := make([]string, columns)
		for j := range marks {
			n++
			marks[j] = r.Placeholder(n)
		}
		tuples[i] = "(" + strings.Join(marks, ",") + ")"
	}
	return strings.Join(tuples, ",")
}

// ReadRows loads the expected rows of one table of a case, by key.
func ReadRows(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	caseID, tableName string,
) (map[string]*ExpectedRow, error) {
	//nolint:gosec // constant table name, quoted by the renderer; every value is bound
	query := "SELECT row_key, occurrences, verdict, null_columns FROM " + table(r, "expected_rows") +
		" WHERE case_id = " + r.Placeholder(1) + " AND table_name = " + r.Placeholder(2)
	rows, err := db.QueryContext(ctx, query, caseID, tableName)
	if err != nil {
		return nil, fmt.Errorf("oracle: %w", err)
	}
	defer rows.Close()

	expected := map[string]*ExpectedRow{}
	for rows.Next() {
		var row ExpectedRow
		var verdict, nullColumns string
		if err := rows.Scan(&row.Key, &row.Occurrences, &verdict, &nullColumns); err != nil {
			return nil, fmt.Errorf("oracle: %w", err)
		}
		row.Verdict = cases.Verdict(verdict)
		if err := json.Unmarshal([]byte(nullColumns), &row.NullColumns); err != nil {
			return nil, fmt.Errorf("oracle: null columns of %s %q: %w", tableName, row.Key, err)
		}
		expected[row.Key] = &row
	}
	return expected, rows.Err()
}

// ReadColumnRules loads the column rules of one table of a case, by column.
func ReadColumnRules(
	ctx context.Context,
	db *sql.DB,
	r schema.Renderer,
	caseID, tableName string,
) (map[string][]cases.Rule, error) {
	//nolint:gosec // constant table name, quoted by the renderer; every value is bound
	query := "SELECT column_name, rule FROM " + table(r, "expected_columns") +
		" WHERE case_id = " + r.Placeholder(1) + " AND table_name = " + r.Placeholder(2) +
		" ORDER BY column_name, rule"
	rows, err := db.QueryContext(ctx, query, caseID, tableName)
	if err != nil {
		return nil, fmt.Errorf("oracle: %w", err)
	}
	defer rows.Close()

	rules := map[string][]cases.Rule{}
	for rows.Next() {
		var column, rule string
		if err := rows.Scan(&column, &rule); err != nil {
			return nil, fmt.Errorf("oracle: %w", err)
		}
		rules[column] = append(rules[column], cases.Rule(rule))
	}
	return rules, rows.Err()
}
