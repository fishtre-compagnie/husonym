package v1alpha1_transformersservice

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The entities are listed for the language a transformer that sets none analyzes in.
func Test_entityLanguage(t *testing.T) {
	french, empty := "fr", ""

	require.Equal(t, "fr", (&Service{cfg: &Config{PresidioDefaultLanguage: &french}}).entityLanguage())
	require.Equal(t, "en", (&Service{cfg: &Config{PresidioDefaultLanguage: &empty}}).entityLanguage())
	require.Equal(t, "en", (&Service{cfg: &Config{}}).entityLanguage())
}
