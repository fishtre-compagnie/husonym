package job

import (
	"testing"

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
