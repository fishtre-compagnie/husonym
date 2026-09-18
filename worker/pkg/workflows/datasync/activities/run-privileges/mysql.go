package runprivileges_activity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	sqlmanager_mysql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mysql"
	"github.com/go-sql-driver/mysql"
)

// MySQL is asked about the account itself, as PostgreSQL is. What a run does with rows —
// read them, insert, update, delete them — is asked with EXPLAIN of the statement, which
// the server checks against every privilege the session holds, from roles or from an
// account declared on a specific host, without running it. Reading the privilege tables
// instead missed both: a run it would refuse went on and failed half way.

// MySQL error numbers of a refused or impossible statement.
const (
	mysqlDatabaseAccessDenied = 1044
	mysqlTableAccessDenied    = 1142
	mysqlColumnAccessDenied   = 1143
	mysqlNoSuchTable          = 1146
)

// errMysqlNoSuchTable says that a probed table does not exist.
var errMysqlNoSuchTable = errors.New("no such table")

// checkMysqlSource: a source only needs to be read. A read-only server is fine.
func checkMysqlSource(ctx context.Context, db sqlDb, name string, tables []*jobTable) ([]string, error) {
	var findings []string
	for _, t := range tables {
		allowed, err := mysqlProbe(ctx, db, t.mysqlSelect())
		switch {
		case errors.Is(err, errMysqlNoSuchTable):
			findings = append(findings, fmt.Sprintf("source %q has no table %s", name, t))
		case err != nil:
			return nil, fmt.Errorf("unable to read the privileges of source %q on %s: %w", name, t, err)
		case !allowed:
			findings = append(findings, fmt.Sprintf("source %q cannot read %s (missing SELECT)", name, t))
		}
	}
	return findings, nil
}

// checkMysqlDestination: a destination must accept writes, on the server and on every
// table; emptying a table before writing it takes DROP; and the run takes the triggers of
// the tables out of its way, which takes TRIGGER, and puts them back as their definer.
func checkMysqlDestination(
	ctx context.Context,
	db sqlDb,
	name string,
	tables []*jobTable,
	createsTables, truncates bool,
) ([]string, error) {
	// read_only refuses the writes of regular accounts, super_read_only everyone's.
	var readOnly bool
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.read_only OR @@GLOBAL.super_read_only").Scan(&readOnly); err != nil {
		return nil, fmt.Errorf("unable to tell whether destination %q accepts writes: %w", name, err)
	}
	if readOnly {
		return []string{fmt.Sprintf("destination %q is a read-only server (read_only or super_read_only is ON): "+
			"a destination must be a standalone or primary server", name)}, nil
	}

	var findings []string
	var existing []*jobTable
	for _, t := range tables {
		missing, err := mysqlMissingRowPrivileges(ctx, db, t)
		switch {
		case errors.Is(err, errMysqlNoSuchTable):
			if !createsTables {
				findings = append(findings, fmt.Sprintf("destination %q has no table %s", name, t))
			}
			continue // a table the run creates is the account's own
		case err != nil:
			return nil, fmt.Errorf("unable to read the privileges of destination %q on %s: %w", name, t, err)
		}
		if len(missing) > 0 {
			findings = append(findings, fmt.Sprintf("destination %q cannot write %s (missing %s)",
				name, t, strings.Join(missing, ", ")))
		}
		existing = append(existing, t)
	}

	grants, err := readMysqlGrants(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
	}
	var seeTriggers []*jobTable
	for _, t := range existing {
		if truncates && !grants.hasTablePrivilege("DROP", t.Schema, t.Table) {
			findings = append(findings, fmt.Sprintf("destination %q cannot empty %s before writing it "+
				"(missing DROP, which TRUNCATE takes)", name, t))
		}
		if !grants.hasTablePrivilege("TRIGGER", t.Schema, t.Table) {
			findings = append(findings, fmt.Sprintf("destination %q cannot see the triggers of %s nor take them "+
				"out of the way of the run (missing TRIGGER): MySQL hides the triggers of a table from an account "+
				"without it, and they would fire on what the run writes", name, t))
			continue
		}
		seeTriggers = append(seeTriggers, t)
	}

	definerFindings, err := checkMysqlTriggerDefiners(ctx, db, name, seeTriggers, grants)
	if err != nil {
		return nil, err
	}
	return append(findings, definerFindings...), nil
}

// checkMysqlTriggerDefiners: a trigger taken out of the way is created again as its
// definer. Naming an account other than one's own as definer takes SET_USER_ID (SUPER, or
// SET_ANY_DEFINER from 8.2 on): without it the triggers would be dropped and could not
// come back.
func checkMysqlTriggerDefiners(
	ctx context.Context,
	db sqlDb,
	name string,
	tables []*jobTable,
	grants mysqlGrants,
) ([]string, error) {
	if len(tables) == 0 || grants.hasGlobalPrivilege("SUPER", "SET_USER_ID", "SET_ANY_DEFINER") {
		return nil, nil
	}
	var account string
	if err := db.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&account); err != nil {
		return nil, fmt.Errorf("unable to read the account of destination %q: %w", name, err)
	}
	var findings []string
	for _, t := range tables {
		rows, err := db.QueryContext(ctx,
			"SELECT TRIGGER_NAME, DEFINER FROM information_schema.TRIGGERS "+
				"WHERE EVENT_OBJECT_SCHEMA = ? AND EVENT_OBJECT_TABLE = ? ORDER BY TRIGGER_NAME", t.Schema, t.Table)
		if err != nil {
			return nil, fmt.Errorf("unable to read the triggers of destination %q: %w", name, err)
		}
		for rows.Next() {
			var trigger, definer string
			if err := rows.Scan(&trigger, &definer); err != nil {
				rows.Close()
				return nil, fmt.Errorf("unable to read the triggers of destination %q: %w", name, err)
			}
			if definer != account {
				findings = append(findings, fmt.Sprintf("destination %q cannot put back the trigger %s of %s, "+
					"whose definer is %s (missing SET_USER_ID or SUPER): a trigger runs as its definer, and only "+
					"such an account can name another one", name, trigger, t, definer))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("unable to read the triggers of destination %q: %w", name, err)
		}
	}
	return findings, nil
}

// mysqlMissingRowPrivileges returns the privileges on rows a destination table refuses.
func mysqlMissingRowPrivileges(ctx context.Context, db sqlDb, t *jobTable) ([]string, error) {
	var missing []string
	for _, probe := range []struct{ privilege, statement string }{
		{"SELECT", t.mysqlSelect()},
		{"INSERT", t.mysqlInsert()},
		{"UPDATE", t.mysqlUpdate()},
		{"DELETE", t.mysqlDelete()},
	} {
		allowed, err := mysqlProbe(ctx, db, probe.statement)
		if err != nil {
			return nil, err
		}
		if !allowed {
			missing = append(missing, probe.privilege)
		}
	}
	return missing, nil
}

// mysqlProbe explains a statement: the server checks its privileges without running it.
func mysqlProbe(ctx context.Context, db sqlDb, statement string) (bool, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN "+statement)
	if err == nil {
		return true, rows.Close()
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case mysqlDatabaseAccessDenied, mysqlTableAccessDenied, mysqlColumnAccessDenied:
			return false, nil
		case mysqlNoSuchTable:
			return false, errMysqlNoSuchTable
		}
	}
	return false, err
}

// readMysqlGrants reads SHOW GRANTS for the session's own account, and whether the server
// folds table names to lower case, which its grants are then written in.
func readMysqlGrants(ctx context.Context, db sqlDb) (mysqlGrants, error) {
	var lowerCaseTableNames int
	if err := db.QueryRowContext(ctx, "SELECT @@lower_case_table_names").Scan(&lowerCaseTableNames); err != nil {
		return mysqlGrants{}, err
	}
	rows, err := db.QueryContext(ctx, "SHOW GRANTS")
	if err != nil {
		return mysqlGrants{}, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return mysqlGrants{}, err
		}
		lines = append(lines, line)
	}
	return parseMysqlGrants(lines, lowerCaseTableNames != 0), rows.Err()
}

// The probes name the columns the run writes, so that a privilege held on some columns only
// answers for those. The statements are never run, but MySQL still evaluates the values of
// an INSERT into a partitioned table to prune its partitions: NULL goes through there for
// any column, where DEFAULT is refused on a column without a default. An UPDATE takes
// DEFAULT, which no partitioning refuses.

func (t *jobTable) mysqlName() string {
	return quoteMysql(t.Schema) + "." + quoteMysql(t.Table)
}

func (t *jobTable) mysqlColumns() string {
	quoted := make([]string, len(t.Columns))
	for i, column := range t.Columns {
		quoted[i] = quoteMysql(column)
	}
	return strings.Join(quoted, ", ")
}

func (t *jobTable) mysqlSelect() string {
	return "SELECT " + t.mysqlColumns() + " FROM " + t.mysqlName() + " WHERE FALSE"
}

func (t *jobTable) mysqlInsert() string {
	nulls := slices.Repeat([]string{"NULL"}, len(t.Columns))
	return "INSERT INTO " + t.mysqlName() + " (" + t.mysqlColumns() + ") VALUES (" + strings.Join(nulls, ", ") + ")"
}

func (t *jobTable) mysqlUpdate() string {
	sets := make([]string, len(t.Columns))
	for i, column := range t.Columns {
		sets[i] = quoteMysql(column) + " = DEFAULT"
	}
	return "UPDATE " + t.mysqlName() + " SET " + strings.Join(sets, ", ") + " WHERE FALSE"
}

func (t *jobTable) mysqlDelete() string {
	return "DELETE FROM " + t.mysqlName() + " WHERE FALSE"
}

var quoteMysql = sqlmanager_mysql.EscapeMysqlColumn
