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

func Test_AcceptedPassthrough_StillHoldsFor(t *testing.T) {
	t.Run("holds for the column it was made about", func(t *testing.T) {
		accepted := AcceptedPassthrough{DataType: "text"}
		require.True(t, accepted.StillHoldsFor("commentaire", "text"))
	})

	t.Run("does not survive the column changing type", func(t *testing.T) {
		// The decision was about a free-text note. A column that is now something else is not
		// the column anybody looked at, and carrying the acceptance over is how a job keeps a
		// clean bill of health while it starts shipping something new in clear.
		accepted := AcceptedPassthrough{DataType: "text"}
		require.False(t, accepted.StillHoldsFor("commentaire", "character varying(255)"))
	})

	t.Run("does not survive the detector learning to read the column", func(t *testing.T) {
		// Category is stored, not recomputed and compared to itself: it records what the
		// detector saw at the time. When a release teaches it to recognise a column it used to
		// pass over, every acceptance granted while it was blind comes back up for confirmation.
		blindWhenAccepted := AcceptedPassthrough{DataType: "varchar(255)", PiiCategory: ""}
		require.False(t, blindWhenAccepted.StillHoldsFor("email", "varchar(255)"))

		seenWhenAccepted := AcceptedPassthrough{DataType: "varchar(255)", PiiCategory: "email"}
		require.True(t, seenWhenAccepted.StillHoldsFor("email", "varchar(255)"))
	})
}
