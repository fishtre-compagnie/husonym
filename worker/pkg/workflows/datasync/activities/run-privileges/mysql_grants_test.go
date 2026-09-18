package runprivileges_activity

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
	grants := parseMysqlGrants(showGrants)
	require.Equal(t, mysqlGrants{
		{privileges: []string{"RELOAD", "PROCESS"}, database: "*", table: "*"},
		{privileges: []string{"SET_USER_ID"}, database: "*", table: "*"},
		{privileges: []string{"DROP", "TRIGGER"}, database: `probe\_cl%`, table: "*"},
		{privileges: []string{"SELECT", "INSERT"}, database: "probe_claude", table: "*"},
		{privileges: []string{"DELETE"}, database: "probe_claude", table: "t"},
	}, grants)
}

func TestMysqlGrants_hasTablePrivilege(t *testing.T) {
	grants := parseMysqlGrants(showGrants)
	require.True(t, grants.hasTablePrivilege("TRIGGER", "probe_claude", "t"), "granted on a matching pattern")
	require.False(t, grants.hasTablePrivilege("TRIGGER", "probeXclaude", "t"), `\_ is a literal underscore`)
	require.True(t, grants.hasTablePrivilege("INSERT", "probe_claude", "autre"), "granted on the database")
	require.True(t, grants.hasTablePrivilege("DELETE", "probe_claude", "t"), "granted on the table")
	require.False(t, grants.hasTablePrivilege("DELETE", "probe_claude", "autre"))
	require.False(t, grants.hasTablePrivilege("UPDATE", "probe_claude", "t"), "granted on some columns only")
	require.True(t, grants.hasGlobalPrivilege("SUPER", "SET_USER_ID"))
	require.False(t, grants.hasGlobalPrivilege("SUPER"))
}

// ALL PRIVILEGES on every database, taken back on one of them (partial_revokes).
func TestMysqlGrants_partialRevoke(t *testing.T) {
	grants := parseMysqlGrants([]string{
		"GRANT ALL PRIVILEGES ON *.* TO `app`@`10.0.0.%` WITH GRANT OPTION",
		"REVOKE DROP ON `prod`.* FROM `app`@`10.0.0.%`",
	})
	require.True(t, grants.hasTablePrivilege("DROP", "staging", "t"))
	require.False(t, grants.hasTablePrivilege("DROP", "prod", "t"))
	require.True(t, grants.hasTablePrivilege("TRIGGER", "prod", "t"))
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
		require.Equal(t, c.match, matchMysqlPattern(c.pattern, c.name), "%s ~ %s", c.pattern, c.name)
	}
}
