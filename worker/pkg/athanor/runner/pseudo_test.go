package runner

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pseudo_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/pseudo"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	"github.com/stretchr/testify/require"
)

// Every function offered to scripts returns what a native transformer returns.
func TestPseudoConfigs_CoverEveryKind(t *testing.T) {
	d := consistency.New([]byte("k"), "run:1")
	require.Len(t, pseudoConfigs, len(pseudo_functions.Kinds))
	for _, kind := range pseudo_functions.Kinds {
		cfg, ok := pseudoConfigs[kind]
		require.True(t, ok, kind)
		_, ok = deterministicValueTransformer(d, cfg)
		require.True(t, ok, "%s has no deterministic transformer", kind)
	}
}

// pseudo.<kind>(value) in a script returns exactly what the native transformer returns
// for the same value, in the same scope.
func TestSpecForTable_PseudoMatchesNative(t *testing.T) {
	const big = int64(1)<<60 + 1 // beyond what a float64 holds exactly
	samples := []struct {
		kind   string
		values []any
	}{
		{"lastName", []any{"Durand", "durand", big, nil}},
		{"firstName", []any{"Marie", int64(7)}},
		{"email", []any{"Bob@exemple.fr", "bob@exemple.fr"}},
		{"phone", []any{"+33612345678"}},
		{"city", []any{"Lyon"}},
	}
	d := consistency.New([]byte("clé de test"), "job:42")
	for _, sample := range samples {
		t.Run(sample.kind, func(t *testing.T) {
			mappings := []*mgmtv1alpha1.JobMapping{
				{Schema: "s", Table: "t", Column: "natif", Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: pseudoConfigs[sample.kind]}},
				{Schema: "s", Table: "t", Column: "script", Transformer: &mgmtv1alpha1.JobMappingTransformer{
					Config: transformJS("return pseudo." + sample.kind + "(value);"),
				}},
			}
			cols, spec, err := SpecForTable(context.Background(), mappings, "s", "t", d, &TransformEnv{})
			require.NoError(t, err)
			plan, err := engine.Compile(cols, spec)
			require.NoError(t, err)

			b := engine.NewBatch(cols, len(sample.values))
			for i, v := range sample.values {
				b.Cols["natif"][i], b.Cols["script"][i] = v, v
			}
			require.NoError(t, plan.Execute(transform.Background(), b))
			for i, v := range sample.values {
				require.Equal(t, b.Cols["natif"][i], b.Cols["script"][i], "value %v", v)
			}
		})
	}
}

// Without a consistency scope the functions fail, naming the column.
func TestSpecForTable_PseudoWithoutDeriver(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "s", Table: "t", Column: "nom", Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: transformJS("return pseudo.lastName(value);")}},
	}
	cols, spec, err := SpecForTable(context.Background(), mappings, "s", "t", nil, &TransformEnv{})
	require.NoError(t, err)
	plan, err := engine.Compile(cols, spec)
	require.NoError(t, err)
	b := engine.NewBatch(cols, 1)
	b.Cols["nom"][0] = "Durand"
	require.ErrorContains(t, plan.Execute(transform.Background(), b), `colonne "nom"`)
}
