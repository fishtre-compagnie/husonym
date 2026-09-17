package selectquerybuilder

import (
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
