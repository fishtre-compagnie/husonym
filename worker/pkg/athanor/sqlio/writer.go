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

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
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
	// MaxRowsPerInsert borne le nombre de lignes d'un INSERT multi-lignes selon
	// les limites du SGBD (nb max de paramètres, de tuples VALUES…), en fonction
	// du nombre de colonnes. Le writer découpe les batches en conséquence.
	MaxRowsPerInsert(numCols int) int
}

// PostgresDialect : placeholders $1, $2… et identifiants entre guillemets doubles.
type PostgresDialect struct{}

func (PostgresDialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }
func (PostgresDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (PostgresDialect) Driver() string { return sqlmanager_shared.PostgresDriver }

// PostgreSQL : limite de 65535 paramètres liés par requête.
func (PostgresDialect) MaxRowsPerInsert(numCols int) int { return maxRowsForParams(65535, numCols) }

// MySQLDialect : placeholders ? et identifiants entre accents graves.
type MySQLDialect struct{}

func (MySQLDialect) Placeholder(int) string { return "?" }
func (MySQLDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
func (MySQLDialect) Driver() string { return sqlmanager_shared.MysqlDriver }

// MySQL : limite de 65535 paramètres (placeholders) par requête préparée.
func (MySQLDialect) MaxRowsPerInsert(numCols int) int { return maxRowsForParams(65535, numCols) }

// MSSQLDialect : SQL Server — placeholders @p1, @p2… (ordinaux, mappés
// positionnellement par go-mssqldb) et identifiants entre crochets.
type MSSQLDialect struct{}

func (MSSQLDialect) Placeholder(n int) string { return "@p" + strconv.Itoa(n) }
func (MSSQLDialect) QuoteIdent(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}
func (MSSQLDialect) Driver() string { return sqlmanager_shared.MssqlDriver }

// SQL Server : max 2100 paramètres par requête ET max 1000 tuples par clause
// VALUES. On budgète 2000 paramètres (marge sous 2100 : la limite inclut un léger
// overhead interne, exactement 2100 est déjà refusé).
func (MSSQLDialect) MaxRowsPerInsert(numCols int) int {
	byParams := maxRowsForParams(2000, numCols)
	if byParams > 1000 {
		return 1000
	}
	return byParams
}

// maxRowsForParams renvoie le nombre de lignes tenant sous une limite de
// paramètres, au moins 1 (une ligne large peut à elle seule dépasser la limite —
// on l'émet quand même et on laisse le SGBD trancher).
func maxRowsForParams(maxParams, numCols int) int {
	if numCols <= 0 {
		return 1
	}
	if r := maxParams / numCols; r > 0 {
		return r
	}
	return 1
}

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

// WriteBatch insère toutes les lignes du lot, en découpant si besoin pour
// respecter les limites du SGBD (MaxRowsPerInsert : nb de paramètres/tuples).
// Chaque sous-lot part en INSERT « maison » (chemin rapide) ou, si une stratégie
// de conflit est configurée, via le query-builder partagé.
func (w *SQLWriter) WriteBatch(columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	chunk := w.dialect.MaxRowsPerInsert(len(columns))
	if chunk <= 0 || chunk > len(rows) {
		chunk = len(rows)
	}

	for start := 0; start < len(rows); start += chunk {
		end := start + chunk
		if end > len(rows) {
			end = len(rows)
		}
		sub := rows[start:end]

		var err error
		if w.conflict != ConflictNone {
			err = w.writeBatchOnConflict(columns, sub)
		} else {
			err = w.writeBatchPlain(columns, sub)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// writeBatchPlain émet un unique INSERT multi-lignes (sans gestion de conflit).
// L'appelant garantit que len(rows) respecte déjà MaxRowsPerInsert.
func (w *SQLWriter) writeBatchPlain(columns []string, rows [][]any) error {
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
