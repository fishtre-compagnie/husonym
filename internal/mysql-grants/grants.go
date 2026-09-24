// Package mysqlgrants reads what a MySQL account may do from SHOW GRANTS.
//
// MySQL has no has_table_privilege. What a run does with rows is asked of the server itself
// (see internal/connection-checks); what it does to a table — empty it, take its triggers out of the way —
// and to a trigger — give it back its definer — has no statement to ask it with, and is
// read from SHOW GRANTS. Without FOR, SHOW GRANTS describes the account the session runs as,
// with the privileges of its active roles merged in: what information_schema shows neither
// for a role nor for an account declared on a specific host.
package mysqlgrants

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

// Querier is what reading the grants needs from a connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// entry is one line of SHOW GRANTS about privileges on a database or a table.
type entry struct {
	// revoke marks a partial revoke: the privileges are taken back on one database from a
	// global grant (partial_revokes).
	revoke bool
	// privileges are the table-level privileges, upper case; column privileges are left
	// out, no table-level question being answered by them.
	privileges []string
	// database is a pattern where _ and % are wildcards and a backslash escapes them, or *.
	database string
	// table is a table name, or *.
	table string
}

// Grants is what SHOW GRANTS said about the account.
type Grants struct {
	grants []entry
	// foldCase compares database and table names without regard to case, as a server with
	// lower_case_table_names 1 or 2 does: it grants on the names folded to lower case, and a
	// job names its tables the way the source spells them.
	foldCase bool
}

// Parse reads the lines of SHOW GRANTS. Lines granting roles or proxies, or
// privileges on routines, say nothing about tables and are left out.
func Parse(lines []string, foldCase bool) Grants {
	grants := Grants{foldCase: foldCase}
	for _, line := range lines {
		grant, ok := parseEntry(line)
		if ok {
			grants.grants = append(grants.grants, grant)
		}
	}
	return grants
}

func parseEntry(line string) (entry, bool) {
	var grant entry
	var rest, to string
	switch {
	case strings.HasPrefix(line, "GRANT "):
		rest, to = line[len("GRANT "):], " TO "
	case strings.HasPrefix(line, "REVOKE "):
		rest, to, grant.revoke = line[len("REVOKE "):], " FROM ", true
	default:
		return grant, false
	}
	on := strings.Index(rest, " ON ")
	account := strings.LastIndex(rest, to)
	if on < 0 || account < on {
		return grant, false // a role granted to the account
	}
	target := rest[on+len(" ON ") : account]
	for _, privilege := range splitTopLevel(rest[:on]) {
		privilege = strings.ToUpper(strings.TrimSpace(privilege))
		switch {
		case privilege == "PROXY":
			return grant, false
		case strings.Contains(privilege, "("):
			continue // a column privilege
		case privilege == "ALL PRIVILEGES":
			privilege = "ALL"
		}
		grant.privileges = append(grant.privileges, privilege)
	}
	database, table, ok := splitTarget(target)
	if !ok {
		return grant, false // a procedure or a function
	}
	grant.database, grant.table = database, table
	return grant, true
}

// splitTopLevel splits a privilege list on the commas outside parentheses: a column
// privilege lists its columns between them.
func splitTopLevel(list string) []string {
	var parts []string
	depth, start := 0, 0
	inQuote := false
	for i, r := range list {
		switch {
		case r == '`':
			inQuote = !inQuote
		case inQuote:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, list[start:i])
			start = i + 1
		}
	}
	return append(parts, list[start:])
}

// splitTarget reads *.*, `db`.* or `db`.`table`.
func splitTarget(target string) (database, table string, ok bool) {
	database, rest, ok := readName(target)
	if !ok || !strings.HasPrefix(rest, ".") {
		return "", "", false
	}
	table, rest, ok = readName(rest[1:])
	if !ok || rest != "" {
		return "", "", false
	}
	return database, table, true
}

// readName reads * or a name between backticks at the start of s.
func readName(s string) (name, rest string, ok bool) {
	if strings.HasPrefix(s, "*") {
		return "*", s[1:], true
	}
	if !strings.HasPrefix(s, "`") {
		return "", "", false
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		if s[i] != '`' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '`' {
			b.WriteByte('`')
			i++
			continue
		}
		return b.String(), s[i+1:], true
	}
	return "", "", false
}

// HasTablePrivilege tells whether the account holds a privilege on a table: on every
// database and not taken back on this one, on the database, or on the table.
func (g Grants) HasTablePrivilege(privilege, database, table string) bool {
	database, table = g.fold(database), g.fold(table)
	for _, grant := range g.grants {
		if grant.revoke || !grant.grants(privilege) {
			continue
		}
		switch {
		case grant.database == "*":
			if !g.revoked(privilege, database) {
				return true
			}
		case grant.table == "*":
			if matchPattern(g.fold(grant.database), database) {
				return true
			}
		case g.fold(grant.database) == database && g.fold(grant.table) == table:
			return true
		}
	}
	return false
}

func (g Grants) fold(name string) string {
	if g.foldCase {
		return strings.ToLower(name)
	}
	return name
}

// HasGlobalPrivilege tells whether the account holds one of the privileges on every
// database — where the dynamic privileges are granted.
func (g Grants) HasGlobalPrivilege(privileges ...string) bool {
	for _, grant := range g.grants {
		if grant.revoke || grant.database != "*" {
			continue
		}
		for _, privilege := range privileges {
			// grants, and not a plain lookup: GRANT ALL PRIVILEGES ON *.* holds every
			// dynamic privilege too, and parseEntry records it as the single "ALL".
			if grant.grants(privilege) {
				return true
			}
		}
	}
	return false
}

func (g Grants) revoked(privilege, database string) bool {
	for _, grant := range g.grants {
		if grant.revoke && g.fold(grant.database) == database && grant.grants(privilege) {
			return true
		}
	}
	return false
}

func (g entry) grants(privilege string) bool {
	return slices.Contains(g.privileges, privilege) || slices.Contains(g.privileges, "ALL")
}

// matchPattern matches a database name against the pattern of a database-level grant:
// _ is any character, % any run of them, and a backslash makes the next one literal.
func matchPattern(pattern, name string) bool {
	return matchRunes([]rune(pattern), []rune(name))
}

func matchRunes(pattern, name []rune) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	switch pattern[0] {
	case '%':
		for i := 0; i <= len(name); i++ {
			if matchRunes(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	case '_':
		return len(name) > 0 && matchRunes(pattern[1:], name[1:])
	case '\\':
		if len(pattern) > 1 {
			return len(name) > 0 && name[0] == pattern[1] && matchRunes(pattern[2:], name[1:])
		}
	}
	return len(name) > 0 && name[0] == pattern[0] && matchRunes(pattern[1:], name[1:])
}

// Read reads SHOW GRANTS for the session's own account, and whether the server
// folds table names to lower case, which its grants are then written in.
func Read(ctx context.Context, db Querier) (Grants, error) {
	var lowerCaseTableNames int
	if err := db.QueryRowContext(ctx, "SELECT @@lower_case_table_names").Scan(&lowerCaseTableNames); err != nil {
		return Grants{}, err
	}
	rows, err := db.QueryContext(ctx, "SHOW GRANTS")
	if err != nil {
		return Grants{}, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return Grants{}, err
		}
		lines = append(lines, line)
	}
	return Parse(lines, lowerCaseTableNames != 0), rows.Err()
}
