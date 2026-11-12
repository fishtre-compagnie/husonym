package selectquerybuilder

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/require"
)

// A user clause with a top-level OR must stay one condition once the builder ANDs its
// own next to it: the page cursor, and the clause of any other subset root.
func Test_BuildQuery_WhereWithTopLevelOr(t *testing.T) {
	for _, driver := range []string{sqlmanager_shared.MysqlDriver, sqlmanager_shared.PostgresDriver, sqlmanager_shared.MssqlDriver} {
		t.Run(driver+" page cursor", func(t *testing.T) {
			configs, err := runconfigs.BuildRunConfigs(
				map[string][]*sqlmanager_shared.ForeignConstraint{},
				map[string]string{"shop.produit": "categorie = 'a' OR categorie = 'b'"},
				map[string][]string{"shop.produit": {"id"}},
				map[string][]string{"shop.produit": {"id", "categorie"}},
				map[string][][]string{}, map[string][][]string{},
			)
			require.NoError(t, err)
			queries, err := BuildSelectQueryMap(driver, configs, false, 100)
			require.NoError(t, err)
			query := queries["shop.produit.insert"]
			require.NotNil(t, query)
			require.Regexp(t, `\(.*categorie.* (OR|or) .*categorie.*\) AND`, query.PageQuery)
		})

		t.Run(driver+" two subset roots", func(t *testing.T) {
			fk := func(column, parent string) *sqlmanager_shared.ForeignConstraint {
				return &sqlmanager_shared.ForeignConstraint{
					Columns: []string{column}, NotNullable: []bool{true},
					ForeignKey: &sqlmanager_shared.ForeignKey{Table: parent, Columns: []string{"id"}},
				}
			}
			configs, err := runconfigs.BuildRunConfigs(
				map[string][]*sqlmanager_shared.ForeignConstraint{
					"shop.commande": {fk("station_id", "shop.station"), fk("client_id", "shop.client")},
				},
				map[string]string{"shop.station": "id = 1 OR id = 3", "shop.client": "actif = 1"},
				map[string][]string{"shop.station": {"id"}, "shop.client": {"id"}, "shop.commande": {"id"}},
				map[string][]string{
					"shop.station": {"id"}, "shop.client": {"id", "actif"},
					"shop.commande": {"id", "station_id", "client_id"},
				},
				map[string][][]string{}, map[string][][]string{},
			)
			require.NoError(t, err)
			queries, err := BuildSelectQueryMap(driver, configs, true, 100)
			require.NoError(t, err)
			query := queries["shop.commande.insert"]
			require.NotNil(t, query)
			require.Regexp(t, `\([^()]*id.* (OR|or) [^()]*id[^()]*\)`, query.Query)
		})
	}
}

// A table without order columns is read in a single pass: no LIMIT, no page query.
func Test_BuildQuery_TableWithoutKeyIsNotPaged(t *testing.T) {
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{},
		map[string]string{},
		map[string][]string{},
		map[string][]string{"shop.journal": {"niveau", "message"}},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	queries, err := BuildSelectQueryMap(sqlmanager_shared.MysqlDriver, configs, false, 100)
	require.NoError(t, err)
	query := queries["shop.journal.insert"]
	require.NotNil(t, query)
	require.NotContains(t, query.Query, "LIMIT")
	require.NotContains(t, query.Query, "ORDER BY")
	require.Empty(t, query.PageQuery)
}

func Test_qualifyMysqlWhereColumnNames(t *testing.T) {
	for clause, want := range map[string]string{
		"ferme_le IS NULL":                                     "t_1.ferme_le is null",
		"id = 1 OR 3 = id":                                     "t_1.id = 1 or 3 = t_1.id",
		"cree_le BETWEEN '2024-01-01' AND now()":               "t_1.cree_le between '2024-01-01' and now()",
		"DATE(cree_le) = '2024-01-01'":                         "date(t_1.cree_le) = '2024-01-01'",
		"station.id IN (1, 2)":                                 "t_1.id in (1, 2)",
		"id IN (SELECT station_id FROM autre WHERE actif = 1)": "t_1.id in (select station_id from autre where actif = 1)",
	} {
		got, err := qualifyMysqlWhereColumnNames("SELECT * FROM station WHERE "+clause, "t_1", filteredTable{schema: "shop", name: "station"})
		require.NoError(t, err, clause)
		require.Equal(t, "select * from station where "+want, got, clause)
	}
}

// A subquery correlated with the filtered table by its name follows it under its alias;
// its own tables, and a subquery that names a table the same way, keep their names.
func Test_qualifyMysqlWhereColumnNames_outerReferences(t *testing.T) {
	filtered := filteredTable{schema: "shop", name: "litige"}
	for clause, want := range map[string]string{
		"EXISTS (SELECT 1 FROM shop.commande AS o WHERE o.id = litige.order_id)":         "exists (select 1 from shop.commande as o where o.id = t_1.order_id)",
		"EXISTS (SELECT 1 FROM commande AS o WHERE o.id = shop.litige.order_id)":         "exists (select 1 from commande as o where o.id = t_1.order_id)",
		"EXISTS (SELECT 1 FROM commande o WHERE o.id = LITIGE.order_id)":                 "exists (select 1 from commande as o where o.id = t_1.order_id)",
		"EXISTS (SELECT 1 FROM a WHERE EXISTS (SELECT 1 FROM b WHERE b.x = litige.y))":   "exists (select 1 from a where exists (select 1 from b where b.x = t_1.y))",
		"EXISTS (SELECT 1 FROM litige WHERE litige.id = 3)":                              "exists (select 1 from litige where litige.id = 3)",
		"EXISTS (SELECT 1 FROM autre AS litige WHERE litige.id = 3)":                     "exists (select 1 from autre as litige where litige.id = 3)",
		"EXISTS (SELECT 1 FROM commande AS o WHERE o.id = autre.litige_id)":              "exists (select 1 from commande as o where o.id = autre.litige_id)",
		"EXISTS (SELECT 1 FROM commande AS o WHERE o.id = other_schema.litige.order_id)": "exists (select 1 from commande as o where o.id = other_schema.litige.order_id)",
	} {
		got, err := qualifyMysqlWhereColumnNames("SELECT * FROM litige WHERE "+clause, "t_1", filtered)
		require.NoError(t, err, clause)
		require.Equal(t, "select * from litige where "+want, got, clause)
	}
}

func Test_qualifyPostgresWhereColumnNames_outerReferences(t *testing.T) {
	filtered := filteredTable{schema: "shop", name: "litige"}
	for clause, want := range map[string]string{
		"EXISTS (SELECT 1 FROM shop.commande o WHERE o.id = litige.order_id)":          "EXISTS (SELECT 1 FROM shop.commande o WHERE o.id = t_1.order_id)",
		"EXISTS (SELECT 1 FROM commande o WHERE o.id = shop.litige.order_id)":          "EXISTS (SELECT 1 FROM commande o WHERE o.id = t_1.order_id)",
		"EXISTS (SELECT 1 FROM a WHERE EXISTS (SELECT 1 FROM b WHERE b.x = litige.y))": "EXISTS (SELECT 1 FROM a WHERE EXISTS (SELECT 1 FROM b WHERE b.x = t_1.y))",
		"EXISTS (SELECT 1 FROM litige WHERE litige.id = 3)":                            "EXISTS (SELECT 1 FROM litige WHERE litige.id = 3)",
		"EXISTS (SELECT 1 FROM autre litige WHERE litige.id = 3)":                      "EXISTS (SELECT 1 FROM autre litige WHERE litige.id = 3)",
		"order_id IN (SELECT id FROM commande WHERE commande.client = litige.client)":  "t_1.order_id IN (SELECT id FROM commande WHERE commande.client = t_1.client)",
	} {
		got, err := qualifyPostgresWhereColumnNames("SELECT 1 FROM t WHERE "+clause, "t_1", filtered)
		require.NoError(t, err, clause)
		require.Contains(t, got, want, clause)
	}
}

// The filter of a parent, correlated with it by name, still reads in the query of its
// child, which joins the parent under another name.
func Test_BuildQuery_ParentFilterCorrelatedByName(t *testing.T) {
	fk := &sqlmanager_shared.ForeignConstraint{
		Columns: []string{"litige_id"}, NotNullable: []bool{true},
		ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.litige", Columns: []string{"id"}},
	}
	where := "EXISTS (SELECT 1 FROM shop.commande AS o WHERE o.id = litige.order_id AND o.recent = 1)"
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{"shop.relance": {fk}},
		map[string]string{"shop.litige": where},
		map[string][]string{"shop.litige": {"id"}, "shop.relance": {"id"}},
		map[string][]string{"shop.litige": {"id", "order_id"}, "shop.relance": {"id", "litige_id"}},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	queries, err := BuildSelectQueryMap(sqlmanager_shared.MysqlDriver, configs, true, 100)
	require.NoError(t, err)

	litige := queries["shop.litige.insert"].Query
	require.Contains(t, litige, "o.id = litige.order_id", litige)

	relance := queries["shop.relance.insert"].Query
	m := regexp.MustCompile("INNER JOIN `shop`.`litige` AS `([^`]+)`").FindStringSubmatch(relance)
	require.NotNil(t, m, relance)
	require.NotEqual(t, "litige", m[1])
	require.Contains(t, relance, "o.id = "+m[1]+".order_id", relance)
	require.NotContains(t, relance, "litige.order_id", relance)
}

// A nullable foreign key on the subset path keeps the rows holding NULL, and so do the
// joins after it; a mandatory one keeps its INNER JOIN and bare clause.
func Test_BuildQuery_NullableForeignKeyOnSubsetPath(t *testing.T) {
	fk := func(column, parent string, notNull bool) *sqlmanager_shared.ForeignConstraint {
		return &sqlmanager_shared.ForeignConstraint{
			Columns: []string{column}, NotNullable: []bool{notNull},
			ForeignKey: &sqlmanager_shared.ForeignKey{Table: parent, Columns: []string{"id"}},
		}
	}
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{
			"shop.fournisseur": {fk("station_id", "shop.station", true)},
			"shop.commande":    {fk("fournisseur_id", "shop.fournisseur", false)},
		},
		map[string]string{"shop.station": "id = 1"},
		map[string][]string{"shop.station": {"id"}, "shop.fournisseur": {"id"}, "shop.commande": {"id"}},
		map[string][]string{
			"shop.station": {"id"}, "shop.fournisseur": {"id", "station_id"},
			"shop.commande": {"id", "fournisseur_id"},
		},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	queries, err := BuildSelectQueryMap(sqlmanager_shared.MysqlDriver, configs, true, 100)
	require.NoError(t, err)

	commande := queries["shop.commande.insert"].Query
	_, joins, found := strings.Cut(commande, "FROM `shop`.`commande` AS `commande`")
	require.True(t, found, commande)
	require.Equal(t, 2, strings.Count(joins, "LEFT JOIN"), commande)
	require.NotContains(t, joins, "INNER JOIN", commande)
	require.Regexp(t, "`commande`.`fournisseur_id` IS NULL\\) OR \\(.*id = 1\\)", joins)
	// The value read is NULL when the referenced row is not selected by its own sync.
	require.Regexp(t, "CASE +WHEN \\(\\(`commande`.`fournisseur_id` IS NULL\\) OR EXISTS \\(SELECT 1 FROM `shop`.`fournisseur` AS `t_[0-9a-f]+` INNER JOIN", commande)
	require.Contains(t, commande, "THEN `commande`.`fournisseur_id` END AS `fournisseur_id`")

	fournisseur := queries["shop.fournisseur.insert"].Query
	require.Equal(t, 1, strings.Count(fournisseur, "INNER JOIN"), fournisseur)
	require.NotContains(t, fournisseur, "LEFT JOIN", fournisseur)
	require.NotContains(t, fournisseur, "IS NULL", fournisseur)
}

// A nullable self-reference is never on a subset path. The subquery selects the table
// under an alias of its own, so its columns are not confused with the outer row's.
func Test_BuildQuery_NullableSelfReferenceOutOfSubset(t *testing.T) {
	configs, err := runconfigs.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{
			"shop.commande": {
				{
					Columns: []string{"groupe_id"}, NotNullable: []bool{false},
					ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.commande", Columns: []string{"id"}},
				},
				{
					Columns: []string{"station_id"}, NotNullable: []bool{true},
					ForeignKey: &sqlmanager_shared.ForeignKey{Table: "shop.station", Columns: []string{"id"}},
				},
			},
		},
		map[string]string{"shop.station": "id = 1"},
		map[string][]string{"shop.station": {"id"}, "shop.commande": {"id"}},
		map[string][]string{"shop.station": {"id"}, "shop.commande": {"id", "station_id", "groupe_id"}},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	queries, err := BuildSelectQueryMap(sqlmanager_shared.MysqlDriver, configs, true, 100)
	require.NoError(t, err)

	for _, id := range []string{"shop.commande.insert", "shop.commande.update.1"} {
		query := queries[id]
		require.NotNil(t, query, id)
		require.Regexp(t,
			"EXISTS \\(SELECT 1 FROM `shop`.`commande` AS `(t_[0-9a-f]+)` INNER JOIN `shop`.`station` .* AND \\(`t_[0-9a-f]+`.`id` = `commande`.`groupe_id`\\)",
			query.Query, id)
		require.Contains(t, query.PageQuery, "EXISTS", id)
	}

	// The station table is not reduced through a foreign key of its own: nothing to project.
	require.NotContains(t, queries["shop.station.insert"].Query, "EXISTS")
}

// Test_qualifyPostgresWhereColumnNames: every column of the clause must carry its table,
// whatever the expression around it. Naming only comparisons and boolean operators left
// IS NULL, BETWEEN, IN, a function call and a CASE with bare columns, which are ambiguous
// once the table is joined to a child holding the same ones — the run then fails on a
// clause it accepted before the subset.
func Test_qualifyPostgresWhereColumnNames(t *testing.T) {
	for name, tc := range map[string]struct{ where, want string }{
		"comparison":    {"ferme_le = '2024-01-01'", `"STATION".ferme_le = '2024-01-01'`},
		"is null":       {"ferme_le IS NULL", `"STATION".ferme_le IS NULL`},
		"between":       {"id BETWEEN 1 AND 9", `"STATION".id BETWEEN 1 AND 9`},
		"in list":       {"id IN (1, 2)", `"STATION".id IN (1, 2)`},
		"function call": {"date(ferme_le) = '2024-01-01'", `date("STATION".ferme_le) = '2024-01-01'`},
		"not":           {"NOT actif", `NOT "STATION".actif`},
		"case":          {"CASE WHEN actif THEN id ELSE 0 END > 1", `CASE WHEN "STATION".actif THEN "STATION".id ELSE 0 END > 1`},
		"other table":   {"autre.actif", `"STATION".actif`},
		// Already named by its table, which is what tells it apart: left as it is.
		"already qualified": {`"STATION".ferme_le IS NULL`, `"STATION".ferme_le IS NULL`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := qualifyPostgresWhereColumnNames(
				fmt.Sprintf("SELECT 1 FROM t WHERE %s", tc.where), "STATION", filteredTable{schema: "public", name: "station"})
			require.NoError(t, err)
			require.Contains(t, got, tc.want)
		})
	}
}

// A subquery names its own tables: only the expression tested against it belongs to the
// filtered table.
func Test_qualifyPostgresWhereColumnNames_leavesSubqueriesAlone(t *testing.T) {
	got, err := qualifyPostgresWhereColumnNames(
		"SELECT 1 FROM t WHERE id IN (SELECT station_id FROM autre WHERE actif)", "STATION",
		filteredTable{schema: "public", name: "station"})
	require.NoError(t, err)
	require.Contains(t, got, `"STATION".id IN (SELECT station_id FROM autre WHERE actif)`)
}
