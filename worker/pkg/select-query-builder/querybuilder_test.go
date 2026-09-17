package selectquerybuilder

import (
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
		got, err := qualifyMysqlWhereColumnNames("SELECT * FROM station WHERE "+clause, nil, "t_1")
		require.NoError(t, err, clause)
		require.Equal(t, "select * from station where "+want, got, clause)
	}
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
