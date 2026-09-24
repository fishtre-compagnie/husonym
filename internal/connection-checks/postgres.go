package connectionchecks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	sqlmanager_postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

// PostgreSQL is asked about the account itself rather than read from the privilege
// tables. information_schema.table_privileges lists what was granted to the account by
// name: a superuser, the owner of a table, or an account holding its rights through a
// role would show nothing there, and a run it could perfectly do would be stopped.
// has_table_privilege answers the question the run will ask, the way it will ask it.

// Privileges a role needs on every table of the job.
var (
	sourcePrivileges      = []string{"SELECT"}
	destinationPrivileges = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
)

// usageOnSchema is how a missing USAGE on the schema of a table is named.
const usageOnSchema = "USAGE on its schema"

// absentTable marks, among the privileges missing, a table that is not there.
const absentTable = "(absent)"

// checkPostgresSource: a source only needs to be read. A read-only server is fine.
func checkPostgresSource(ctx context.Context, db Db, name string, tables []*Table) ([]*Finding, error) {
	missing, err := postgresMissingPrivileges(ctx, db, tables, sourcePrivileges, false)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of source %q: %w", name, err)
	}
	if len(missing) == 0 {
		return nil, nil
	}
	return describeMissing(CheckReadable, "source", name, "read", missing, postgresAccount(ctx, db)), nil
}

// checkPostgresDestination: a destination must accept writes, on the server and on every
// table; emptying a table before writing it takes TRUNCATE; the run disables the triggers
// of the tables, which takes their owner; and when Athanor writes, the account must be
// allowed to suspend foreign keys.
func checkPostgresDestination(
	ctx context.Context,
	db Db,
	name string,
	tables []*Table,
	options DestinationOptions,
) ([]*Finding, error) {
	// transaction_read_only is on for a standby and for a database set to
	// default_transaction_read_only: one question covers both.
	var readOnly string
	if err := db.QueryRowContext(ctx, "SELECT current_setting('transaction_read_only')").Scan(&readOnly); err != nil {
		return nil, fmt.Errorf("unable to tell whether destination %q accepts writes: %w", name, err)
	}
	if readOnly == "on" {
		return []*Finding{blocking(CheckServerWritable, "", nil, "", fmt.Sprintf(
			"destination %q refuses writes (transaction_read_only is on: a standby, "+
				"or a database set to default_transaction_read_only): a destination must be a standalone or "+
				"primary server accepting writes", name))}, nil
	}

	missing, err := postgresMissingPrivileges(ctx, db, tables, destinationPrivileges, options.CreatesTables)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
	}
	var truncate []postgresTable
	if options.Truncates {
		missingTruncate, err := postgresMissingPrivileges(ctx, db, tables, []string{"TRUNCATE"}, options.CreatesTables)
		if err != nil {
			return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
		}
		for table, privileges := range missingTruncate {
			if slices.Contains(privileges, "TRUNCATE") {
				truncate = append(truncate, table)
			}
		}
	}
	triggers, err := postgresTriggersNotOwned(ctx, db, name, tables)
	if err != nil {
		return nil, err
	}
	var foreignKeys error
	if options.SuspendsForeignKeys {
		if foreignKeys, err = probeForeignKeySuspension(ctx, db, name); err != nil {
			return nil, err
		}
	}
	if len(missing) == 0 && len(truncate) == 0 && len(triggers) == 0 && foreignKeys == nil {
		return nil, nil
	}

	account := postgresAccount(ctx, db)
	findings := describeMissing(CheckWritable, "destination", name, "write", missing, account)
	for _, table := range truncate {
		findings = append(findings, blocking(CheckTruncate, table.String(), []string{"TRUNCATE"},
			postgresGrantOnTable([]string{"TRUNCATE"}, table, account),
			fmt.Sprintf("destination %q cannot empty %s before writing it (missing TRUNCATE)", name, table)))
	}
	for _, trigger := range triggers {
		// No remedy: disabling a trigger takes the owner's role, often postgres or the
		// service's master user; granting it would hand the account all that role owns.
		findings = append(findings, blocking(CheckTriggers, trigger.table, []string{"ownership of the table"}, "",
			fmt.Sprintf("destination %q cannot take the triggers of %s out of the way of "+
				"the run: disabling a trigger takes the owner of the table (%s), a member of its role, or a superuser",
				name, trigger.table, trigger.owner)))
	}
	if foreignKeys != nil {
		remedy := ""
		if postgresVersion(ctx, db) >= 150000 {
			// GRANT SET ON PARAMETER came with PostgreSQL 15; before, only a superuser may.
			remedy = fmt.Sprintf("GRANT SET ON PARAMETER session_replication_role TO %s;", account)
		}
		findings = append(findings, blocking(CheckForeignKeySuspension, "", []string{"SET on session_replication_role"},
			remedy,
			fmt.Sprintf("destination %q cannot suspend foreign keys, which Athanor writes each table in one "+
				"pass with (%v): make the account a superuser, or from PostgreSQL 15 on run "+
				"GRANT SET ON PARAMETER session_replication_role TO %s", name, foreignKeys, account)))
	}
	sortFindings(findings)
	return findings, nil
}

// postgresTable names a table the way PostgreSQL has it, schema and name apart.
type postgresTable struct {
	schema, table string
}

func (t postgresTable) String() string { return t.schema + "." + t.table }

// quoted is the table as a statement names it.
func (t postgresTable) quoted() string {
	return sqlmanager_postgres.EscapePgColumn(t.schema) + "." + sqlmanager_postgres.EscapePgColumn(t.table)
}

// tablesJSON lists tables as JSON, which PostgreSQL unfolds itself: an array parameter is
// something only some drivers bind.
func tablesJSON(tables []*Table) (string, error) {
	type tableRef struct {
		Schema string `json:"schema_name"`
		Table  string `json:"table_name"`
	}
	refs := make([]tableRef, len(tables))
	for i, t := range tables {
		refs[i] = tableRef{Schema: t.Schema, Table: t.Table}
	}
	bits, err := json.Marshal(refs)
	return string(bits), err
}

// postgresMissingPrivileges returns, per table, the privileges the account lacks on it,
// USAGE on its schema included. A table that does not exist yet is left out when the run
// creates it, and reported as missing otherwise.
func postgresMissingPrivileges(
	ctx context.Context,
	db Db,
	tables []*Table,
	privileges []string,
	createsTables bool,
) (map[postgresTable][]string, error) {
	tablesList, err := tablesJSON(tables)
	if err != nil {
		return nil, err
	}
	privilegesJSON, err := json.Marshal(append(slices.Clone(privileges), "USAGE"))
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT t.schema_name, t.table_name, p.privilege
FROM jsonb_to_recordset($1::jsonb) AS t(schema_name text, table_name text)
CROSS JOIN jsonb_array_elements_text($2::jsonb) AS p(privilege)
CROSS JOIN LATERAL (
  SELECT to_regclass(quote_ident(t.schema_name) || '.' || quote_ident(t.table_name)) AS rel,
         to_regnamespace(quote_ident(t.schema_name)) AS nsp
) r
WHERE CASE
  WHEN p.privilege = 'USAGE' THEN r.nsp IS NOT NULL AND NOT has_schema_privilege(r.nsp, 'USAGE')
  WHEN r.rel IS NULL THEN FALSE
  ELSE NOT has_table_privilege(r.rel, p.privilege)
END
UNION
-- A table that is not there, and that the run does not create, lacks nothing: it is absent.
SELECT t.schema_name, t.table_name, '`+absentTable+`'
FROM jsonb_to_recordset($1::jsonb) AS t(schema_name text, table_name text)
WHERE NOT $3::bool AND to_regclass(quote_ident(t.schema_name) || '.' || quote_ident(t.table_name)) IS NULL
ORDER BY 1, 2, 3`, tablesList, string(privilegesJSON), createsTables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	missing := map[postgresTable][]string{}
	for rows.Next() {
		var table postgresTable
		var privilege string
		if err := rows.Scan(&table.schema, &table.table, &privilege); err != nil {
			return nil, err
		}
		if privilege == "USAGE" {
			privilege = usageOnSchema
		}
		missing[table] = append(missing[table], privilege)
	}
	return missing, rows.Err()
}

// triggerNotOwned is a table holding a trigger the account cannot disable, and its owner.
type triggerNotOwned struct {
	table, owner string
}

// postgresTriggersNotOwned lists the tables holding a trigger that can fire and that the
// account cannot disable: ALTER TABLE … DISABLE TRIGGER takes the owner of the table, a
// member of its role, or a superuser — no privilege grants it. The triggers enforcing
// foreign keys are internal, and never touched.
func postgresTriggersNotOwned(ctx context.Context, db Db, name string, tables []*Table) ([]triggerNotOwned, error) {
	tablesList, err := tablesJSON(tables)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT n.nspname || '.' || c.relname, pg_get_userbyid(c.relowner)
FROM jsonb_to_recordset($1::jsonb) AS j(schema_name text, table_name text)
JOIN pg_catalog.pg_namespace n ON n.nspname = j.schema_name
JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = j.table_name
JOIN pg_catalog.pg_trigger t ON t.tgrelid = c.oid
WHERE NOT t.tgisinternal AND t.tgenabled <> 'D' AND NOT pg_has_role(c.relowner, 'USAGE')
ORDER BY 1`, tablesList)
	if err != nil {
		return nil, fmt.Errorf("unable to read the triggers of destination %q: %w", name, err)
	}
	defer rows.Close()
	var found []triggerNotOwned
	for rows.Next() {
		var trigger triggerNotOwned
		if err := rows.Scan(&trigger.table, &trigger.owner); err != nil {
			return nil, fmt.Errorf("unable to read the triggers of destination %q: %w", name, err)
		}
		found = append(found, trigger)
	}
	return found, rows.Err()
}

// describeMissing makes a finding of the privileges each table lacks.
func describeMissing(
	check Check,
	role, name, verb string,
	missing map[postgresTable][]string,
	account string,
) []*Finding {
	findings := make([]*Finding, 0, len(missing))
	for table, privileges := range missing {
		if slices.Contains(privileges, absentTable) {
			findings = append(findings, blocking(CheckTableExists, table.String(), nil, "",
				fmt.Sprintf("%s %q has no table %s", role, name, table)))
			continue
		}
		findings = append(findings, blocking(check, table.String(), privileges,
			postgresGrantOnTable(privileges, table, account),
			fmt.Sprintf("%s %q cannot %s %s (missing %s)", role, name, verb, table, strings.Join(privileges, ", "))))
	}
	sortFindings(findings)
	return findings
}

// postgresGrantOnTable is the statement granting privileges on a table, USAGE on its schema
// included.
func postgresGrantOnTable(privileges []string, table postgresTable, account string) string {
	var statements []string
	onTable := slices.DeleteFunc(slices.Clone(privileges), func(p string) bool { return p == usageOnSchema })
	if len(onTable) < len(privileges) {
		statements = append(statements, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s;",
			sqlmanager_postgres.EscapePgColumn(table.schema), account))
	}
	if len(onTable) > 0 {
		statements = append(statements, fmt.Sprintf("GRANT %s ON TABLE %s TO %s;",
			strings.Join(onTable, ", "), table.quoted(), account))
	}
	return strings.Join(statements, " ")
}

// postgresVersion is server_version_num, or 0 when it cannot be read.
func postgresVersion(ctx context.Context, db Db) int {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return 0
	}
	return version
}

// postgresAccount is the account the session runs as, quoted for a statement to name it; a
// placeholder when it cannot be read, which a remedy still reads right with.
func postgresAccount(ctx context.Context, db Db) string {
	var account string
	if err := db.QueryRowContext(ctx, "SELECT current_user").Scan(&account); err != nil {
		return "<account>"
	}
	return sqlmanager_postgres.EscapePgColumn(account)
}

// probeForeignKeySuspension tries, in a transaction rolled back at once, the statement
// Athanor writes each page with, and returns why it was refused, or nil. Whether an account
// may set session_replication_role depends on SUPERUSER, or from PostgreSQL 15 on on GRANT
// SET ON PARAMETER; a managed service has no real superuser, and reading rolsuper would say
// no where the grant says yes. Trying it is the one answer that holds everywhere.
func probeForeignKeySuspension(ctx context.Context, db Db, name string) (refused, err error) {
	disable, _, _ := sqlio.PostgresDialect{}.ForeignKeyChecksStatements()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to open a transaction on destination %q: %w", name, err)
	}
	_, probeErr := tx.ExecContext(ctx, disable)
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return nil, fmt.Errorf("unable to roll back the probe on destination %q: %w", name, err)
	}
	return probeErr, nil
}
