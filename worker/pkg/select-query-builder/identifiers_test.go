package selectquerybuilder

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/doug-martin/goqu/v9"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/require"
)

// Each engine is given a schema, tables and columns that hold the quote character its
// statements are written with: " for PostgreSQL, ` for MySQL, and " for SQL Server, whose
// statements goqu writes between double quotes. Two SQL Server names also hold a closing
// bracket, which is an ordinary character between double quotes.
//
// names gives each name as the catalog holds it, and quoted the same names as the
// statements must hold them. The expected statements name them {schema}, {station}, …
type oddNames struct {
	driver string
	names  map[string]string
	quoted map[string]string
}

var (
	oddPostgres = oddNames{
		driver: sqlmanager_shared.PostgresDriver,
		names: map[string]string{
			"schema": `sh"op`, "station": `sta"tion`, "visit": `vi"sit`, "report": `re"port`, "tag": `ta"g`,
			"id": `i"d`, "year": `ye"ar`, "code": `co"de`,
			"station_id": `sta"tion_id`, "station_year": `sta"tion_year`, "station_code": `sta"tion_code`,
			"previous_id": `pre"vious_id`,
		},
		quoted: map[string]string{
			"schema": `"sh""op"`, "station": `"sta""tion"`, "visit": `"vi""sit"`, "report": `"re""port"`, "tag": `"ta""g"`,
			"id": `"i""d"`, "year": `"ye""ar"`, "code": `"co""de"`,
			"station_id": `"sta""tion_id"`, "station_year": `"sta""tion_year"`, "station_code": `"sta""tion_code"`,
			"previous_id": `"pre""vious_id"`,
		},
	}
	oddMysql = oddNames{
		driver: sqlmanager_shared.MysqlDriver,
		names: map[string]string{
			"schema": "sh`op", "station": "sta`tion", "visit": "vi`sit", "report": "re`port", "tag": "ta`g",
			"id": "i`d", "year": "ye`ar", "code": "co`de",
			"station_id": "sta`tion_id", "station_year": "sta`tion_year", "station_code": "sta`tion_code",
			"previous_id": "pre`vious_id",
		},
		quoted: map[string]string{
			"schema": "`sh``op`", "station": "`sta``tion`", "visit": "`vi``sit`", "report": "`re``port`", "tag": "`ta``g`",
			"id": "`i``d`", "year": "`ye``ar`", "code": "`co``de`",
			"station_id": "`sta``tion_id`", "station_year": "`sta``tion_year`", "station_code": "`sta``tion_code`",
			"previous_id": "`pre``vious_id`",
		},
	}
	oddSqlServer = oddNames{
		driver: sqlmanager_shared.MssqlDriver,
		names: map[string]string{
			"schema": `sh"op`, "station": `sta"t]ion`, "visit": `vi"sit`, "report": `re"port`, "tag": `ta"g`,
			"id": `i"]d`, "year": `ye"ar`, "code": `co"de`,
			"station_id": `sta"tion_id`, "station_year": `sta"tion_year`, "station_code": `sta"tion_code`,
			"previous_id": `pre"vious_id`,
		},
		quoted: map[string]string{
			"schema": `"sh""op"`, "station": `"sta""t]ion"`, "visit": `"vi""sit"`, "report": `"re""port"`, "tag": `"ta""g"`,
			"id": `"i""]d"`, "year": `"ye""ar"`, "code": `"co""de"`,
			"station_id": `"sta""tion_id"`, "station_year": `"sta""tion_year"`, "station_code": `"sta""tion_code"`,
			"previous_id": `"pre""vious_id"`,
		},
	}
	oddEngines = []oddNames{oddPostgres, oddMysql, oddSqlServer}
)

// key gives the key of a table: its schema, a dot, its name.
func (n oddNames) key(table string) string { return n.names["schema"] + "." + n.names[table] }

// columns gives the names of the columns.
func (n oddNames) columns(columns ...string) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		if name, ok := n.names[column]; ok {
			out[i] = name
		} else {
			out[i] = column
		}
	}
	return out
}

// statement writes the names into an expected statement.
func (n oddNames) statement(template string) string {
	pairs := make([]string, 0, 2*len(n.quoted))
	for name, quoted := range n.quoted {
		pairs = append(pairs, "{"+name+"}", quoted)
	}
	return strings.NewReplacer(pairs...).Replace(template)
}

// configs gives the tables of the cases below. The station table has a two-column key and
// a clause of its own. A visit references its station, and the visit before it. A report
// references its station by the two columns of its key. A tag references a station by its
// code, which is not its key: a foreign key the job configuration declares.
func (n oddNames) configs(t *testing.T) []*runconfigs.RunConfig {
	t.Helper()
	reference := func(parent string, notNull []bool, columns, referenced []string) *sqlmanager_shared.ForeignConstraint {
		return &sqlmanager_shared.ForeignConstraint{
			Columns: n.columns(columns...), NotNullable: notNull,
			ForeignKey: &sqlmanager_shared.ForeignKey{Table: n.key(parent), Columns: n.columns(referenced...)},
		}
	}
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{
			n.key("visit"): {
				reference("station", []bool{true}, []string{"station_id"}, []string{"id"}),
				reference("visit", []bool{false}, []string{"previous_id"}, []string{"id"}),
			},
			n.key("report"): {
				reference("station", []bool{false, false}, []string{"station_id", "station_year"}, []string{"id", "year"}),
			},
			n.key("tag"): {
				reference("station", []bool{false}, []string{"station_code"}, []string{"code"}),
			},
		},
		map[string]string{n.key("station"): "kind = 'A' OR kind = 'B'"},
		map[string][]string{
			n.key("station"): n.columns("id", "year"),
			n.key("visit"):   n.columns("id"),
			n.key("report"):  n.columns("id"),
			n.key("tag"):     n.columns("id"),
		},
		map[string][]string{
			n.key("station"): n.columns("id", "year", "code", "kind"),
			n.key("visit"):   n.columns("id", "station_id", "previous_id"),
			n.key("report"):  n.columns("id", "station_id", "station_year"),
			n.key("tag"):     n.columns("id", "station_code"),
		},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	return configs
}

var generatedAlias = regexp.MustCompile(`t_[0-9a-f]{16}`)

// numbered names the aliases the builder generates t_1, t_2, … in the order a statement
// first holds them.
func numbered(sql string) string {
	numbers := map[string]string{}
	return generatedAlias.ReplaceAllStringFunc(sql, func(alias string) string {
		if _, ok := numbers[alias]; !ok {
			numbers[alias] = "t_" + strconv.Itoa(len(numbers)+1)
		}
		return numbers[alias]
	})
}

// buildInsertQuery builds the read of one table with a builder of its own, so that the
// aliases it generates do not depend on the other tables.
func buildInsertQuery(
	t *testing.T,
	driver string,
	configs []*runconfigs.RunConfig,
	table string,
	subsetByForeignKeyConstraints bool,
) (query, pageQuery string) {
	t.Helper()
	for _, config := range configs {
		if config.Id() != table+".insert" {
			continue
		}
		query, _, pageQuery, _, err := NewSelectQueryBuilder("public", driver, subsetByForeignKeyConstraints, 100).
			WithRunConfigs(configs).
			BuildQuery(config)
		require.NoError(t, err)
		return numbered(query), numbered(pageQuery)
	}
	require.Fail(t, "no run config for "+table)
	return "", ""
}

func Test_BuildQuery_RootTableNamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]struct{ query, page string }{
		sqlmanager_shared.PostgresDriver: {
			query: `SELECT {station}.{id}, {station}.{year}, {station}.{code}, {station}."kind" FROM {schema}.{station} AS {station}` +
				` WHERE ({station}.kind = 'A' OR {station}.kind = 'B') ORDER BY {station}.{id} ASC, {station}.{year} ASC LIMIT 100`,
			page: `SELECT {station}.{id}, {station}.{year}, {station}.{code}, {station}."kind" FROM {schema}.{station} AS {station}` +
				` WHERE (({station}.kind = 'A' OR {station}.kind = 'B') AND (({station}.{id} > $1) OR (({station}.{id} = $2) AND ({station}.{year} > $3))))` +
				` ORDER BY {station}.{id} ASC, {station}.{year} ASC LIMIT $4`,
		},
		sqlmanager_shared.MysqlDriver: {
			query: "SELECT {station}.{id}, {station}.{year}, {station}.{code}, {station}.`kind` FROM {schema}.{station} AS {station}" +
				" WHERE ({station}.kind = 'A' or {station}.kind = 'B') ORDER BY {station}.{id} ASC, {station}.{year} ASC LIMIT 100",
			page: "SELECT {station}.{id}, {station}.{year}, {station}.{code}, {station}.`kind` FROM {schema}.{station} AS {station}" +
				" WHERE (({station}.kind = 'A' or {station}.kind = 'B') AND (({station}.{id} > ?) OR (({station}.{id} = ?) AND ({station}.{year} > ?))))" +
				" ORDER BY {station}.{id} ASC, {station}.{year} ASC LIMIT ?",
		},
		sqlmanager_shared.MssqlDriver: {
			query: `SELECT  TOP (100) {station}.{id}, {station}.{year}, {station}.{code}, {station}."kind" FROM {schema}.{station} AS {station}` +
				` WHERE ({station}."kind" = 'A' OR {station}."kind" = 'B') ORDER BY {station}.{id} ASC, {station}.{year} ASC`,
			page: `SELECT  TOP (CAST(@p1 AS INT)) {station}.{id}, {station}.{year}, {station}.{code}, {station}."kind" FROM {schema}.{station} AS {station}` +
				` WHERE (({station}."kind" = 'A' OR {station}."kind" = 'B') AND (({station}.{id} > @p2) OR (({station}.{id} = @p3) AND ({station}.{year} > @p4))))` +
				` ORDER BY {station}.{id} ASC, {station}.{year} ASC`,
		},
	}
	for _, n := range oddEngines {
		for _, byForeignKeys := range []bool{true, false} {
			t.Run(n.driver+" by foreign keys "+strconv.FormatBool(byForeignKeys), func(t *testing.T) {
				query, page := buildInsertQuery(t, n.driver, n.configs(t), n.key("station"), byForeignKeys)
				require.Equal(t, n.statement(expected[n.driver].query), query)
				require.Equal(t, n.statement(expected[n.driver].page), page)
			})
		}
	}
}

// A visit is joined to its station on a single column. The visit before it is a nullable
// reference to the table itself: it is read as NULL when that visit is not selected.
func Test_BuildQuery_JoinAndSelfReferenceNamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]string{
		sqlmanager_shared.PostgresDriver: `SELECT {visit}.{id}, {visit}.{station_id},` +
			` CASE  WHEN (({visit}.{previous_id} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{visit} AS "t_1"` +
			` INNER JOIN {schema}.{station} AS "t_2" ON ("t_2".{id} = "t_1".{station_id})` +
			` WHERE ((t_2.kind = 'A' OR t_2.kind = 'B') AND ("t_1".{id} = {visit}.{previous_id})))) THEN {visit}.{previous_id} END AS {previous_id}` +
			` FROM {schema}.{visit} AS {visit} INNER JOIN {schema}.{station} AS "t_3" ON ("t_3".{id} = {visit}.{station_id})` +
			` WHERE (t_3.kind = 'A' OR t_3.kind = 'B') ORDER BY {visit}.{id} ASC LIMIT 100`,
		sqlmanager_shared.MysqlDriver: "SELECT {visit}.{id}, {visit}.{station_id}," +
			" CASE  WHEN (({visit}.{previous_id} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{visit} AS `t_1`" +
			" INNER JOIN {schema}.{station} AS `t_2` ON (`t_2`.{id} = `t_1`.{station_id})" +
			" WHERE ((t_2.kind = 'A' or t_2.kind = 'B') AND (`t_1`.{id} = {visit}.{previous_id})))) THEN {visit}.{previous_id} END AS {previous_id}" +
			" FROM {schema}.{visit} AS {visit} INNER JOIN {schema}.{station} AS `t_3` ON (`t_3`.{id} = {visit}.{station_id})" +
			" WHERE (t_3.kind = 'A' or t_3.kind = 'B') ORDER BY {visit}.{id} ASC LIMIT 100",
		sqlmanager_shared.MssqlDriver: `SELECT  TOP (100) {visit}.{id}, {visit}.{station_id},` +
			` CASE  WHEN (({visit}.{previous_id} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{visit} AS "t_1"` +
			` INNER JOIN {schema}.{station} AS "t_2" ON ("t_2".{id} = "t_1".{station_id})` +
			` WHERE (("t_2"."kind" = 'A' OR "t_2"."kind" = 'B') AND ("t_1".{id} = {visit}.{previous_id})))) THEN {visit}.{previous_id} END AS {previous_id}` +
			` FROM {schema}.{visit} AS {visit} INNER JOIN {schema}.{station} AS "t_3" ON ("t_3".{id} = {visit}.{station_id})` +
			` WHERE ("t_3"."kind" = 'A' OR "t_3"."kind" = 'B') ORDER BY {visit}.{id} ASC`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			query, _ := buildInsertQuery(t, n.driver, n.configs(t), n.key("visit"), true)
			require.Equal(t, n.statement(expected[n.driver]), query)
		})
	}
}

// A report references its station by two nullable columns: the join keeps the reports that
// reference nothing, and each column is read as NULL when the station is not selected.
func Test_BuildQuery_TwoColumnForeignKeyNamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]string{
		sqlmanager_shared.PostgresDriver: `SELECT {report}.{id},` +
			` CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE ((t_1.kind = 'A' OR t_1.kind = 'B') AND ("t_1".{id} = {report}.{station_id}) AND ("t_1".{year} = {report}.{station_year}))))` +
			` THEN {report}.{station_id} END AS {station_id},` +
			` CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE ((t_1.kind = 'A' OR t_1.kind = 'B') AND ("t_1".{id} = {report}.{station_id}) AND ("t_1".{year} = {report}.{station_year}))))` +
			` THEN {report}.{station_year} END AS {station_year}` +
			` FROM {schema}.{report} AS {report} LEFT JOIN {schema}.{station} AS "t_2"` +
			` ON (("t_2".{id} = {report}.{station_id}) AND ("t_2".{year} = {report}.{station_year}))` +
			` WHERE (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR (t_2.kind = 'A' OR t_2.kind = 'B'))` +
			` ORDER BY {report}.{id} ASC LIMIT 100`,
		sqlmanager_shared.MysqlDriver: "SELECT {report}.{id}," +
			" CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS `t_1`" +
			" WHERE ((t_1.kind = 'A' or t_1.kind = 'B') AND (`t_1`.{id} = {report}.{station_id}) AND (`t_1`.{year} = {report}.{station_year}))))" +
			" THEN {report}.{station_id} END AS {station_id}," +
			" CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS `t_1`" +
			" WHERE ((t_1.kind = 'A' or t_1.kind = 'B') AND (`t_1`.{id} = {report}.{station_id}) AND (`t_1`.{year} = {report}.{station_year}))))" +
			" THEN {report}.{station_year} END AS {station_year}" +
			" FROM {schema}.{report} AS {report} LEFT JOIN {schema}.{station} AS `t_2`" +
			" ON ((`t_2`.{id} = {report}.{station_id}) AND (`t_2`.{year} = {report}.{station_year}))" +
			" WHERE (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR (t_2.kind = 'A' or t_2.kind = 'B'))" +
			" ORDER BY {report}.{id} ASC LIMIT 100",
		sqlmanager_shared.MssqlDriver: `SELECT  TOP (100) {report}.{id},` +
			` CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE (("t_1"."kind" = 'A' OR "t_1"."kind" = 'B') AND ("t_1".{id} = {report}.{station_id}) AND ("t_1".{year} = {report}.{station_year}))))` +
			` THEN {report}.{station_id} END AS {station_id},` +
			` CASE  WHEN (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE (("t_1"."kind" = 'A' OR "t_1"."kind" = 'B') AND ("t_1".{id} = {report}.{station_id}) AND ("t_1".{year} = {report}.{station_year}))))` +
			` THEN {report}.{station_year} END AS {station_year}` +
			` FROM {schema}.{report} AS {report} LEFT JOIN {schema}.{station} AS "t_2"` +
			` ON (("t_2".{id} = {report}.{station_id}) AND ("t_2".{year} = {report}.{station_year}))` +
			` WHERE (({report}.{station_id} IS NULL) OR ({report}.{station_year} IS NULL) OR ("t_2"."kind" = 'A' OR "t_2"."kind" = 'B'))` +
			` ORDER BY {report}.{id} ASC`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			query, _ := buildInsertQuery(t, n.driver, n.configs(t), n.key("report"), true)
			require.Equal(t, n.statement(expected[n.driver]), query)
		})
	}
}

// A tag references a station by a column that is not its key, as a foreign key declared
// in the job configuration does: the referenced column is a name like the others.
func Test_BuildQuery_VirtualForeignKeyNamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]string{
		sqlmanager_shared.PostgresDriver: `SELECT {tag}.{id},` +
			` CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE ((t_1.kind = 'A' OR t_1.kind = 'B') AND ("t_1".{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}` +
			` FROM {schema}.{tag} AS {tag} LEFT JOIN {schema}.{station} AS "t_2" ON ("t_2".{code} = {tag}.{station_code})` +
			` WHERE (({tag}.{station_code} IS NULL) OR (t_2.kind = 'A' OR t_2.kind = 'B')) ORDER BY {tag}.{id} ASC LIMIT 100`,
		sqlmanager_shared.MysqlDriver: "SELECT {tag}.{id}," +
			" CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS `t_1`" +
			" WHERE ((t_1.kind = 'A' or t_1.kind = 'B') AND (`t_1`.{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}" +
			" FROM {schema}.{tag} AS {tag} LEFT JOIN {schema}.{station} AS `t_2` ON (`t_2`.{code} = {tag}.{station_code})" +
			" WHERE (({tag}.{station_code} IS NULL) OR (t_2.kind = 'A' or t_2.kind = 'B')) ORDER BY {tag}.{id} ASC LIMIT 100",
		sqlmanager_shared.MssqlDriver: `SELECT  TOP (100) {tag}.{id},` +
			` CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE (("t_1"."kind" = 'A' OR "t_1"."kind" = 'B') AND ("t_1".{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}` +
			` FROM {schema}.{tag} AS {tag} LEFT JOIN {schema}.{station} AS "t_2" ON ("t_2".{code} = {tag}.{station_code})` +
			` WHERE (({tag}.{station_code} IS NULL) OR ("t_2"."kind" = 'A' OR "t_2"."kind" = 'B')) ORDER BY {tag}.{id} ASC`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			query, _ := buildInsertQuery(t, n.driver, n.configs(t), n.key("tag"), true)
			require.Equal(t, n.statement(expected[n.driver]), query)
		})
	}
}

// Without the subset by foreign keys, a table is not joined to its parent: only the value
// of a nullable reference is read through the clause of the parent.
func Test_BuildQuery_ReferenceReadThroughTheParentClause(t *testing.T) {
	expected := map[string]string{
		sqlmanager_shared.PostgresDriver: `SELECT {tag}.{id},` +
			` CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE ((t_1.kind = 'A' OR t_1.kind = 'B') AND ("t_1".{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}` +
			` FROM {schema}.{tag} AS {tag} ORDER BY {tag}.{id} ASC LIMIT 100`,
		sqlmanager_shared.MysqlDriver: "SELECT {tag}.{id}," +
			" CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS `t_1`" +
			" WHERE ((t_1.kind = 'A' or t_1.kind = 'B') AND (`t_1`.{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}" +
			" FROM {schema}.{tag} AS {tag} ORDER BY {tag}.{id} ASC LIMIT 100",
		sqlmanager_shared.MssqlDriver: `SELECT  TOP (100) {tag}.{id},` +
			` CASE  WHEN (({tag}.{station_code} IS NULL) OR EXISTS (SELECT 1 FROM {schema}.{station} AS "t_1"` +
			` WHERE (("t_1"."kind" = 'A' OR "t_1"."kind" = 'B') AND ("t_1".{code} = {tag}.{station_code})))) THEN {tag}.{station_code} END AS {station_code}` +
			` FROM {schema}.{tag} AS {tag} ORDER BY {tag}.{id} ASC`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			query, _ := buildInsertQuery(t, n.driver, n.configs(t), n.key("tag"), false)
			require.Equal(t, n.statement(expected[n.driver]), query)
		})
	}
}

// A column named * is a column, and a dot in a column name does not separate: selected,
// ordered, compared by the page cursor, joined on and projected, each is one identifier.
func Test_BuildQuery_StarAndDottedColumnsAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]struct{ query, page string }{
		sqlmanager_shared.PostgresDriver: {
			query: `SELECT "item"."*", "item"."a.b",` +
				` CASE  WHEN (("item"."c.d" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_1"` +
				` WHERE ((t_1.kind = 'A') AND ("t_1"."e.f" = "item"."c.d")))) THEN "item"."c.d" END AS "c.d",` +
				` CASE  WHEN (("item"."g*" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_2"` +
				` WHERE ((t_2.kind = 'A') AND ("t_2"."*" = "item"."g*")))) THEN "item"."g*" END AS "g*"` +
				` FROM "shop"."item" AS "item" ORDER BY "item"."*" ASC, "item"."a.b" ASC LIMIT 100`,
			page: `SELECT "item"."*", "item"."a.b",` +
				` CASE  WHEN (("item"."c.d" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_1"` +
				` WHERE ((t_1.kind = 'A') AND ("t_1"."e.f" = "item"."c.d")))) THEN "item"."c.d" END AS "c.d",` +
				` CASE  WHEN (("item"."g*" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_2"` +
				` WHERE ((t_2.kind = 'A') AND ("t_2"."*" = "item"."g*")))) THEN "item"."g*" END AS "g*"` +
				` FROM "shop"."item" AS "item" WHERE (("item"."*" > $1) OR (("item"."*" = $2) AND ("item"."a.b" > $3)))` +
				` ORDER BY "item"."*" ASC, "item"."a.b" ASC LIMIT $4`,
		},
		sqlmanager_shared.MysqlDriver: {
			query: "SELECT `item`.`*`, `item`.`a.b`," +
				" CASE  WHEN ((`item`.`c.d` IS NULL) OR EXISTS (SELECT 1 FROM `shop`.`box` AS `t_1`" +
				" WHERE ((t_1.kind = 'A') AND (`t_1`.`e.f` = `item`.`c.d`)))) THEN `item`.`c.d` END AS `c.d`," +
				" CASE  WHEN ((`item`.`g*` IS NULL) OR EXISTS (SELECT 1 FROM `shop`.`box` AS `t_2`" +
				" WHERE ((t_2.kind = 'A') AND (`t_2`.`*` = `item`.`g*`)))) THEN `item`.`g*` END AS `g*`" +
				" FROM `shop`.`item` AS `item` ORDER BY `item`.`*` ASC, `item`.`a.b` ASC LIMIT 100",
			page: "SELECT `item`.`*`, `item`.`a.b`," +
				" CASE  WHEN ((`item`.`c.d` IS NULL) OR EXISTS (SELECT 1 FROM `shop`.`box` AS `t_1`" +
				" WHERE ((t_1.kind = 'A') AND (`t_1`.`e.f` = `item`.`c.d`)))) THEN `item`.`c.d` END AS `c.d`," +
				" CASE  WHEN ((`item`.`g*` IS NULL) OR EXISTS (SELECT 1 FROM `shop`.`box` AS `t_2`" +
				" WHERE ((t_2.kind = 'A') AND (`t_2`.`*` = `item`.`g*`)))) THEN `item`.`g*` END AS `g*`" +
				" FROM `shop`.`item` AS `item` WHERE ((`item`.`*` > ?) OR ((`item`.`*` = ?) AND (`item`.`a.b` > ?)))" +
				" ORDER BY `item`.`*` ASC, `item`.`a.b` ASC LIMIT ?",
		},
		sqlmanager_shared.MssqlDriver: {
			query: `SELECT  TOP (100) "item"."*", "item"."a.b",` +
				` CASE  WHEN (("item"."c.d" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_1"` +
				` WHERE (("t_1"."kind" = 'A') AND ("t_1"."e.f" = "item"."c.d")))) THEN "item"."c.d" END AS "c.d",` +
				` CASE  WHEN (("item"."g*" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_2"` +
				` WHERE (("t_2"."kind" = 'A') AND ("t_2"."*" = "item"."g*")))) THEN "item"."g*" END AS "g*"` +
				` FROM "shop"."item" AS "item" ORDER BY "item"."*" ASC, "item"."a.b" ASC`,
			page: `SELECT  TOP (CAST(@p1 AS INT)) "item"."*", "item"."a.b",` +
				` CASE  WHEN (("item"."c.d" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_1"` +
				` WHERE (("t_1"."kind" = 'A') AND ("t_1"."e.f" = "item"."c.d")))) THEN "item"."c.d" END AS "c.d",` +
				` CASE  WHEN (("item"."g*" IS NULL) OR EXISTS (SELECT 1 FROM "shop"."box" AS "t_2"` +
				` WHERE (("t_2"."kind" = 'A') AND ("t_2"."*" = "item"."g*")))) THEN "item"."g*" END AS "g*"` +
				` FROM "shop"."item" AS "item" WHERE (("item"."*" > @p2) OR (("item"."*" = @p3) AND ("item"."a.b" > @p4)))` +
				` ORDER BY "item"."*" ASC, "item"."a.b" ASC`,
		},
	}
	for _, driver := range []string{sqlmanager_shared.PostgresDriver, sqlmanager_shared.MysqlDriver, sqlmanager_shared.MssqlDriver} {
		t.Run(driver, func(t *testing.T) {
			configs, err := runconfigs.BuildRunConfigs(
				map[string][]*sqlmanager_shared.ForeignConstraint{
					"shop.item": {
						{
							Columns: []string{"c.d"}, NotNullable: []bool{false},
							ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.box", Columns: []string{"e.f"}},
						},
						{
							Columns: []string{"g*"}, NotNullable: []bool{false},
							ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.box", Columns: []string{"*"}},
						},
					},
				},
				map[string]string{"shop.box": "kind = 'A'"},
				map[string][]string{"shop.item": {"*", "a.b"}, "shop.box": {"e.f"}},
				map[string][]string{"shop.item": {"*", "a.b", "c.d", "g*"}, "shop.box": {"e.f", "*", "kind"}},
				map[string][][]string{}, map[string][][]string{},
			)
			require.NoError(t, err)
			query, page := buildInsertQuery(t, driver, configs, "shop.item", false)
			require.Equal(t, expected[driver].query, query)
			require.Equal(t, expected[driver].page, page)
		})
	}
}

func Test_BuildQuery_HarmlessOdditiesAreWrittenAsTheyAre(t *testing.T) {
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{},
		map[string]string{`o'clock.back\slash`: "id = 1"},
		map[string][]string{`o'clock.back\slash`: {"semi;colon"}},
		map[string][]string{`o'clock.back\slash`: {"semi;colon", "a space", "we`ird", "we]ird", "two$$dollars"}},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)

	query, _ := buildInsertQuery(t, sqlmanager_shared.PostgresDriver, configs, `o'clock.back\slash`, false)
	require.Equal(t,
		`SELECT "back\slash"."semi;colon", "back\slash"."a space", "back\slash"."we`+"`"+`ird", "back\slash"."we]ird", "back\slash"."two$$dollars"`+
			` FROM "o'clock"."back\slash" AS "back\slash" WHERE ("back\slash".id = 1) ORDER BY "back\slash"."semi;colon" ASC LIMIT 100`,
		query)

	query, _ = buildInsertQuery(t, sqlmanager_shared.MssqlDriver, configs, `o'clock.back\slash`, false)
	require.Equal(t,
		`SELECT  TOP (100) "back\slash"."semi;colon", "back\slash"."a space", "back\slash"."we`+"`"+`ird", "back\slash"."we]ird", "back\slash"."two$$dollars"`+
			` FROM "o'clock"."back\slash" AS "back\slash" WHERE ("back\slash"."id" = 1) ORDER BY "back\slash"."semi;colon" ASC`,
		query)

	query, _ = buildInsertQuery(t, sqlmanager_shared.MysqlDriver, configs, `o'clock.back\slash`, false)
	require.Equal(t,
		"SELECT `back\\slash`.`semi;colon`, `back\\slash`.`a space`, `back\\slash`.`we``ird`, `back\\slash`.`we]ird`, `back\\slash`.`two$$dollars`"+
			" FROM `o'clock`.`back\\slash` AS `back\\slash` WHERE (`back\\slash`.id = 1) ORDER BY `back\\slash`.`semi;colon` ASC LIMIT 100",
		query)
}

// A joined table is known by its key. The key of a schema and a table made of letters,
// digits and underscores is written as goqu writes it from a string, and so is a key with
// more dots, or with none; each part of a key is one identifier whatever it holds.
func Test_tableOfKey(t *testing.T) {
	render := func(t *testing.T, qb *QueryBuilder, table any) string {
		t.Helper()
		sql, _, err := qb.getDialect().From(table).ToSQL()
		require.NoError(t, err)
		return sql
	}
	for _, driver := range []string{sqlmanager_shared.PostgresDriver, sqlmanager_shared.MysqlDriver, sqlmanager_shared.MssqlDriver} {
		qb := NewSelectQueryBuilder("public", driver, true, 100)
		for _, key := range []string{"shop.station", "Shop.Station_2", "station", "shop.pro.duct", "a.b.c.d"} {
			t.Run(driver+" "+key, func(t *testing.T) {
				table, err := qb.tableOfKey(key, "t_1")
				require.NoError(t, err)
				require.Equal(t, render(t, qb, goqu.I(key).As("t_1")), render(t, qb, table))
			})
		}
	}

	for name, tc := range map[string]struct{ driver, key, want string }{
		"postgres":         {sqlmanager_shared.PostgresDriver, `sh"op.sta"tion`, `SELECT * FROM "sh""op"."sta""tion" AS "t_1"`},
		"mysql":            {sqlmanager_shared.MysqlDriver, "sh`op.sta`tion", "SELECT * FROM `sh``op`.`sta``tion` AS `t_1`"},
		"sqlserver":        {sqlmanager_shared.MssqlDriver, `sh"op.sta"t]ion`, `SELECT * FROM "sh""op"."sta""t]ion" AS "t_1"`},
		"three parts":      {sqlmanager_shared.PostgresDriver, `sh"op.pro"duct.x"y`, `SELECT * FROM "sh""op"."pro""duct"."x""y" AS "t_1"`},
		"no schema":        {sqlmanager_shared.PostgresDriver, `sta"tion`, `SELECT * FROM "sta""tion" AS "t_1"`},
		"a table named *":  {sqlmanager_shared.PostgresDriver, "shop.*", `SELECT * FROM "shop"."*" AS "t_1"`},
		"a third part *":   {sqlmanager_shared.PostgresDriver, "a.b.*", `SELECT * FROM "a"."b"."*" AS "t_1"`},
		"harmless oddness": {sqlmanager_shared.PostgresDriver, `o'clock.back\slash semi;colon`, `SELECT * FROM "o'clock"."back\slash semi;colon" AS "t_1"`},
	} {
		t.Run(name, func(t *testing.T) {
			qb := NewSelectQueryBuilder("public", tc.driver, true, 100)
			table, err := qb.tableOfKey(tc.key, "t_1")
			require.NoError(t, err)
			require.Equal(t, tc.want, render(t, qb, table))
		})
	}

	t.Run("a key without a table name is refused", func(t *testing.T) {
		qb := NewSelectQueryBuilder("public", sqlmanager_shared.PostgresDriver, true, 100)
		for _, key := range []string{"", "shop.", "a.b."} {
			_, err := qb.tableOfKey(key, "t_1")
			require.ErrorContains(t, err, "table name: a name cannot be empty", key)
		}
	})
}

// A name no engine takes, and a driver without a dialect, are refused with an error that
// says so.
func Test_BuildQuery_RefusesWhatItCannotWrite(t *testing.T) {
	t.Run("a column without a name", func(t *testing.T) {
		configs, err := runconfigs.BuildRunConfigs(
			map[string][]*sqlmanager_shared.ForeignConstraint{},
			map[string]string{},
			map[string][]string{"shop.station": {"id"}},
			map[string][]string{"shop.station": {"id", ""}},
			map[string][][]string{}, map[string][][]string{},
		)
		require.NoError(t, err)
		for _, driver := range []string{sqlmanager_shared.PostgresDriver, sqlmanager_shared.MysqlDriver, sqlmanager_shared.MssqlDriver} {
			_, err = BuildSelectQueryMap(driver, configs, false, 100)
			require.ErrorContains(t, err, "column name: a name cannot be empty", driver)
		}
	})
	t.Run("a referenced column holding a NUL byte", func(t *testing.T) {
		configs, err := runconfigs.BuildRunConfigs(
			map[string][]*sqlmanager_shared.ForeignConstraint{
				"shop.visit": {{
					Columns: []string{"station_id"}, NotNullable: []bool{true},
					ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.station", Columns: []string{"i\x00d"}},
				}},
			},
			map[string]string{"shop.station": "id = 1"},
			map[string][]string{"shop.station": {"id"}, "shop.visit": {"id"}},
			map[string][]string{"shop.station": {"id", "i\x00d"}, "shop.visit": {"id", "station_id"}},
			map[string][][]string{}, map[string][][]string{},
		)
		require.NoError(t, err)
		for _, config := range configs {
			if config.Id() != "shop.visit.insert" {
				continue
			}
			_, _, _, _, err = NewSelectQueryBuilder("public", sqlmanager_shared.PostgresDriver, true, 100).
				WithRunConfigs(configs).
				BuildQuery(config)
		}
		require.ErrorContains(t, err, "column name: a name cannot hold a NUL byte")
	})
	t.Run("a driver without a dialect", func(t *testing.T) {
		configs, err := runconfigs.BuildRunConfigs(
			map[string][]*sqlmanager_shared.ForeignConstraint{},
			map[string]string{},
			map[string][]string{"shop.station": {"id"}},
			map[string][]string{"shop.station": {"id"}},
			map[string][][]string{}, map[string][][]string{},
		)
		require.NoError(t, err)
		_, err = BuildSelectQueryMap("sqlite3", configs, false, 100)
		require.ErrorContains(t, err, `no SQL dialect for driver "sqlite3"`)
	})
}

// On MySQL the table, or the alias, that names the columns of a where clause is written as
// one identifier whatever it holds. A name of letters, digits and underscores is written
// as it always was.
func Test_BuildQuery_MysqlWhereQualifierIsWrittenAsOneIdentifier(t *testing.T) {
	const columns = "SELECT {t}.`id`, {t}.`autocommit` FROM `shop`.{t} AS {t} WHERE (%s) ORDER BY {t}.`id` ASC LIMIT 100"
	for name, tc := range map[string]struct{ table, clause, where string }{
		"a name made of at signs":      {"@@session", "autocommit = 1", "`@@session`.autocommit = 1"},
		"a name with a backtick":       {"@@a`b", "id = 1", "`@@a``b`.id = 1"},
		"a name with one at sign":      {"a@b", "id = 1", "`a@b`.id = 1"},
		"a name with a plain backtick": {"we`ird", "id = 1", "`we``ird`.id = 1"},
		"an ordinary name":             {"customers", "id = 1", "customers.id = 1"},
		"a function and a list":        {"customers", "lower(email) = 'a' and id in (1, 2)", "lower(customers.email) = 'a' and customers.id in (1, 2)"},
		"a subquery":                   {"customers", "id in (select customer_id from orders where total > 10)", "customers.id in (select customer_id from orders where total > 10)"},
		"a column already qualified":   {"customers", "customers.id = 1", "customers.id = 1"},
	} {
		for _, byForeignKeys := range []bool{true, false} {
			t.Run(name+" by foreign keys "+strconv.FormatBool(byForeignKeys), func(t *testing.T) {
				key := "shop." + tc.table
				configs, err := runconfigs.BuildRunConfigs(
					map[string][]*sqlmanager_shared.ForeignConstraint{},
					map[string]string{key: tc.clause},
					map[string][]string{key: {"id"}},
					map[string][]string{key: {"id", "autocommit"}},
					map[string][][]string{}, map[string][][]string{},
				)
				require.NoError(t, err)
				quotedTable := "`" + strings.ReplaceAll(tc.table, "`", "``") + "`"
				query, _ := buildInsertQuery(t, sqlmanager_shared.MysqlDriver, configs, key, byForeignKeys)
				require.Equal(t, strings.ReplaceAll(strings.ReplaceAll(columns, "%s", tc.where), "{t}", quotedTable), query)
			})
		}
	}
}
