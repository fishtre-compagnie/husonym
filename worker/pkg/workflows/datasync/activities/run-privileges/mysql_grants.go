package runprivileges_activity

import (
	"slices"
	"strings"
)

// MySQL has no has_table_privilege. What a run does with rows is asked of the server itself
// (see mysql.go); what it does to a table — empty it, take its triggers out of the way —
// and to a trigger — give it back its definer — has no statement to ask it with, and is
// read from SHOW GRANTS. Without FOR, SHOW GRANTS describes the account the session runs as,
// with the privileges of its active roles merged in: what information_schema shows neither
// for a role nor for an account declared on a specific host.

// mysqlGrant is one line of SHOW GRANTS about privileges on a database or a table.
type mysqlGrant struct {
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

// mysqlGrants is what SHOW GRANTS said about the account.
type mysqlGrants []mysqlGrant

// parseMysqlGrants reads the lines of SHOW GRANTS. Lines granting roles or proxies, or
// privileges on routines, say nothing about tables and are left out.
func parseMysqlGrants(lines []string) mysqlGrants {
	var grants mysqlGrants
	for _, line := range lines {
		grant, ok := parseMysqlGrant(line)
		if ok {
			grants = append(grants, grant)
		}
	}
	return grants
}

func parseMysqlGrant(line string) (mysqlGrant, bool) {
	var grant mysqlGrant
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
	database, table, ok := splitMysqlTarget(target)
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

// splitMysqlTarget reads *.*, `db`.* or `db`.`table`.
func splitMysqlTarget(target string) (database, table string, ok bool) {
	database, rest, ok := readMysqlName(target)
	if !ok || !strings.HasPrefix(rest, ".") {
		return "", "", false
	}
	table, rest, ok = readMysqlName(rest[1:])
	if !ok || rest != "" {
		return "", "", false
	}
	return database, table, true
}

// readMysqlName reads * or a name between backticks at the start of s.
func readMysqlName(s string) (name, rest string, ok bool) {
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

// hasTablePrivilege tells whether the account holds a privilege on a table: on every
// database and not taken back on this one, on the database, or on the table.
func (g mysqlGrants) hasTablePrivilege(privilege, database, table string) bool {
	for _, grant := range g {
		if grant.revoke || !grant.grants(privilege) {
			continue
		}
		switch {
		case grant.database == "*":
			if !g.revoked(privilege, database) {
				return true
			}
		case grant.table == "*":
			if matchMysqlPattern(grant.database, database) {
				return true
			}
		case grant.database == database && grant.table == table:
			return true
		}
	}
	return false
}

// hasGlobalPrivilege tells whether the account holds one of the privileges on every
// database — where the dynamic privileges are granted.
func (g mysqlGrants) hasGlobalPrivilege(privileges ...string) bool {
	for _, grant := range g {
		if grant.revoke || grant.database != "*" {
			continue
		}
		for _, privilege := range privileges {
			if slices.Contains(grant.privileges, privilege) {
				return true
			}
		}
	}
	return false
}

func (g mysqlGrants) revoked(privilege, database string) bool {
	for _, grant := range g {
		if grant.revoke && grant.database == database && grant.grants(privilege) {
			return true
		}
	}
	return false
}

func (g mysqlGrant) grants(privilege string) bool {
	return slices.Contains(g.privileges, privilege) || slices.Contains(g.privileges, "ALL")
}

// matchMysqlPattern matches a database name against the pattern of a database-level grant:
// _ is any character, % any run of them, and a backslash makes the next one literal.
func matchMysqlPattern(pattern, name string) bool {
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
