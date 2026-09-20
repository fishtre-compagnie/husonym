package v1alpha1_transformersservice

import (
	"context"
	"math"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
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

func trialRule(code string) []runner.JavascriptRule {
	return []runner.JavascriptRule{{Column: "nom", Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
			TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
		},
	}}}
}

// A rule allocating without end is stopped, instead of taking the API down.
func TestRunTrial_MemoryLimit(t *testing.T) {
	_, failure, err := runTrial(context.Background(),
		trialRule(`const a = []; while (true) { a.push("x".repeat(100000) + a.length); }`),
		[]map[string]any{{"nom": "Durand"}}, 32<<20)
	require.NoError(t, err)
	require.NotNil(t, failure)
	require.Equal(t, "nom", failure.Column)
	require.Contains(t, failure.Message, "MiB during the trial")
}

func TestDisplayable(t *testing.T) {
	row := displayable(map[string]any{
		"nan": math.NaN(), "inf": math.Inf(1), "moins": math.Inf(-1), "n": 1.5,
		"liste": []any{math.NaN()},
	}).(map[string]any)
	require.Equal(t, "NaN", row["nan"])
	require.Equal(t, "Infinity", row["inf"])
	require.Equal(t, "-Infinity", row["moins"])
	require.InDelta(t, 1.5, row["n"], 0)
	require.Equal(t, []any{"NaN"}, row["liste"])
}
