package transform

import (
	"testing"

	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// Prouve que la couture fonctionne : un transformer Neosync existant, vu à
// travers la NOUVELLE interface ValueTransformer, produit bien un résultat.

func TestWrap_Passthrough_equivalence(t *testing.T) {
	cfg := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
			PassthroughConfig: &mgmtv1alpha1.Passthrough{},
		},
	}
	vt, err := WrapNeosyncConfig(cfg)
	require.NoError(t, err)

	out, err := vt.TransformValue(Background(), "bonjour")
	require.NoError(t, err)
	require.Equal(t, "bonjour", out, "passthrough doit renvoyer l'entrée inchangée")
}

func TestWrap_TransformInt64_realTransformer(t *testing.T) {
	min, max := int64(1), int64(10)
	cfg := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformInt64Config{
			TransformInt64Config: &mgmtv1alpha1.TransformInt64{
				RandomizationRangeMin: &min,
				RandomizationRangeMax: &max,
			},
		},
	}
	vt, err := WrapNeosyncConfig(cfg)
	require.NoError(t, err)

	out, err := vt.TransformValue(Background(), int64(5))
	require.NoError(t, err)

	// transformInt renvoie un *int64 dans la plage [valeur-min, valeur+max].
	got, ok := out.(*int64)
	require.True(t, ok, "TransformInt64 renvoie un *int64, obtenu %T", out)
	require.NotNil(t, got)
}

// Prouve la capacité NOUVELLE : un transformer multi-colonnes (portée Row),
// impossible dans le modèle Benthos actuel.
func TestRow_FirstNameBySex(t *testing.T) {
	tr := FirstNameBySex{SexColumn: "sexe", NameColumn: "prenom"}
	require.Equal(t, []string{"sexe"}, tr.Reads())
	require.Equal(t, []string{"prenom"}, tr.Writes())

	cases := map[string]string{"F": "Léa", "H": "Karim", "": "Alex"}
	for sexe, want := range cases {
		row := MapRow{"sexe": sexe, "prenom": "Jean"}
		require.NoError(t, tr.TransformRow(Background(), row))
		require.Equal(t, want, row["prenom"], "sexe=%q", sexe)
	}
}
