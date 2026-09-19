package runner

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestTryJavascriptRules(t *testing.T) {
	rules := []JavascriptRule{
		{Column: "nom", Config: transformJS(`neosync.pseudo = pseudo.lastName(value); return neosync.pseudo;`)},
		{Column: "login", Config: transformJS(`return (neosync.pseudo || "vide").toLowerCase() + "-" + input.id;`)},
	}
	rows := []map[string]any{
		{"id": int64(1), "nom": "Durand", "login": "jdurand"},
		{"id": int64(2), "nom": "Durand", "login": "autre"},
	}
	out, failure, err := TryJavascriptRules(context.Background(), rules, rows)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.Len(t, out, 2)
	require.Equal(t, int64(1), out[0]["id"], "a column no rule writes is kept")
	require.NotEqual(t, "Durand", out[0]["nom"])
	require.Equal(t, out[0]["nom"], out[1]["nom"], "the same value gives the same pseudonym")
	require.Contains(t, out[0]["login"], "-1", "the columns of a row share their state")
}

func TestTryJavascriptRules_Failure(t *testing.T) {
	rules := []JavascriptRule{
		{Column: "nom", Config: transformJS(`return value;`)},
		{Column: "ville", Config: transformJS(`if (input.id === 2) { throw new Error("ville refusée"); } return value;`)},
	}
	rows := []map[string]any{{"id": int64(1), "ville": "Lyon"}, {"id": int64(2), "ville": "Paris"}}
	out, failure, err := TryJavascriptRules(context.Background(), rules, rows)
	require.NoError(t, err)
	require.Nil(t, out)
	require.Equal(t, 1, failure.Row)
	require.Equal(t, "ville", failure.Column)
	require.Contains(t, failure.Message, "ville refusée")
	require.NotContains(t, failure.Message, "engine:", "the script's error, not the engine's layers")
}

func TestTryJavascriptRules_OnlyJavascript(t *testing.T) {
	rules := []JavascriptRule{{Column: "nom", Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{GenerateLastNameConfig: &mgmtv1alpha1.GenerateLastName{}},
	}}}
	_, _, err := TryJavascriptRules(context.Background(), rules, []map[string]any{{"nom": "Durand"}})
	require.ErrorIs(t, err, ErrNotARule)
}
