package connectionchecks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	sqlmanager_mysql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mysql"
	mysqlgrants "github.com/fishtre-compagnie/husonym/internal/mysql-grants"
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
	mysqlNoSuchDatabase       = 1049
	mysqlNoSuchColumn         = 1054
	mysqlTableAccessDenied    = 1142
	mysqlColumnAccessDenied   = 1143
	mysqlNoSuchTable          = 1146
)

var (
	// errMysqlNoSuchTable says that a probed table does not exist, nor maybe its database,
	// which a run creating its tables creates too.
	errMysqlNoSuchTable = errors.New("no such table")
	// errMysqlNoSuchColumn says that a probed table lacks a column the run writes, which
	// a run reconciling the schema adds.
	errMysqlNoSuchColumn = errors.New("no such column")
)

// checkMysqlSource: a source only needs to be read. A read-only server is fine.
func checkMysqlSource(ctx context.Context, db Db, name string, tables []*Table) ([]*Finding, error) {
	account := &mysqlAccount{db: db}
	var findings []*Finding
	for _, t := range tables {
		allowed, err := mysqlProbe(ctx, db, t.mysqlSelect())
		switch {
		case errors.Is(err, errMysqlNoSuchTable):
			findings = append(findings, blocking(CheckTableExists, t.String(), nil, "",
				fmt.Sprintf("source %q has no table %s", name, t)))
		case err != nil:
			return nil, fmt.Errorf("unable to read the privileges of source %q on %s: %w", name, t, err)
		case !allowed:
			findings = append(findings, blocking(CheckReadable, t.String(), []string{"SELECT"},
				mysqlGrantOnTable([]string{"SELECT"}, t, account.quoted(ctx)),
				fmt.Sprintf("source %q cannot read %s (missing SELECT)", name, t)))
		}
	}
	return findings, nil
}

// checkMysqlDestination: a destination must accept writes, on the server and on every
// table; emptying a table before writing it takes DROP; and the run takes the triggers of
// the tables out of its way, which takes TRIGGER, and puts them back as their definer.
func checkMysqlDestination(
	ctx context.Context,
	db Db,
	name string,
	tables []*Table,
	createsTables, truncates bool,
) ([]*Finding, error) {
	readOnly, err := mysqlReadOnly(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("unable to tell whether destination %q accepts writes: %w", name, err)
	}
	if readOnly {
		return []*Finding{blocking(CheckServerWritable, "", nil, "", fmt.Sprintf(
			"destination %q is a read-only server (read_only or super_read_only is ON): "+
				"a destination must be a standalone or primary server", name))}, nil
	}

	account := &mysqlAccount{db: db}
	var findings []*Finding
	var existing []*Table
	for _, t := range tables {
		missing, err := mysqlMissingRowPrivileges(ctx, db, t)
		if errors.Is(err, errMysqlNoSuchColumn) {
			// The probes ask about the columns the table has; the others are added by the
			// run when it reconciles the schema, and are missing otherwise.
			// present, and not t: on an error mysqlPresentColumns returns no table, and
			// the error below reports the table it was asked about.
			present, absent, perr := mysqlPresentColumns(ctx, db, t)
			err = perr
			if perr == nil {
				if !createsTables {
					findings = append(findings, blocking(CheckTableExists, present.String(), absent, "",
						fmt.Sprintf("destination %q has no column %s in %s that the account can see",
							name, strings.Join(absent, ", "), present)))
				}
				if len(present.Columns) == 0 {
					// Not one column of the mapping is there: there is nothing to probe,
					// and the probes would be built on an empty column list.
					continue
				}
				t = present
				missing, err = mysqlMissingRowPrivileges(ctx, db, t)
			}
		}
		switch {
		case errors.Is(err, errMysqlNoSuchTable):
			if !createsTables {
				findings = append(findings, blocking(CheckTableExists, t.String(), nil, "",
					fmt.Sprintf("destination %q has no table %s", name, t)))
			}
			continue // a table the run creates is the account's own
		case err != nil:
			return nil, fmt.Errorf("unable to read the privileges of destination %q on %s: %w", name, t, err)
		}
		if len(missing) > 0 {
			findings = append(findings, blocking(CheckWritable, t.String(), missing,
				mysqlGrantOnTable(missing, t, account.quoted(ctx)),
				fmt.Sprintf("destination %q cannot write %s (missing %s)", name, t, strings.Join(missing, ", "))))
		}
		existing = append(existing, t)
	}

	grants, err := mysqlgrants.Read(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
	}
	var seeTriggers []*Table
	for _, t := range existing {
		if truncates && !grants.HasTablePrivilege("DROP", t.Schema, t.Table) {
			findings = append(findings, blocking(CheckTruncate, t.String(), []string{"DROP"},
				mysqlGrantOnTable([]string{"DROP"}, t, account.quoted(ctx)),
				fmt.Sprintf("destination %q cannot empty %s before writing it "+
					"(missing DROP, which TRUNCATE takes)", name, t)))
		}
		if !grants.HasTablePrivilege("TRIGGER", t.Schema, t.Table) {
			findings = append(findings, blocking(CheckTriggers, t.String(), []string{"TRIGGER"},
				mysqlGrantOnTable([]string{"TRIGGER"}, t, account.quoted(ctx)),
				fmt.Sprintf("destination %q cannot see the triggers of %s nor take them "+
					"out of the way of the run (missing TRIGGER): MySQL hides the triggers of a table from an account "+
					"without it, and they would fire on what the run writes", name, t)))
			continue
		}
		seeTriggers = append(seeTriggers, t)
	}

	definerFindings, err := checkMysqlTriggerDefiners(ctx, db, name, seeTriggers, grants, account)
	if err != nil {
		return nil, err
	}
	return append(findings, definerFindings...), nil
}

// checkMysqlTriggerDefiners: a trigger taken out of the way is created again as its
// definer. Naming an account other than one's own as definer takes SET_USER_ID (SUPER, or
// SET_ANY_DEFINER from 8.2 on; SET USER on MariaDB): without it the triggers would be dropped
// and could not come back. No remedy is written: the privilege lets an account run code as
// any other, root included, which is not a statement to hand out for pasting.
func checkMysqlTriggerDefiners(
	ctx context.Context,
	db Db,
	name string,
	tables []*Table,
	grants mysqlgrants.Grants,
	account *mysqlAccount,
) ([]*Finding, error) {
	if len(tables) == 0 || grants.HasGlobalPrivilege("SUPER", "SET_USER_ID", "SET_ANY_DEFINER", "SET USER") {
		return nil, nil
	}
	current, err := account.current(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the account of destination %q: %w", name, err)
	}
	var findings []*Finding
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
			if definer != current {
				findings = append(findings, blocking(CheckTriggerDefiner, t.String(),
					[]string{"SET_USER_ID, SET_ANY_DEFINER or SUPER; SET USER on MariaDB"}, "",
					fmt.Sprintf("destination %q cannot put back the trigger %s of %s, "+
						"whose definer is %s (missing SET_USER_ID, SET_ANY_DEFINER or SUPER; SET USER on MariaDB): "+
						"a trigger runs as its definer, and only "+
						"such an account can name another one", name, trigger, t, definer)))
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
func mysqlMissingRowPrivileges(ctx context.Context, db Db, t *Table) ([]string, error) {
	if len(t.Columns) == 0 {
		// Asked without columns, the table is taken with those the account can see, and it
		// sees a column when it holds any privilege on it: none seen, it holds none of these.
		// Only a caller outside a run asks so; a run always names the columns it writes.
		allowed, err := mysqlProbe(ctx, db, t.mysqlSelect())
		if err != nil || allowed {
			return nil, err
		}
		return []string{"SELECT", "INSERT", "UPDATE", "DELETE"}, nil
	}
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
func mysqlProbe(ctx context.Context, db Db, statement string) (bool, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN "+statement)
	if err == nil {
		// MariaDB answers an EXPLAIN SELECT it refuses with the header of its result, and
		// the error after it: the result is read to its end before the answer is known.
		for rows.Next() {
		}
		err = errors.Join(rows.Err(), rows.Close())
	}
	if err == nil {
		return true, nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case mysqlDatabaseAccessDenied, mysqlTableAccessDenied, mysqlColumnAccessDenied:
			return false, nil
		case mysqlNoSuchTable, mysqlNoSuchDatabase:
			return false, errMysqlNoSuchTable
		case mysqlNoSuchColumn:
			return false, errMysqlNoSuchColumn
		}
	}
	return false, err
}

// mysqlPresentColumns splits the columns a table is written with between those the
// destination has, which the table returned keeps, and those it lacks. information_schema
// lists only the columns the account holds a privilege on.
func mysqlPresentColumns(ctx context.Context, db Db, t *Table) (*Table, []string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		t.Schema, t.Table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var has []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, nil, err
		}
		has = append(has, column)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	present := &Table{Schema: t.Schema, Table: t.Table}
	var absent []string
	for _, column := range t.Columns {
		// MySQL compares column names regardless of case
		if slices.ContainsFunc(has, func(c string) bool { return strings.EqualFold(c, column) }) {
			present.Columns = append(present.Columns, column)
		} else {
			absent = append(absent, column)
		}
	}
	return present, absent, nil
}

// mysqlReadOnly tells whether the server refuses writes: read_only refuses those of regular
// accounts, super_read_only everyone's. MariaDB has no super_read_only, which SHOW then
// leaves out where selecting it fails.
func mysqlReadOnly(ctx context.Context, db Db) (bool, error) {
	rows, err := db.QueryContext(ctx,
		"SHOW GLOBAL VARIABLES WHERE Variable_name IN ('read_only', 'super_read_only')")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	readOnly := false
	for rows.Next() {
		var variable, value string
		if err := rows.Scan(&variable, &value); err != nil {
			return false, err
		}
		readOnly = readOnly || !strings.EqualFold(value, "OFF")
	}
	return readOnly, rows.Err()
}

// The probes name the columns the run writes, so that a privilege held on some columns only
// answers for those. The statements are never run, but the values they set are still
// evaluated: MySQL prunes the partitions of an INSERT with them, and MariaDB refuses
// DEFAULT on a column without a default. NULL goes through both, on any column.

func (t *Table) mysqlName() string {
	return quoteMysql(t.Schema) + "." + quoteMysql(t.Table)
}

func (t *Table) mysqlColumns() string {
	quoted := make([]string, len(t.Columns))
	for i, column := range t.Columns {
		quoted[i] = quoteMysql(column)
	}
	return strings.Join(quoted, ", ")
}

func (t *Table) mysqlSelect() string {
	columns := t.mysqlColumns()
	if columns == "" {
		// A table without columns is asked about as a whole.
		columns = "1"
	}
	return "SELECT " + columns + " FROM " + t.mysqlName() + " WHERE FALSE"
}

func (t *Table) mysqlInsert() string {
	nulls := slices.Repeat([]string{"NULL"}, len(t.Columns))
	return "INSERT INTO " + t.mysqlName() + " (" + t.mysqlColumns() + ") VALUES (" + strings.Join(nulls, ", ") + ")"
}

func (t *Table) mysqlUpdate() string {
	sets := make([]string, len(t.Columns))
	for i, column := range t.Columns {
		sets[i] = quoteMysql(column) + " = NULL"
	}
	return "UPDATE " + t.mysqlName() + " SET " + strings.Join(sets, ", ") + " WHERE FALSE"
}

func (t *Table) mysqlDelete() string {
	return "DELETE FROM " + t.mysqlName() + " WHERE FALSE"
}

var quoteMysql = sqlmanager_mysql.EscapeMysqlColumn

// mysqlAccount is the account the session runs as, read once, and only when something asks.
type mysqlAccount struct {
	db   Db
	name string
	err  error
	read bool
}

// current is the account as CURRENT_USER() gives it: user@host.
func (a *mysqlAccount) current(ctx context.Context) (string, error) {
	if !a.read {
		a.err = a.db.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&a.name)
		a.read = true
	}
	return a.name, a.err
}

// quoted is the account as a statement names it, 'user'@'host'; a placeholder when it
// cannot be read, which a remedy still reads right with.
func (a *mysqlAccount) quoted(ctx context.Context) string {
	current, err := a.current(ctx)
	if err != nil {
		return "<account>"
	}
	// A user name may hold an @, a host may not: the account splits at the last one.
	at := strings.LastIndex(current, "@")
	if at < 0 {
		return quoteMysqlString(current)
	}
	return quoteMysqlString(current[:at]) + "@" + quoteMysqlString(current[at+1:])
}

// mysqlGrantOnTable is the statement granting privileges on a table.
func mysqlGrantOnTable(privileges []string, t *Table, account string) string {
	return fmt.Sprintf("GRANT %s ON %s TO %s;", strings.Join(privileges, ", "), t.mysqlName(), account)
}

// quoteMysqlString quotes a name the way an account is written in a statement. A quote is
// doubled, which holds whether the server takes backslashes as escapes or not.
func quoteMysqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
