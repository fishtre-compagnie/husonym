package sqlio

// writer.go — a RowWriter that really writes to a SQL destination, through
// batched INSERTs over the standard database/sql. Driver-agnostic: the destination
// is abstracted by Execer (satisfied by both *sql.DB and *sql.Tx) and dialect
// specifics (placeholders, quoting) are isolated behind Dialect.
//
// Key conflict handling (RFC §7.4 — idempotent replay): a plain INSERT by default,
// the fastest path. When an onConflict policy is configured on the job's
// destination, query building is delegated to the shared query-builder
// (pkg/query-builder) already used by the Benthos path — same "do nothing" /
// "do update" (upsert) semantics, without duplicating the SQL.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"time"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
)

// Execer est le sous-ensemble de database/sql suffisant pour écrire.
// *sql.DB et *sql.Tx le satisfont tels quels.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// TxBeginner opens a transaction, pinned to a single connection of the pool.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
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
	// ForeignKeyChecksStatements returns the session statements that turn foreign key
	// checks off and back on, and false when the database offers none usable by a
	// regular user.
	ForeignKeyChecksStatements() (disable, enable string, ok bool)
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

// PostgreSQL : session_replication_role exige un superutilisateur, et les
// contraintes DEFERRABLE dépendent du schéma. Aucune voie générale n'est disponible.
func (PostgresDialect) ForeignKeyChecksStatements() (disable, enable string, ok bool) {
	return "", "", false
}

// MySQLDialect : placeholders ? et identifiants entre accents graves.
type MySQLDialect struct{}

func (MySQLDialect) Placeholder(int) string { return "?" }
func (MySQLDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
func (MySQLDialect) Driver() string { return sqlmanager_shared.MysqlDriver }

// MySQL : limite de 65535 paramètres (placeholders) par requête préparée.
func (MySQLDialect) MaxRowsPerInsert(numCols int) int { return maxRowsForParams(65535, numCols) }

// MySQL : variable de session, modifiable sans privilège particulier.
func (MySQLDialect) ForeignKeyChecksStatements() (disable, enable string, ok bool) {
	return "SET FOREIGN_KEY_CHECKS=0", "SET FOREIGN_KEY_CHECKS=1", true
}

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

// SQL Server : NOCHECK CONSTRAINT exige le droit ALTER sur la table.
func (MSSQLDialect) ForeignKeyChecksStatements() (disable, enable string, ok bool) {
	return "", "", false
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

// ConflictAction describes what to do when an inserted row collides with an
// existing key (PK/unique) at the destination.
type ConflictAction int

const (
	// ConflictNone: plain INSERT (historical behavior). A collision fails.
	ConflictNone ConflictAction = iota
	// ConflictDoNothing: skip the colliding row (INSERT IGNORE / ON CONFLICT DO NOTHING).
	ConflictDoNothing
	// ConflictDoUpdate: upsert — update the existing row (ON DUPLICATE KEY UPDATE /
	// ON CONFLICT DO UPDATE). Postgres needs the conflict target columns
	// (PKColumns); MySQL does not (it fires on any unique key).
	ConflictDoUpdate
)

// WriterOption configure un SQLWriter à la construction.
type WriterOption func(*SQLWriter)

// WithOnConflict sets the conflict policy. pkColumns only matters for
// ConflictDoUpdate on Postgres (the columns of the conflict target).
func WithOnConflict(action ConflictAction, pkColumns []string) WriterOption {
	return func(w *SQLWriter) {
		w.conflict = action
		w.pkColumns = pkColumns
	}
}

// WithForeignKeyChecksDisabled writes each batch in a transaction with foreign key
// checks turned off, so a table can be written in a single pass whatever the order of
// its rows and of the tables it references. The dialect must support it.
func WithForeignKeyChecksDisabled(db TxBeginner) WriterOption {
	return func(w *SQLWriter) {
		w.txBeginner = db
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
	txBeginner    TxBeginner // non nil : lots écrits en transaction, FK désactivées
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

// WriteBatch inserts every row of the batch, splitting it when needed to stay
// within the database limits (MaxRowsPerInsert: parameter/tuple count). Each chunk
// goes out as an in-house INSERT (the fast path) or, when a conflict policy is
// configured, through the shared query-builder.
func (w *SQLWriter) WriteBatch(columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		for i, v := range row {
			converted, err := toDriverValue(v)
			if err != nil {
				return fmt.Errorf("sqlio: colonne %q: %w", columns[min(i, len(columns)-1)], err)
			}
			row[i] = converted
		}
	}
	if w.txBeginner == nil {
		return w.writeChunks(w.db, columns, rows)
	}
	return w.writeWithoutForeignKeyChecks(columns, rows)
}

// writeWithoutForeignKeyChecks writes the batch in one transaction with foreign key
// checks off. The setting belongs to the connection, which returns to the pool after
// the transaction: checks are always turned back on first, even when a write fails.
func (w *SQLWriter) writeWithoutForeignKeyChecks(columns []string, rows [][]any) (err error) {
	disable, enable, ok := w.dialect.ForeignKeyChecksStatements()
	if !ok {
		return fmt.Errorf("sqlio: %s ne permet pas de désactiver les clés étrangères", w.dialect.Driver())
	}
	tx, err := w.txBeginner.BeginTx(w.ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlio: ouverture de transaction sur %s: %w", w.ref, err)
	}
	if _, err := tx.ExecContext(w.ctx, disable); err != nil {
		return errors.Join(fmt.Errorf("sqlio: désactivation des clés étrangères: %w", err), tx.Rollback())
	}
	defer func() {
		if _, rerr := tx.ExecContext(w.ctx, enable); rerr != nil {
			err = errors.Join(err, fmt.Errorf("sqlio: réactivation des clés étrangères: %w", rerr))
		}
		if err != nil {
			err = errors.Join(err, tx.Rollback())
			return
		}
		if cerr := tx.Commit(); cerr != nil {
			err = fmt.Errorf("sqlio: validation de la transaction sur %s: %w", w.ref, cerr)
		}
	}()
	return w.writeChunks(tx, columns, rows)
}

// writeChunks splits the batch to stay within the database limits and writes each chunk.
func (w *SQLWriter) writeChunks(db Execer, columns []string, rows [][]any) error {
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
			err = w.writeBatchOnConflict(db, columns, sub)
		} else {
			err = w.writeBatchPlain(db, columns, sub)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// writeBatchPlain emits a single multi-row INSERT (no conflict handling).
// The caller guarantees that len(rows) already honors MaxRowsPerInsert.
func (w *SQLWriter) writeBatchPlain(db Execer, columns []string, rows [][]any) error {
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

	if _, err := db.ExecContext(w.ctx, b.String(), args...); err != nil {
		return fmt.Errorf("sqlio: INSERT dans %s: %w", w.ref, err)
	}
	return nil
}

// writeBatchOnConflict construit l'INSERT ... ON CONFLICT via le query-builder
// partagé (même sémantique que le chemin Benthos) puis l'exécute.
func (w *SQLWriter) writeBatchOnConflict(db Execer, columns []string, rows [][]any) error {
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
	if _, err := db.ExecContext(w.ctx, query, args...); err != nil {
		return fmt.Errorf("sqlio: INSERT ... ON CONFLICT dans %s: %w", w.ref, err)
	}
	return nil
}

// toDriverValue turns the structured values a transformer can return (a JavaScript
// object or array, for instance) into JSON, which database/sql cannot bind as is.
// Scalars and raw bytes are left untouched.
func toDriverValue(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch v.(type) {
	case []byte, time.Time, driver.Valuer:
		return v, nil
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		bits, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("conversion en JSON: %w", err)
		}
		return bits, nil
	default:
		return v, nil
	}
}

var _ RowWriter = (*SQLWriter)(nil)
