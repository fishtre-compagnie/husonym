package mysqlgrants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lines MySQL 8.0 printed for an account holding privileges of its own, through a role
// and on a database pattern (the role's are merged into the account's).
var showGrants = []string{
	"GRANT RELOAD, PROCESS ON *.* TO `pg1`@`%`",
	"GRANT SET_USER_ID ON *.* TO `pg1`@`%`",
	"GRANT DROP, TRIGGER ON `probe\\_cl%`.* TO `pg1`@`%`",
	"GRANT SELECT, INSERT ON `probe_claude`.* TO `pg1`@`%`",
	"GRANT UPDATE (`n`, `m`), DELETE ON `probe_claude`.`t` TO `pg1`@`%`",
	"GRANT `rg1`@`%` TO `pg1`@`%`",
	"GRANT PROXY ON ``@`` TO `pg1`@`%` WITH GRANT OPTION",
}

func TestParseMysqlGrants(t *testing.T) {
	grants := Parse(showGrants, false)
	require.Equal(t, []entry{
		{privileges: []string{"RELOAD", "PROCESS"}, database: "*", table: "*"},
		{privileges: []string{"SET_USER_ID"}, database: "*", table: "*"},
		{privileges: []string{"DROP", "TRIGGER"}, database: `probe\_cl%`, table: "*"},
		{privileges: []string{"SELECT", "INSERT"}, database: "probe_claude", table: "*"},
		{privileges: []string{"DELETE"}, database: "probe_claude", table: "t"},
	}, grants.grants)
}

func TestMysqlGrants_hasTablePrivilege(t *testing.T) {
	grants := Parse(showGrants, false)
	require.True(t, grants.HasTablePrivilege("TRIGGER", "probe_claude", "t"), "granted on a matching pattern")
	require.False(t, grants.HasTablePrivilege("TRIGGER", "probeXclaude", "t"), `\_ is a literal underscore`)
	require.True(t, grants.HasTablePrivilege("INSERT", "probe_claude", "autre"), "granted on the database")
	require.True(t, grants.HasTablePrivilege("DELETE", "probe_claude", "t"), "granted on the table")
	require.False(t, grants.HasTablePrivilege("DELETE", "probe_claude", "autre"))
	require.False(t, grants.HasTablePrivilege("UPDATE", "probe_claude", "t"), "granted on some columns only")
	require.True(t, grants.HasGlobalPrivilege("SUPER", "SET_USER_ID"))
	require.False(t, grants.HasGlobalPrivilege("SUPER"))
}

// ALL PRIVILEGES on every database, taken back on one of them (partial_revokes).
func TestMysqlGrants_partialRevoke(t *testing.T) {
	grants := Parse([]string{
		"GRANT ALL PRIVILEGES ON *.* TO `app`@`10.0.0.%` WITH GRANT OPTION",
		"REVOKE DROP ON `prod`.* FROM `app`@`10.0.0.%`",
	}, false)
	require.True(t, grants.HasTablePrivilege("DROP", "staging", "t"))
	require.False(t, grants.HasTablePrivilege("DROP", "prod", "t"))
	require.True(t, grants.HasTablePrivilege("TRIGGER", "prod", "t"))
}

func TestMatchMysqlPattern(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		match         bool
	}{
		{"shop", "shop", true},
		{"shop", "shops", false},
		{"sh_p", "shop", true},
		{`sh\_p`, "shop", false},
		{`sh\_p`, "sh_p", true},
		{"s%", "shop", true},
		{"%p", "shop", true},
		{"%", "", true},
		{"é_", "éa", true},
	} {
		require.Equal(t, c.match, matchPattern(c.pattern, c.name), "%s ~ %s", c.pattern, c.name)
	}
}

// A server folding table names grants on the lower case names; the job spells them as the
// source does.
func TestMysqlGrants_foldCase(t *testing.T) {
	lines := []string{
		"GRANT SELECT, TRIGGER ON `bench_x`.* TO `u`@`%`",
		"GRANT DELETE ON `bench_x`.`article` TO `u`@`%`",
	}
	folding := Parse(lines, true)
	require.True(t, folding.HasTablePrivilege("TRIGGER", "Bench_X", "ARTICLE"))
	require.True(t, folding.HasTablePrivilege("DELETE", "Bench_X", "ARTICLE"))
	exact := Parse(lines, false)
	require.False(t, exact.HasTablePrivilege("DELETE", "bench_x", "ARTICLE"), "another table on a case-sensitive server")
}

// GRANT ALL PRIVILEGES ON *.* holds the dynamic privileges too. Reading it as the single
// word it is parsed into reported the account as lacking SUPER and SET_USER_ID, and the
// run was refused before it started.
func TestMysqlGrants_hasGlobalPrivilegeThroughAll(t *testing.T) {
	grants := Parse([]string{
		"GRANT ALL PRIVILEGES ON *.* TO `app`@`%` WITH GRANT OPTION",
	}, false)
	require.True(t, grants.HasGlobalPrivilege("SUPER", "SET_USER_ID"))
	require.True(t, grants.HasGlobalPrivilege("SET_USER_ID"))

	onOneDatabase := Parse([]string{
		"GRANT ALL PRIVILEGES ON `prod`.* TO `app`@`%`",
	}, false)
	require.False(t, onOneDatabase.HasGlobalPrivilege("SUPER", "SET_USER_ID"),
		"ALL on one database is not ALL on every database")
}

// MariaDB grants the definer privilege as SET USER, two words.
func TestGrants_setUser(t *testing.T) {
	grants := Parse([]string{"GRANT SET USER ON *.* TO `app`@`%`"}, false)
	require.True(t, grants.HasGlobalPrivilege("SET USER"))
	require.False(t, grants.HasGlobalPrivilege("SUPER"))
}
