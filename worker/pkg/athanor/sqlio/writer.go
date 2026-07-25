package sqlio

// writer.go — un RowWriter qui écrit réellement dans une destination SQL, par
// INSERT groupés, via database/sql standard. Driver-agnostique : on abstrait la
// destination par Execer (que *sql.DB et *sql.Tx satisfont) et on isole les
// spécificités de dialecte (placeholders, quoting) derrière Dialect.
//
// Gestion des conflits de clé (RFC §7.4 — rejeu idempotent) : par défaut un INSERT
// sec (le plus rapide). Si une stratégie onConflict est configurée sur la
// destination du job, on délègue la construction de la requête au query-builder
// partagé (pkg/query-builder), déjà utilisé par le chemin Benthos — même
// sémantique « do nothing » / « do update » (upsert), sans dupliquer le SQL.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	sqlmanager_shared "github.com/Groupe-Hevea/neosync/backend/pkg/sqlmanager/shared"
	querybuilder "github.com/Groupe-Hevea/neosync/worker/pkg/query-builder"
)

// Execer est le sous-ensemble de database/sql suffisant pour écrire.
// *sql.DB et *sql.Tx le satisfont tels quels.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Dialect isole les différences de syntaxe entre SGBD.
type Dialect interface {
	Placeholder(n int) string // n est 1-indexé
	QuoteIdent(s string) string
	Driver() string // identifiant driver attendu par le query-builder (pgx, mysql…)
}

// PostgresDialect : placeholders $1, $2… et identifiants entre guillemets doubles.
type PostgresDialect struct{}

func (PostgresDialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }
func (PostgresDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (PostgresDialect) Driver() string { return sqlmanager_shared.PostgresDriver }

// MySQLDialect : placeholders ? et identifiants entre accents graves.
type MySQLDialect struct{}

func (MySQLDialect) Placeholder(int) string { return "?" }
func (MySQLDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
func (MySQLDialect) Driver() string { return sqlmanager_shared.MysqlDriver }

// ConflictAction décrit quoi faire quand une ligne insérée entre en conflit avec
// une clé (PK/unique) existante en destination.
type ConflictAction int

const (
	// ConflictNone : INSERT simple (comportement historique). Un conflit échoue.
	ConflictNone ConflictAction = iota
	// ConflictDoNothing : on ignore la ligne en conflit (INSERT IGNORE / ON CONFLICT DO NOTHING).
	ConflictDoNothing
	// ConflictDoUpdate : upsert — on met à jour la ligne existante (ON DUPLICATE
	// KEY UPDATE / ON CONFLICT DO UPDATE). Pour Postgres, requiert les colonnes de
	// conflit (PKColumns) ; MySQL n'en a pas besoin (déclenché sur toute clé unique).
	ConflictDoUpdate
)

// WriterOption configure un SQLWriter à la construction.
type WriterOption func(*SQLWriter)

// WithOnConflict fixe la stratégie de conflit. pkColumns n'est utile qu'en
// ConflictDoUpdate sous Postgres (colonnes de la contrainte de conflit).
func WithOnConflict(action ConflictAction, pkColumns []string) WriterOption {
	return func(w *SQLWriter) {
		w.conflict = action
		w.pkColumns = pkColumns
	}
}

// WithLogger surcharge le logger (par défaut slog.Default()).
func WithLogger(l *slog.Logger) WriterOption {
	return func(w *SQLWriter) {
		if l != nil {
			w.logger = l
		}
	}
}

// SQLWriter écrit des batches dans une table via INSERT groupé (avec gestion
// optionnelle des conflits de clé).
type SQLWriter struct {
	ctx           context.Context
	db            Execer
	dialect       Dialect
	schema, table string // bruts (non quotés) — requis par le query-builder
	ref           string // référence de table déjà quotée (schema.table)
	conflict      ConflictAction
	pkColumns     []string
	logger        *slog.Logger
}

// NewSQLWriter construit un writer. schema peut être vide (table non qualifiée).
func NewSQLWriter(ctx context.Context, db Execer, dialect Dialect, schema, table string, opts ...WriterOption) *SQLWriter {
	ref := dialect.QuoteIdent(table)
	if schema != "" {
		ref = dialect.QuoteIdent(schema) + "." + ref
	}
	w := &SQLWriter{
		ctx:     ctx,
		db:      db,
		dialect: dialect,
		schema:  schema,
		table:   table,
		ref:     ref,
		logger:  slog.Default(),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// WriteBatch insère toutes les lignes du lot. Sans stratégie de conflit, un seul
// INSERT groupé « maison » (chemin rapide historique). Avec stratégie, la requête
// est construite par le query-builder partagé.
func (w *SQLWriter) WriteBatch(columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	if w.conflict != ConflictNone {
		return w.writeBatchOnConflict(columns, rows)
	}

	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = w.dialect.QuoteIdent(c)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES ", w.ref, strings.Join(quoted, ", "))

	args := make([]any, 0, len(rows)*len(columns))
	ph := 1
	for r, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("sqlio: ligne %d a %d valeurs, %d colonnes attendues", r, len(row), len(columns))
		}
		if r > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for c := range columns {
			if c > 0 {
				b.WriteString(", ")
			}
			b.WriteString(w.dialect.Placeholder(ph))
			ph++
			args = append(args, row[c])
		}
		b.WriteByte(')')
	}

	if _, err := w.db.ExecContext(w.ctx, b.String(), args...); err != nil {
		return fmt.Errorf("sqlio: INSERT dans %s: %w", w.ref, err)
	}
	return nil
}

// writeBatchOnConflict construit l'INSERT ... ON CONFLICT via le query-builder
// partagé (même sémantique que le chemin Benthos) puis l'exécute.
func (w *SQLWriter) writeBatchOnConflict(columns []string, rows [][]any) error {
	var iopts []querybuilder.InsertOption
	switch w.conflict {
	case ConflictDoNothing:
		iopts = append(iopts, querybuilder.WithOnConflictDoNothing())
	case ConflictDoUpdate:
		iopts = append(iopts, querybuilder.WithOnConflictDoUpdate(w.pkColumns))
	}

	builder, err := querybuilder.GetInsertBuilder(w.logger, w.dialect.Driver(), w.schema, w.table, nil, iopts...)
	if err != nil {
		return fmt.Errorf("sqlio: constructeur d'insert (%s): %w", w.dialect.Driver(), err)
	}

	recs := make([]map[string]any, len(rows))
	for i, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("sqlio: ligne %d a %d valeurs, %d colonnes attendues", i, len(row), len(columns))
		}
		m := make(map[string]any, len(columns))
		for j, c := range columns {
			m[c] = row[j]
		}
		recs[i] = m
	}

	query, args, err := builder.BuildInsertQuery(recs)
	if err != nil {
		return fmt.Errorf("sqlio: construction INSERT ... ON CONFLICT dans %s: %w", w.ref, err)
	}
	if _, err := w.db.ExecContext(w.ctx, query, args...); err != nil {
		return fmt.Errorf("sqlio: INSERT ... ON CONFLICT dans %s: %w", w.ref, err)
	}
	return nil
}

var _ RowWriter = (*SQLWriter)(nil)
