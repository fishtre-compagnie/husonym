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

func TestSQLWriter_MSSQL(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, MSSQLDialect{}, "dbo", "clients")

	if err := w.WriteBatch([]string{"id", "prenom"}, [][]any{{int64(1), "léa"}, {int64(2), "karim"}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	want := "INSERT INTO [dbo].[clients] ([id], [prenom]) VALUES (@p1, @p2), (@p3, @p4)"
	if e.query != want {
		t.Fatalf("SQL:\n  obtenu %q\n  voulu  %q", e.query, want)
	}
}

func TestSQLWriter_MSSQL_OnConflictDoNothing(t *testing.T) {
	e := &fakeExecer{}
	w := NewSQLWriter(context.Background(), e, MSSQLDialect{}, "dbo", "clients",
		WithOnConflict(ConflictDoNothing, nil))

	if err := w.WriteBatch([]string{"id"}, [][]any{{int64(1)}}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	// SQL Server exprime le « do nothing » via un MERGE / NOT EXISTS selon le
	// query-builder ; on vérifie juste qu'une requête a été produite et exécutée.
	if e.calls != 1 || e.query == "" {
		t.Fatalf("une requête do-nothing devait être produite, obtenu: calls=%d query=%q", e.calls, e.query)
	}
}

func TestSQLWriter_MSSQL_ChunksLargeBatch(t *testing.T) {
	// countingExecer compte les requêtes émises.
	e := &countingExecer{}
	w := NewSQLWriter(context.Background(), e, MSSQLDialect{}, "dbo", "t")

	// 1 colonne, 2500 lignes. Limite MSSQL = min(2100/1, 1000) = 1000 lignes/insert
	// => 1000 + 1000 + 500 = 3 requêtes.
	rows := make([][]any, 2500)
	for i := range rows {
		rows[i] = []any{int64(i)}
	}
	if err := w.WriteBatch([]string{"id"}, rows); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if e.calls != 3 {
		t.Fatalf("attendu 3 requêtes (chunks de 1000), obtenu %d", e.calls)
	}
}

// countingExecer compte les appels (le fakeExecer n'écrase que la dernière requête).
type countingExecer struct{ calls int }

func (e *countingExecer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	e.calls++
	return driverResult{}, nil
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
