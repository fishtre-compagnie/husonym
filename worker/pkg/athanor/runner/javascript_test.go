package runner

import (
	"context"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	"github.com/stretchr/testify/require"
)

type mapResolver map[string]*mgmtv1alpha1.TransformerConfig

func (r mapResolver) GetUserDefinedTransformer(_ context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
	return r[id], nil
}

func transformJS(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func userDefined(id string) *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
			UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: id},
		},
	}}
}

// The JavaScript transformers of a table share one VM, like in Benthos: a column can
// leave state on the neosync global for the following columns, and read the whole row
// through `input`. Run in separate VMs, the reader would return the real name.
func TestSpecForTable_JavascriptSharesStateAcrossColumns(t *testing.T) {
	resolver := mapResolver{
		"adresse": transformJS(`neosync.nomSelectionne = "Durand"; return "1 rue de la Paix";`),
		"nom": transformJS(`
			var nom = (typeof neosync.nomSelectionne === 'undefined') ? "" : String(neosync.nomSelectionne);
			return nom !== "" ? nom : value;`),
		"login": transformJS(`return "demo-" + String(input.role).toLowerCase() + "-" + input.id;`),
	}
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "web", Table: "users", Column: "id", Transformer: passthrough()},
		{Schema: "web", Table: "users", Column: "role", Transformer: passthrough()},
		{Schema: "web", Table: "users", Column: "adresse", Transformer: userDefined("adresse")},
		{Schema: "web", Table: "users", Column: "nom", Transformer: userDefined("nom")},
		{Schema: "web", Table: "users", Column: "login", Transformer: userDefined("login")},
	}

	cols, spec, err := SpecForTable(context.Background(), mappings, "web", "users", nil, &TransformEnv{Resolver: resolver})
	require.NoError(t, err)
	plan, err := engine.Compile(cols, spec)
	require.NoError(t, err)

	b := engine.NewBatch(cols, 1)
	b.Cols["id"][0] = int64(65)
	b.Cols["role"][0] = "Gestionnaire"
	b.Cols["adresse"][0] = "12 rue réelle"
	b.Cols["nom"][0] = "Nom Réel"
	b.Cols["login"][0] = "vrai.login"
	require.NoError(t, plan.Execute(transform.Background(), b))

	require.Equal(t, "1 rue de la Paix", b.Cols["adresse"][0])
	require.Equal(t, "Durand", b.Cols["nom"][0], "le nom réel ne doit jamais ressortir")
	require.Equal(t, "demo-gestionnaire-65", b.Cols["login"][0])
	require.Equal(t, int64(65), b.Cols["id"][0], "une colonne passthrough garde son type Go")
}

func TestSpecForTable_UserDefinedWithoutResolver(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "web", Table: "users", Column: "nom", Transformer: userDefined("nom")},
	}
	_, _, err := SpecForTable(context.Background(), mappings, "web", "users", nil, nil)
	require.Error(t, err)
}

// A failed script names its column, whether it throws or never ends.
func TestSpecForTable_JavascriptErrorNamesTheColumn(t *testing.T) {
	scripts := map[string]string{
		"throws":     `throw new Error("règle refusée");`,
		"never ends": `while (true) {}`,
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			resolver := mapResolver{"ok": transformJS(`return "x";`), "failing": transformJS(script)}
			mappings := []*mgmtv1alpha1.JobMapping{
				{Schema: "web", Table: "users", Column: "nom", Transformer: userDefined("ok")},
				{Schema: "web", Table: "users", Column: "ville", Transformer: userDefined("failing")},
			}
			cols, spec, err := SpecForTable(context.Background(), mappings, "web", "users", nil, &TransformEnv{Resolver: resolver})
			require.NoError(t, err)
			plan, err := engine.Compile(cols, spec)
			require.NoError(t, err)

			b := engine.NewBatch(cols, 1)
			b.Cols["nom"][0], b.Cols["ville"][0] = "Nom Réel", "Ville Réelle"
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err = plan.Execute(transform.Ctx{Context: ctx}, b)
			require.ErrorContains(t, err, `colonne "ville"`)
			require.NotContains(t, err.Error(), "Réel", "no value of the row in the message")
		})
	}
}
