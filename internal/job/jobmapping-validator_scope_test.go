package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func TestValidateJobMappingsExistInSource_DatabaseScope(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "allopneus", Table: "client", Column: "id"},
		{Schema: "alloblog", Table: "wp_users", Column: "id"},
	}
	source := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"allopneus.client":  {"id": {}},
		"alloblog.wp_users": {"id": {}},
	}

	t.Run("a table outside the database of the connection is refused", func(t *testing.T) {
		v := NewJobMappingsValidator(mappings, WithDatabaseScope("allopneus"))
		v.ValidateJobMappingsExistInSource(source)
		errs := v.GetTableErrors()
		require.Len(t, errs, 1)
		require.Len(t, errs["alloblog.wp_users"], 1)
		require.Equal(t, OutOfDatabaseScopeMessage("alloblog.wp_users", "allopneus"), errs["alloblog.wp_users"][0].GetMessage())
		require.Empty(t, v.GetColumnErrors())
	})

	t.Run("without a database every table the source holds passes", func(t *testing.T) {
		v := NewJobMappingsValidator(mappings)
		v.ValidateJobMappingsExistInSource(source)
		require.Empty(t, v.GetTableErrors())
	})
}
