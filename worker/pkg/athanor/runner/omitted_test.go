package runner

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// udtResolver résout un transformer défini par l'utilisateur vers sa configuration.
type udtResolver map[string]*mgmtv1alpha1.TransformerConfig

func (r udtResolver) GetUserDefinedTransformer(_ context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
	return r[id], nil
}

func generateDefault() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
		GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
	}}
}

// Les colonnes omises sont celles en GenerateDefault, y compris derrière un transformer
// défini par l'utilisateur : ce que SpecForTable laisse hors de l'INSERT.
func Test_OmittedColumns(t *testing.T) {
	passthrough := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
		PassthroughConfig: &mgmtv1alpha1.Passthrough{},
	}}
	udt := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "udt-default"},
	}}
	mapping := func(table, column string, cfg *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: "public", Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: cfg},
		}
	}
	mappings := []*mgmtv1alpha1.JobMapping{
		mapping("users", "id", passthrough),
		mapping("users", "created_at", generateDefault()),
		mapping("users", "updated_at", udt),
		mapping("orders", "id", passthrough),
	}

	omitted, err := OmittedColumns(context.Background(), mappings, udtResolver{"udt-default": generateDefault()})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"public.users": {"created_at", "updated_at"}}, omitted)

	cols, _, err := SpecForTable(context.Background(), mappings, "public", "users", nil,
		&TransformEnv{Resolver: udtResolver{"udt-default": generateDefault()}})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, cols, "l'INSERT laisse de côté ce que nomme OmittedColumns")
}
