package runprivileges_activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

// PostgreSQL is asked about the account itself rather than read from the privilege
// tables. information_schema.table_privileges lists what was granted to the account by
// name: a superuser, the owner of a table, or an account holding its rights through a
// role would show nothing there, and a run it could perfectly do would be stopped.
// has_table_privilege answers the question the run will ask, the way it will ask it.

// postgresDb is what the PostgreSQL checks need from a connection.
type postgresDb interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// checkPostgresSource: a source only needs to be read. A read-only server is fine.
func checkPostgresSource(
	ctx context.Context,
	db postgresDb,
	name string,
	tables []*sqlmanager_shared.SchemaTable,
) ([]string, error) {
	missing, err := postgresMissingPrivileges(ctx, db, tables, sourcePrivileges, false)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of source %q: %w", name, err)
	}
	return describeMissing("source", name, "read", missing), nil
}

// checkPostgresDestination: a destination must accept writes, on the server and on every
// table; and when Athanor writes it, the account must be allowed to suspend foreign keys.
func checkPostgresDestination(
	ctx context.Context,
	db postgresDb,
	name string,
	tables []*sqlmanager_shared.SchemaTable,
	createsTables bool,
	suspendsForeignKeys bool,
) ([]string, error) {
	// transaction_read_only is on for a standby and for a database set to
	// default_transaction_read_only: one question covers both.
	var readOnly string
	if err := db.QueryRowContext(ctx, "SELECT current_setting('transaction_read_only')").Scan(&readOnly); err != nil {
		return nil, fmt.Errorf("unable to tell whether destination %q accepts writes: %w", name, err)
	}
	if readOnly == "on" {
		return []string{fmt.Sprintf("destination %q refuses writes (transaction_read_only is on: a standby, "+
			"or a database set to default_transaction_read_only): a destination must be a standalone or "+
			"primary server accepting writes", name)}, nil
	}

	missing, err := postgresMissingPrivileges(ctx, db, tables, destinationPrivileges, createsTables)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
	}
	findings := describeMissing("destination", name, "write", missing)

	if suspendsForeignKeys {
		finding, err := probeForeignKeySuspension(ctx, db, name)
		if err != nil {
			return nil, err
		}
		if finding != "" {
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

// postgresMissingPrivileges returns, per table ("schema.table"), the privileges the account
// lacks on it, USAGE on its schema included. A table that does not exist yet is left out
// when the run creates it, and reported as missing otherwise.
func postgresMissingPrivileges(
	ctx context.Context,
	db postgresDb,
	tables []*sqlmanager_shared.SchemaTable,
	privileges []string,
	createsTables bool,
) (map[string][]string, error) {
	// The lists travel as JSON, which PostgreSQL unfolds itself: an array parameter is
	// something only some drivers bind.
	type tableRef struct {
		Schema string `json:"schema_name"`
		Table  string `json:"table_name"`
	}
	refs := make([]tableRef, len(tables))
	for i, t := range tables {
		refs[i] = tableRef{Schema: t.Schema, Table: t.Table}
	}
	tablesJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, err
	}
	privilegesJSON, err := json.Marshal(append(slices.Clone(privileges), "USAGE"))
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT t.schema_name || '.' || t.table_name, p.privilege
FROM jsonb_to_recordset($1::jsonb) AS t(schema_name text, table_name text)
CROSS JOIN jsonb_array_elements_text($2::jsonb) AS p(privilege)
CROSS JOIN LATERAL (
  SELECT to_regclass(quote_ident(t.schema_name) || '.' || quote_ident(t.table_name)) AS rel,
         to_regnamespace(quote_ident(t.schema_name)) AS nsp
) r
WHERE CASE
  WHEN p.privilege = 'USAGE' THEN r.nsp IS NOT NULL AND NOT has_schema_privilege(r.nsp, 'USAGE')
  WHEN r.rel IS NULL THEN NOT $3::bool
  ELSE NOT has_table_privilege(r.rel, p.privilege)
END
ORDER BY 1, 2`, string(tablesJSON), string(privilegesJSON), createsTables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	missing := map[string][]string{}
	for rows.Next() {
		var table, privilege string
		if err := rows.Scan(&table, &privilege); err != nil {
			return nil, err
		}
		if privilege == "USAGE" {
			privilege = "USAGE on its schema"
		}
		missing[table] = append(missing[table], privilege)
	}
	return missing, rows.Err()
}

func describeMissing(role, name, verb string, missing map[string][]string) []string {
	findings := make([]string, 0, len(missing))
	for table, privileges := range missing {
		findings = append(findings, fmt.Sprintf("%s %q cannot %s %s (missing %s)",
			role, name, verb, table, strings.Join(privileges, ", ")))
	}
	slices.Sort(findings)
	return findings
}

// probeForeignKeySuspension tries, in a transaction rolled back at once, the statement
// Athanor writes each page with. Whether an account may set session_replication_role
// depends on SUPERUSER, or from PostgreSQL 15 on on GRANT SET ON PARAMETER; a managed
// service has no real superuser, and reading rolsuper would say no where the grant says
// yes. Trying it is the one answer that holds everywhere.
func probeForeignKeySuspension(ctx context.Context, db postgresDb, name string) (string, error) {
	disable, _, _ := sqlio.PostgresDialect{}.ForeignKeyChecksStatements()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("unable to open a transaction on destination %q: %w", name, err)
	}
	_, probeErr := tx.ExecContext(ctx, disable)
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return "", fmt.Errorf("unable to roll back the probe on destination %q: %w", name, err)
	}
	if probeErr == nil {
		return "", nil
	}
	var account string
	if err := db.QueryRowContext(ctx, "SELECT current_user").Scan(&account); err != nil {
		account = "<account>"
	}
	return fmt.Sprintf("destination %q cannot suspend foreign keys, which Athanor writes each table in one "+
		"pass with (%v): make the account a superuser, or from PostgreSQL 15 on run "+
		"GRANT SET ON PARAMETER session_replication_role TO %s", name, probeErr, quoteRole(account)), nil
}

func quoteRole(role string) string {
	return `"` + strings.ReplaceAll(role, `"`, `""`) + `"`
}
