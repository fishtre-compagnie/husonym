package sqlident

import (
	"testing"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	_ "github.com/doug-martin/goqu/v9/dialect/sqlserver"
	"github.com/stretchr/testify/require"
)

func Test_Quote(t *testing.T) {
	cases := []struct {
		d    Dialect
		in   string
		want string
	}{
		{Postgres, `plain`, `"plain"`},
		{Postgres, `we"ird`, `"we""ird"`},
		{Postgres, `back\slash`, `"back\slash"`},
		{Postgres, `o'clock`, `"o'clock"`},
		{Postgres, "new\nline", "\"new\nline\""},
		{Postgres, `two$$dollars`, `"two$$dollars"`},
		{Postgres, `semi;colon`, `"semi;colon"`},
		{MySQL, "plain", "`plain`"},
		{MySQL, "we`ird", "`we``ird`"},
		{MySQL, `we"ird`, "`we\"ird`"},
		{MySQL, `back\slash`, "`back\\slash`"},
		{SQLServer, `plain`, `[plain]`},
		{SQLServer, `we]ird`, `[we]]ird]`},
		{SQLServer, `we"ird`, `[we"ird]`},
		{SQLServer, "new\nline", "[new\nline]"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.d.Quote(c.in))
	}
}

func Test_Qualified(t *testing.T) {
	require.Equal(t, `"public"."users"`, Postgres.Qualified("public", "users"))
	require.Equal(t, `"a.b"."c"`, Postgres.Qualified("a.b", "c"))
	require.Equal(t, `"users"`, Postgres.Qualified("", "users"))
	require.Equal(t, "`app`.`we``ird`", MySQL.Qualified("app", "we`ird"))
	require.Equal(t, "`users`", MySQL.Qualified("", "users"))
	require.Equal(t, `[dbo].[we]]ird]`, SQLServer.Qualified("dbo", "we]ird"))
	require.Equal(t, `[users]`, SQLServer.Qualified("", "users"))
}

func Test_Literal(t *testing.T) {
	cases := []struct {
		d    Dialect
		in   string
		want string
	}{
		{Postgres, `plain`, `'plain'`},
		{Postgres, `o'clock`, `'o''clock'`},
		{Postgres, `back\slash`, `'back\slash'`},
		{Postgres, ``, `''`},
		{MySQL, `plain`, `'plain'`},
		{MySQL, `o'clock`, `'o''clock'`},
		{MySQL, ``, `''`},
		{MySQL, "new\nline", "'new\nline'"},
		// A backslash switches to the hex form, which reads the same under every sql_mode.
		{MySQL, `back\slash`, `_utf8mb4 0x6261636B5C736C617368`},
		{MySQL, `\`, `_utf8mb4 0x5C`},
		{MySQL, `o'\é`, `_utf8mb4 0x6F275CC3A9`},
		{SQLServer, `plain`, `N'plain'`},
		{SQLServer, `o'clock`, `N'o''clock'`},
		{SQLServer, ``, `N''`},
		{SQLServer, "lone\nnewline", "N'lone\nnewline'"},
		{SQLServer, `back\slash`, `N'back\slash'`},
		{SQLServer, `ends\`, `N'ends\'`},
		{SQLServer, "a\\\nb", "N'a\\' + N'\nb'"},
		{SQLServer, "a\\\r\nb", "N'a\\' + N'\r\nb'"},
		{SQLServer, "a\\\n\\\nb", "N'a\\' + N'\n\\' + N'\nb'"},
		{SQLServer, "o'\\\nb", "N'o''\\' + N'\nb'"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.d.Literal(c.in))
	}
}

func Test_Check(t *testing.T) {
	require.Error(t, Check(""))
	require.Error(t, Check("a\x00b"))
	for _, name := range []string{"plain", `we"ird`, "we`ird", `we]ird`, `o'clock`, `back\slash`, `semi;colon`, `two$$dollars`, "new\nline", " ", "a.b"} {
		require.NoError(t, Check(name), name)
	}
}

func Test_ForDriver(t *testing.T) {
	for driver, want := range map[string]Dialect{"pgx": Postgres, "postgres": Postgres, "mysql": MySQL, "sqlserver": SQLServer} {
		got, err := ForDriver(driver)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := ForDriver("oracle")
	require.Error(t, err)
}

func Test_GoquIdentifiers(t *testing.T) {
	cases := []struct {
		d       Dialect
		goqu    string
		quote   string
		weird   string
		doubled string
	}{
		{Postgres, "postgres", `"`, `we"ird`, `we""ird`},
		{MySQL, "mysql", "`", "we`ird", "we``ird"},
		{SQLServer, "sqlserver", `"`, `we"ird`, `we""ird`},
	}
	for _, c := range cases {
		dialect := goqu.Dialect(c.goqu)
		q := c.quote

		// An ordinary name gives what goqu writes for its own identifiers.
		got, _, err := dialect.From(c.d.Table("s", "t")).Select(c.d.Col("c"), c.d.TableCol("a", "c")).ToSQL()
		require.NoError(t, err)
		want, _, err := dialect.From(goqu.S("s").Table("t")).Select(goqu.C("c"), goqu.T("a").Col("c")).ToSQL()
		require.NoError(t, err)
		require.Equal(t, want, got)

		// A name holding the quote character is written as one identifier.
		got, _, err = dialect.From(c.d.Table(c.weird, c.weird)).Select(c.d.Col(c.weird), c.d.TableCol(c.weird, c.weird)).ToSQL()
		require.NoError(t, err)
		require.Equal(t,
			"SELECT "+q+c.doubled+q+", "+q+c.doubled+q+"."+q+c.doubled+q+
				" FROM "+q+c.doubled+q+"."+q+c.doubled+q, got)

		// A dot belongs to the name.
		got, _, err = dialect.From(c.d.Table("a.b", "c.d")).Select(c.d.Col("e.f")).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT "+q+"e.f"+q+" FROM "+q+"a.b"+q+"."+q+"c.d"+q, got)

		// An empty schema gives the table alone.
		got, _, err = dialect.From(c.d.Table("", "t")).Select(goqu.Star()).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+"t"+q, got)
	}
}
