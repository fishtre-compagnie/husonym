package preflight_activity

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	"github.com/stretchr/testify/require"
)

// A destination is checked on the columns the run writes into it. Athanor leaves a column
// mapped to GenerateDefault out of its INSERT: a destination without it is fine. Benthos
// writes DEFAULT into it: the destination must have it.
func Test_writtenTables(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "users", Column: "id", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
				PassthroughConfig: &mgmtv1alpha1.Passthrough{},
			}},
		}},
		{Schema: "public", Table: "users", Column: "created_at", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
				GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
			}},
		}},
	}}
	tables := []*connectionchecks.Table{{Schema: "public", Table: "users", Columns: []string{"id", "created_at"}}}
	a := &Activity{}

	athanor, err := a.writtenTables(context.Background(), job, tables, true)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, athanor[0].Columns)
	require.Equal(t, []string{"id", "created_at"}, tables[0].Columns, "the source is checked on the tables as given")

	benthos, err := a.writtenTables(context.Background(), job, tables, false)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "created_at"}, benthos[0].Columns)
}
