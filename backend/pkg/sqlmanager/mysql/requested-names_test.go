package sqlmanager_mysql

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// A server folding table names answers in lower case; the caller gets its own spelling back.
func Test_requestedNames(t *testing.T) {
	names := newRequestedNames([]*sqlmanager_shared.SchemaTable{{Schema: "Shop", Table: "COMMANDE"}})

	schema, table := names.table("shop", "commande")
	require.Equal(t, "Shop", schema)
	require.Equal(t, "COMMANDE", table)

	schema, table = names.table("shop", "client")
	require.Equal(t, "Shop", schema, "a table not asked about takes the spelling of its database")
	require.Equal(t, "client", table)

	schema, table = names.table("Shop", "COMMANDE")
	require.Equal(t, "Shop", schema)
	require.Equal(t, "COMMANDE", table, "an exact answer is left as is")

	require.Equal(t, "autre", names.schema("autre"))
	require.Equal(t, "Shop", newRequestedSchemas([]string{"Shop"}).schema("shop"))
}
