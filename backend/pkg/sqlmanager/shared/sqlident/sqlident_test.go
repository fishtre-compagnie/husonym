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
		{Postgres, ``, `''`},
		{Postgres, "line\nbreak", "'line\nbreak'"},
		{Postgres, `é☃`, `'é☃'`},
		// A backslash switches to the escape form, which reads the same under both settings
		// of standard_conforming_strings.
		{Postgres, `back\slash`, `E'back\\slash'`},
		{Postgres, `trail\`, `E'trail\\'`},
		{Postgres, `\'`, `E'\\'''`},
		{Postgres, `a\nb`, `E'a\\nb'`},
		{Postgres, "o'clock\\\nline", "E'o''clock\\\\\nline'"},
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
		{SQLServer, "a\\\rb", "N'a\\' + N'\rb'"},
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

var goquDialects = []struct {
	d     Dialect
	name  string
	q     string
	weird string
}{
	{Postgres, "postgres", `"`, `we"ird`},
	{MySQL, "mysql", "`", "we`ird"},
	{SQLServer, "sqlserver", `"`, `we"ird`},
}

func Test_GoquDotsStayInsideOneIdentifier(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		got, _, err := goqu.Dialect(c.name).From(goqu.T("x")).Select(c.d.TableCol("a.b", "c.d")).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT "+q+"a.b"+q+"."+q+"c.d"+q+` FROM `+q+"x"+q, got)
	}
}

func Test_GoquTableAloneDoublesTheQuote(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		doubled := c.weird[:3] + c.weird[2:]
		got, _, err := goqu.Dialect(c.name).From(c.d.Table("", c.weird)).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+doubled+q, got)
	}
}

func Test_GoquColumnNamedStar(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		star := q + "*" + q
		dialect := goqu.Dialect(c.name)
		col := c.d.Col("*")
		tc := c.d.TableCol("t", "*")

		got, _, err := dialect.From("t").Select(col, tc).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT "+star+", "+q+"t"+q+"."+star+" FROM "+q+"t"+q, got)

		got, _, err = dialect.Insert("t").Cols(col).Vals([]interface{}{1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "INSERT INTO "+q+"t"+q+" ("+star+") VALUES (1)", got)

		got, _, err = dialect.Update("t").Set(col.Set(2)).Where(col.Eq(1)).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "UPDATE "+q+"t"+q+" SET "+star+"=2 WHERE ("+star+" = 1)", got)

		got, _, err = dialect.From("t").Where(col.Gt(1), tc.IsNull()).Order(col.Asc(), tc.Desc()).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+"t"+q+" WHERE (("+star+" > 1) AND ("+q+"t"+q+"."+star+" IS NULL)) ORDER BY "+star+" ASC, "+q+"t"+q+"."+star+" DESC", got)

		got, _, err = dialect.From("t").Select(col.As("x")).Join(goqu.T("u"), goqu.On(col.Eq(tc))).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT "+star+" AS "+q+"x"+q+" FROM "+q+"t"+q+" INNER JOIN "+q+"u"+q+" ON ("+star+" = "+q+"t"+q+"."+star+")", got)
	}
}

// The forms below are the ones the package comment gives for the places where goqu takes a
// column name as a string.
func Test_GoquRecommendedForms(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		dialect := goqu.Dialect(c.name)
		a, b := c.d.Col(c.weird), c.d.Col("b")
		w := q + c.weird[:3] + c.weird[2:] + q

		got, _, err := dialect.Insert("t").Cols(a, b).Vals([]interface{}{1, 2}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "INSERT INTO "+q+"t"+q+" ("+w+", "+q+"b"+q+") VALUES (1, 2)", got)

		got, _, err = dialect.Update("t").Set(a.Set(1)).Where(a.Eq(3)).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "UPDATE "+q+"t"+q+" SET "+w+"=1 WHERE ("+w+" = 3)", got)

		got, _, err = dialect.From("t").Where(a.Eq(1)).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+"t"+q+" WHERE ("+w+" = 1)", got)
	}
}

func Test_GoquKeys(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		dialect := goqu.Dialect(c.name)
		w := q + c.weird[:3] + c.weird[2:] + q

		got, _, err := dialect.Update(c.d.Table("s", "t")).
			Set(goqu.Record{c.d.Key(c.weird): 1, c.d.Key("b"): 2}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "UPDATE "+q+"s"+q+"."+q+"t"+q+" SET "+q+"b"+q+"=2,"+w+"=1", got)

		got, _, err = dialect.Insert("t").Rows(goqu.Record{c.d.Key(c.weird): 1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "INSERT INTO "+q+"t"+q+" ("+w+") VALUES (1)", got)

		got, _, err = dialect.From("t").Where(goqu.Ex{c.d.Key(c.weird): 1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+"t"+q+" WHERE ("+w+" = 1)", got)

		// An ordinary name gives what the bare name gives.
		got, _, err = dialect.Update("t").Set(goqu.Record{c.d.Key("a"): 1, c.d.Key("b"): 2}).Where(goqu.Ex{c.d.Key("a"): 1}).ToSQL()
		require.NoError(t, err)
		want, _, err := dialect.Update("t").Set(goqu.Record{"a": 1, "b": 2}).Where(goqu.Ex{"a": 1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

// Known limit of string keys: goqu splits a key on dots, so a name holding a dot is
// written as several identifiers there. Names that may hold a dot use Col, whose
// expression form keeps the dot inside one identifier.
func Test_GoquKeysSplitOnDots(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		got, _, err := goqu.Dialect(c.name).Update("t").Set(goqu.Record{c.d.Key("a.b"): 1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "UPDATE "+q+"t"+q+" SET "+q+"a"+q+"."+q+"b"+q+"=1", got)
	}
}

// Known limit of string keys: goqu reads the key * as the star, not as a column name.
// A column that may be named * uses Col.
func Test_GoquKeyStarIsNotQuoted(t *testing.T) {
	for _, c := range goquDialects {
		q := c.q
		got, _, err := goqu.Dialect(c.name).From("t").Where(goqu.Ex{c.d.Key("*"): 1}).ToSQL()
		require.NoError(t, err)
		require.Equal(t, "SELECT * FROM "+q+"t"+q+" WHERE (* = 1)", got)
	}
}
