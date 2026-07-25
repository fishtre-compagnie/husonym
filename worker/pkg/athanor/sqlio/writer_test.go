package sqlio

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// fakeExecer capture la dernière requête et ses arguments (pas de base réelle).
type fakeExecer struct {
	query string
	args  []any
	calls int
}

func (e *fakeExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.query = query
	e.args = args
	e.calls++
	return driverResult{}, nil
}

type driverResult struct{}

func (driverResult) LastInsertId() (int64, error) { return 0, nil }
func (driverResult) RowsAffected() (int64, error) { return 0, nil }

func TestSQLWriter_Postgres(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, PostgresDialect{}, "public", "clients")

	rows := [][]any{
		{int64(1), "léa"},
		{int64(2), "karim"},
	}
	if err := w.WriteBatch([]string{"id", "prenom"}, rows); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	want := `INSERT INTO "public"."clients" ("id", "prenom") VALUES ($1, $2), ($3, $4)`
	if e.query != want {
		t.Fatalf("SQL:\n  obtenu %q\n  voulu  %q", e.query, want)
	}
	if len(e.args) != 4 || e.args[0] != int64(1) || e.args[3] != "karim" {
		t.Fatalf("args inattendus: %#v", e.args)
	}
}

func TestSQLWriter_MySQL(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, MySQLDialect{}, "", "clients")

	if err := w.WriteBatch([]string{"id"}, [][]any{{int64(1)}, {int64(2)}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	want := "INSERT INTO `clients` (`id`) VALUES (?), (?)"
	if e.query != want {
		t.Fatalf("SQL:\n  obtenu %q\n  voulu  %q", e.query, want)
	}
}

func TestSQLWriter_MySQL_OnConflictDoNothing(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, MySQLDialect{}, "demo", "clients",
		WithOnConflict(ConflictDoNothing, nil))

	if err := w.WriteBatch([]string{"id"}, [][]any{{int64(1)}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if !strings.Contains(e.query, "INSERT IGNORE") {
		t.Fatalf("MySQL do-nothing attendu (INSERT IGNORE), obtenu: %q", e.query)
	}
}

func TestSQLWriter_MySQL_OnConflictDoUpdate(t *testing.T) {
	e := &fakeExecer{}
	// MySQL n'a pas besoin des colonnes PK : ON DUPLICATE KEY UPDATE se déclenche
	// sur toute clé unique en conflit.
	w := NewSQLWriter(context.Background(), e, MySQLDialect{}, "demo", "clients",
		WithOnConflict(ConflictDoUpdate, nil))

	if err := w.WriteBatch([]string{"id", "prenom"}, [][]any{{int64(1), "léa"}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if !strings.Contains(e.query, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("MySQL upsert attendu (ON DUPLICATE KEY UPDATE), obtenu: %q", e.query)
	}
}

func TestSQLWriter_Postgres_OnConflictDoNothing(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, PostgresDialect{}, "public", "clients",
		WithOnConflict(ConflictDoNothing, nil))

	if err := w.WriteBatch([]string{"id"}, [][]any{{int64(1)}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if !strings.Contains(e.query, "ON CONFLICT") || !strings.Contains(e.query, "DO NOTHING") {
		t.Fatalf("Postgres do-nothing attendu (ON CONFLICT DO NOTHING), obtenu: %q", e.query)
	}
}

func TestSQLWriter_EmptyBatchNoOp(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, PostgresDialect{}, "", "t")
	if err := w.WriteBatch([]string{"id"}, nil); err != nil {
		t.Fatalf("WriteBatch vide: %v", err)
	}
	if e.calls != 0 {
		t.Fatal("un batch vide ne doit déclencher aucune requête")
	}
}

func TestSQLWriter_ArityMismatch(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, PostgresDialect{}, "", "t")
	err := w.WriteBatch([]string{"a", "b"}, [][]any{{1}}) // 1 valeur, 2 colonnes
	if err == nil {
		t.Fatal("un nombre de valeurs incohérent doit échouer")
	}
}
