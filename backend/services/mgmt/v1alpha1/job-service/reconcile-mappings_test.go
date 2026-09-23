package v1alpha1_jobservice

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/stretchr/testify/require"
)

func passthroughMapping(table, column string) *mgmtv1alpha1.JobMapping {
	return &mgmtv1alpha1.JobMapping{
		Schema: "public", Table: table, Column: column,
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
		}},
	}
}

func emailMapping(table, column string) *mgmtv1alpha1.JobMapping {
	return &mgmtv1alpha1.JobMapping{
		Schema: "public", Table: table, Column: column,
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{}},
		}},
	}
}

func storedMappings(t *testing.T, dtos ...*mgmtv1alpha1.JobMapping) []*pg_models.JobMapping {
	t.Helper()
	out := make([]*pg_models.JobMapping, 0, len(dtos))
	for _, dto := range dtos {
		m := &pg_models.JobMapping{}
		require.NoError(t, m.FromDto(dto))
		out = append(out, m)
	}
	return out
}

func columnsOf(mappings []*pg_models.JobMapping) []string {
	out := make([]string, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, m.Table+"."+m.Column)
	}
	return out
}

func Test_reconcileMappings(t *testing.T) {
	t.Run("adds the new columns and removes the gone ones, keeping the others in order", func(t *testing.T) {
		stored := storedMappings(t,
			passthroughMapping("users", "id"),
			emailMapping("users", "email"),
			passthroughMapping("users", "commentaire"),
		)
		result, err := reconcileMappings(stored,
			[]*mgmtv1alpha1.JobMapping{passthroughMapping("users", "telephone")},
			[]*mgmtv1alpha1.JobColumn{{Schema: "public", Table: "users", Column: "commentaire"}},
		)
		require.NoError(t, err)
		require.Equal(t, []string{"users.id", "users.email", "users.telephone"}, columnsOf(result.mappings))
		require.Len(t, result.added, 1)
		require.Len(t, result.removed, 1)
		require.Equal(t, "commentaire", result.removed[0].GetColumn())
		require.NotNil(t, result.removed[0].GetTransformer().GetConfig().GetPassthroughConfig(),
			"a removed mapping comes back with the transformer it had")
	})

	t.Run("a column mapped in the meantime keeps its mapping", func(t *testing.T) {
		stored := storedMappings(t, emailMapping("users", "email"))
		result, err := reconcileMappings(stored,
			[]*mgmtv1alpha1.JobMapping{passthroughMapping("users", "email")},
			nil,
		)
		require.NoError(t, err)
		require.Empty(t, result.added)
		require.NotNil(t, result.mappings[0].JobMappingTransformer)
		dto, err := result.mappings[0].ToDto()
		require.NoError(t, err)
		require.NotNil(t, dto.GetTransformer().GetConfig().GetGenerateEmailConfig())
	})

	t.Run("applied twice, the second call changes nothing", func(t *testing.T) {
		added := []*mgmtv1alpha1.JobMapping{passthroughMapping("users", "telephone")}
		removed := []*mgmtv1alpha1.JobColumn{{Schema: "public", Table: "users", Column: "commentaire"}}

		first, err := reconcileMappings(storedMappings(t,
			passthroughMapping("users", "id"),
			passthroughMapping("users", "commentaire"),
		), added, removed)
		require.NoError(t, err)

		second, err := reconcileMappings(first.mappings, added, removed)
		require.NoError(t, err)
		require.Empty(t, second.added)
		require.Empty(t, second.removed)
		require.Equal(t, columnsOf(first.mappings), columnsOf(second.mappings))
	})

	t.Run("a column removed and mapped again in the same call stays removed", func(t *testing.T) {
		result, err := reconcileMappings(
			storedMappings(t, passthroughMapping("users", "commentaire")),
			[]*mgmtv1alpha1.JobMapping{passthroughMapping("users", "commentaire")},
			[]*mgmtv1alpha1.JobColumn{{Schema: "public", Table: "users", Column: "commentaire"}},
		)
		require.NoError(t, err)
		require.Empty(t, result.mappings)
		require.Empty(t, result.added)
	})

	t.Run("an added mapping without a transformer is refused", func(t *testing.T) {
		_, err := reconcileMappings(nil,
			[]*mgmtv1alpha1.JobMapping{{Schema: "public", Table: "users", Column: "telephone"}},
			nil,
		)
		require.Error(t, err)
	})
}

func sourceColumn(table, column, dataType string) *mgmtv1alpha1.JobSourceColumn {
	return &mgmtv1alpha1.JobSourceColumn{
		Column:   &mgmtv1alpha1.JobColumn{Schema: "public", Table: table, Column: column},
		DataType: dataType,
	}
}

func Test_journalEntries(t *testing.T) {
	t.Run("records what was added, removed, and what changed type", func(t *testing.T) {
		result, err := reconcileMappings(
			storedMappings(t,
				passthroughMapping("users", "id"),
				emailMapping("users", "email"),
				passthroughMapping("users", "commentaire"),
			),
			[]*mgmtv1alpha1.JobMapping{emailMapping("users", "courriel")},
			[]*mgmtv1alpha1.JobColumn{{Schema: "public", Table: "users", Column: "commentaire"}},
		)
		require.NoError(t, err)

		previous := map[columnRef]string{
			{"public", "users", "id"}:          "integer",
			{"public", "users", "email"}:       "text",
			{"public", "users", "commentaire"}: "text",
		}
		entries, err := journalEntries(result, previous, []*mgmtv1alpha1.JobSourceColumn{
			sourceColumn("users", "id", "integer"),
			sourceColumn("users", "email", "character varying(255)"),
			sourceColumn("users", "courriel", "text"),
		})
		require.NoError(t, err)

		byColumn := map[string]journalEntry{}
		for _, e := range entries {
			byColumn[e.column.column] = e
		}
		require.Len(t, entries, 3)

		require.Equal(t, changeAdded, byColumn["courriel"].kind)
		require.Equal(t, "text", byColumn["courriel"].dataType)
		require.Equal(t, "email", byColumn["courriel"].piiCategory)
		require.NotNil(t, byColumn["courriel"].transformer)

		require.Equal(t, changeRemoved, byColumn["commentaire"].kind)
		require.Equal(t, "text", byColumn["commentaire"].dataType, "the type it had, the source no longer has it")

		require.Equal(t, changeTypeChanged, byColumn["email"].kind)
		require.Equal(t, "text", byColumn["email"].previousDataType)
		require.Equal(t, "character varying(255)", byColumn["email"].dataType)
	})

	t.Run("a first run has nothing to compare types with", func(t *testing.T) {
		result, err := reconcileMappings(storedMappings(t, emailMapping("users", "email")), nil, nil)
		require.NoError(t, err)
		entries, err := journalEntries(result, map[columnRef]string{}, []*mgmtv1alpha1.JobSourceColumn{
			sourceColumn("users", "email", "text"),
		})
		require.NoError(t, err)
		require.Empty(t, entries)
	})

	t.Run("a column mapped by hand in the meantime is not the run's change", func(t *testing.T) {
		result, err := reconcileMappings(
			storedMappings(t, emailMapping("users", "courriel")),
			[]*mgmtv1alpha1.JobMapping{passthroughMapping("users", "courriel")},
			nil,
		)
		require.NoError(t, err)
		entries, err := journalEntries(result, map[columnRef]string{}, []*mgmtv1alpha1.JobSourceColumn{
			sourceColumn("users", "courriel", "text"),
		})
		require.NoError(t, err)
		require.Empty(t, entries)
	})
}
