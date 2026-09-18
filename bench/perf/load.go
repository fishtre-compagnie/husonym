package perf

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/gen"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// rowsPerInsert is how many rows one INSERT carries while loading. Wide rows are split
// further so that the statement stays under the parameter limit of the server.
const rowsPerInsert = 2_000

// seedOf gives each table its own deterministic generator: the same scale always loads
// the same rows, so two passes of the perf mode compare the same work.
func seedOf(table string) *rand.Rand {
	sum := uint64(len(table))
	for _, b := range []byte(table) {
		sum = sum*1099511628211 + uint64(b)
	}
	//nolint:gosec // the rows of a dataset, not a secret: the same seed must give the same rows
	return rand.New(rand.NewPCG(sum, 0x5eed))
}

// Load creates the database of the dataset on the source and fills it. It returns how
// many rows each table received.
//
// Rows are written with foreign key checks off and in the order of the tables: the
// references are built from the row numbers, not read back.
func Load(ctx context.Context, db *sql.DB, r schema.Renderer, dataset *cases.Case, scale int) (map[string]int, error) {
	if err := gen.CreateSchema(ctx, db, r, dataset); err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("perf: %w", err)
	}
	defer conn.Close()
	for _, stmt := range []string{"SET FOREIGN_KEY_CHECKS=0", "SET UNIQUE_CHECKS=0", "SET SESSION sql_log_bin=0"} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil && !strings.Contains(err.Error(), "sql_log_bin") {
			return nil, fmt.Errorf("perf: %s: %w", stmt, err)
		}
	}

	counts := map[string]int{}
	for _, t := range dataset.Tables {
		rows := Rows(t.Name, scale)
		if rows == 0 {
			continue
		}
		if err := loadTable(ctx, conn, r, dataset.Schema(), t, rows, scale); err != nil {
			return nil, err
		}
		counts[t.Name] = rows
	}
	return counts, nil
}

// loadTable writes the rows of one table, in batches.
func loadTable(
	ctx context.Context,
	conn *sql.Conn,
	r schema.Renderer,
	database string,
	t *schema.Table,
	rows, scale int,
) error {
	columns := t.ColumnNames()
	quoted := make([]string, len(columns))
	for i, name := range columns {
		quoted[i] = r.QuoteIdent(name)
	}
	tuple := "(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
	prefix := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES ",
		r.QuoteIdent(database), r.QuoteIdent(t.Name), strings.Join(quoted, ", "))

	perInsert := min(rowsPerInsert, 60_000/len(columns))
	generator := rowGenerator(t.Name, scale)
	batch := make([]any, 0, perInsert*len(columns))
	written := 0
	for written < rows {
		batch = batch[:0]
		count := min(perInsert, rows-written)
		for i := 0; i < count; i++ {
			batch = append(batch, generator(written+i+1)...)
		}
		//nolint:gosec // identifiers come from the dataset and are quoted
		query := prefix + strings.TrimSuffix(strings.Repeat(tuple+",", count), ",")
		if _, err := conn.ExecContext(ctx, query, batch...); err != nil {
			return fmt.Errorf("perf: insert into %s: %w", t.Name, err)
		}
		written += count
	}
	return nil
}

// rowGenerator returns the function giving the values of the n-th row of a table, in
// column order. Foreign keys point at row numbers of the parent table, so a reference is
// valid without reading anything back.
func rowGenerator(table string, scale int) func(n int) []any {
	random := seedOf(table)
	clients := Rows(ClientTable, scale)
	commandeRows := Rows(CommandeTable, scale)
	referentiels := Rows(ReferentielTable, scale)
	patients := Rows(PatientTable, scale)
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	stamp := func(n int, precision string) string {
		return start.Add(time.Duration(n) * time.Minute).Format(precision)
	}

	switch table {
	case ReferentielTable:
		return func(n int) []any {
			return []any{int64(n), fmt.Sprintf("REF-%07d", n), fmt.Sprintf("Référence %d", n), int64(n % 2)}
		}
	case ClientTable:
		return func(n int) []any {
			return []any{
				int64(n), int64(n % regions), fmt.Sprintf("Nom %d", n),
				fmt.Sprintf("client%d@exemple.test", n), fmt.Sprintf("06%08d", n%100000000),
				stamp(n, "2006-01-02 15:04:05.000000"),
			}
		}
	case CommandeTable:
		return func(n int) []any {
			return []any{
				int64(n), int64(n%clients + 1), fmt.Sprintf("CMD-%09d", n),
				fmt.Sprintf("%d.%02d", n%10000, n%100), stamp(n, "2006-01-02 15:04:05"),
			}
		}
	case LigneTable:
		return func(n int) []any {
			return []any{
				int64(n), int64(n%commandeRows + 1), int64(n%referentiels + 1),
				int64(n%9 + 1), fmt.Sprintf("%d.%02d", n%500, n%100),
			}
		}
	case AdresseTable:
		return func(n int) []any {
			var facture any
			// One address in three bills another client, half of them outside the subset.
			if n%3 == 0 {
				facture = int64(random.IntN(clients) + 1)
			}
			return []any{
				int64(n), int64(n%clients + 1), facture,
				fmt.Sprintf("%d rue des Essais", n%900+1), fmt.Sprintf("Ville %d", n%500),
			}
		}
	case JournalTable:
		return func(n int) []any {
			return []any{
				int64(n), stamp(n, "2006-01-02 15:04:05.000000"),
				fmt.Sprintf("événement %d de la campagne %d", n, n%97),
			}
		}
	case DocumentTable:
		body := strings.Repeat("corps de document, ", 120)
		return func(n int) []any {
			values := []any{
				int64(n), int64(n%clients + 1), fmt.Sprintf("%s#%d", body, n),
				fmt.Sprintf(`{"numero":%d,"tags":["a","b"],"note":"document %d"}`, n, n),
				[]byte(fmt.Sprintf("%016d", n)),
				fmt.Sprintf("%d.%06d", n%100000, n%1000000),
				stamp(n, "2006-01-02 15:04:05.000000"),
			}
			for i := 1; i <= 20; i++ {
				values = append(values, fmt.Sprintf("champ %02d de %d", i, n))
			}
			return values
		}
	case PatientTable:
		return func(n int) []any {
			return []any{
				int64(n), fmt.Sprintf("DOS-%08d", n), int64(n%clients + 1), fmt.Sprintf("Patient %d", n),
			}
		}
	case VisiteTable:
		return func(n int) []any {
			return []any{
				int64(n), int64(n%patients + 1), stamp(n, "2006-01-02 15:04:05"),
				fmt.Sprintf("motif %d", n%53),
			}
		}
	default:
		return func(n int) []any { return []any{int64(n)} }
	}
}
