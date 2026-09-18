package v1alpha1_transformersservice

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestValidateUserJavascriptCode(t *testing.T) {
	s := &Service{}
	validate := func(code string) *mgmtv1alpha1.ValidateUserJavascriptCodeResponse {
		resp, err := s.ValidateUserJavascriptCode(context.Background(),
			connect.NewRequest(&mgmtv1alpha1.ValidateUserJavascriptCodeRequest{Code: code}))
		require.NoError(t, err)
		return resp.Msg
	}

	require.False(t, validate(`return (;`).GetValid())
	// Compiled as a run compiles it: not in strict mode.
	require.True(t, validate(`with (input) { return nom; }`).GetValid())

	resp := validate(`picked = value; neosync.nom = value; var local = 1; return local;`)
	require.True(t, resp.GetValid())
	require.ElementsMatch(t, []string{"picked", "neosync.nom"}, resp.GetGlobalWrites())
}

func TestParseRow(t *testing.T) {
	row, err := parseRow(`{"id": 1152921504606846977, "prix": 12.5, "nom": "Durand", "tags": [1, {"n": 2}], "vide": null}`)
	require.NoError(t, err)
	require.Equal(t, int64(1152921504606846977), row["id"], "an integer stays exact")
	require.InDelta(t, 12.5, row["prix"], 0)
	require.Equal(t, "Durand", row["nom"])
	require.Equal(t, []any{int64(1), map[string]any{"n": int64(2)}}, row["tags"])
	require.Nil(t, row["vide"])

	for _, raw := range []string{`[1]`, `null`, `"texte"`, `{`} {
		_, err := parseRow(raw)
		require.Error(t, err, raw)
	}
}
