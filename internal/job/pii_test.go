package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func Test_LooksSensitive(t *testing.T) {
	t.Run("recognises personal data from the name and type", func(t *testing.T) {
		category, sensitive := LooksSensitive("email", "character varying(255)")
		require.True(t, sensitive)
		require.Equal(t, "email", category)

		// The column behind a real incident: a login is personal data, and this is the kind of
		// column that turns up in a schema without anyone mapping it.
		category, sensitive = LooksSensitive("LIB_LOGIN", "varchar(50)")
		require.True(t, sensitive)
		require.Equal(t, "username", category)
	})

	t.Run("says nothing about a column it does not recognise", func(t *testing.T) {
		// And "nothing" is the point: `champ_libre` may hold an address. The caller may rank on
		// this verdict, never clear a column with it.
		category, sensitive := LooksSensitive("champ_libre", "text")
		require.False(t, sensitive)
		require.Empty(t, category)

		category, sensitive = LooksSensitive("total", "numeric")
		require.False(t, sensitive)
		require.Empty(t, category)
	})
}

func Test_SuggestedTransformer(t *testing.T) {
	t.Run("a phone column stored as text keeps its format", func(t *testing.T) {
		source, category, ok := SuggestedTransformer("telephone", "character varying")
		require.True(t, ok)
		require.Equal(t, "phone_number", category)
		require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER, source)
	})

	t.Run("nothing for a column it does not recognise", func(t *testing.T) {
		_, _, ok := SuggestedTransformer("champ_libre", "text")
		require.False(t, ok)
	})

	t.Run("nothing when the column's type does not take the suggestion", func(t *testing.T) {
		// The detection reads the name: `state` names a generator of "CA", which a smallint
		// column refuses on every row. A run that writes the suggestion into the job would fail
		// that run and every one after it, until somebody edits the mapping by hand.
		for _, column := range []struct{ name, dataType string }{
			{"state", "smallint"},
			{"gender", "boolean"},
			{"city", "int"},
			{"country", "smallint"},
		} {
			_, _, ok := SuggestedTransformer(column.name, column.dataType)
			require.False(t, ok, "%s %s", column.name, column.dataType)
		}
	})

	t.Run("the same column as a string is suggested", func(t *testing.T) {
		source, _, ok := SuggestedTransformer("state", "character varying(2)")
		require.True(t, ok)
		require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE, source)
	})

	t.Run("a date column keeps its suggestion, type spelled out in full", func(t *testing.T) {
		// PostgreSQL reports "timestamp without time zone": a table that only knew "timestamp"
		// would drop the suggestion of every timestamp column in the product.
		_, _, ok := SuggestedTransformer("date_naissance", "timestamp without time zone")
		require.True(t, ok)
	})
}
