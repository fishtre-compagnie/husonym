package benthosbuilder_builders

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func Test_isSourceMissingColumns(t *testing.T) {
	t.Run("no missing columns", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id":   {},
					"name": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "users",
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "users",
					Column: "name",
				},
			},
		)
		require.False(t, ok)
		require.Empty(t, missing)
	})

	t.Run("missing table", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "accounts", // non-existent table
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "accounts", // non-existent table
					Column: "name",
				},
			},
		)
		require.True(t, ok)
		require.ElementsMatch(t, []string{"public.accounts.id", "public.accounts.name"}, missing)
	})

	t.Run("missing column", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "users",
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "users",
					Column: "email", // non-existent column
				},
			},
		)
		require.True(t, ok)
		require.Equal(t, []string{"public.users.email"}, missing)
	})
}

func Test_removeMappingsNotFoundInSource(t *testing.T) {
	t.Run("removes mappings for non-existent tables", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "accounts", // non-existent table
				Column: "id",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 1)
		require.Equal(t, "public", result[0].Schema)
		require.Equal(t, "users", result[0].Table)
		require.Equal(t, "id", result[0].Column)
		require.Equal(t, []*mgmtv1alpha1.JobMapping{mappings[1]}, removed)
	})

	t.Run("removes mappings for non-existent columns", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "users",
				Column: "email", // non-existent column
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 1)
		require.Equal(t, "public", result[0].Schema)
		require.Equal(t, "users", result[0].Table)
		require.Equal(t, "id", result[0].Column)
		require.Equal(t, []*mgmtv1alpha1.JobMapping{mappings[1]}, removed)
	})

	t.Run("keeps all mappings when everything exists", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "users",
				Column: "email",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id":    {},
				"email": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 2)
		require.Equal(t, mappings, result)
		require.Empty(t, removed)
	})

	t.Run("returns empty slice when no mappings exist in schema", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.accounts": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Empty(t, result)
		require.Equal(t, mappings, removed)
	})
}

func Test_checkSourceShowsTheJob(t *testing.T) {
	mapping := &mgmtv1alpha1.JobMapping{Schema: "public", Table: "users", Column: "id"}

	require.NoError(t, checkSourceShowsTheJob(nil, nil), "a job that maps nothing yet")
	require.NoError(t, checkSourceShowsTheJob(
		[]*mgmtv1alpha1.JobMapping{mapping, {Schema: "public", Table: "users", Column: "gone"}},
		[]*mgmtv1alpha1.JobMapping{mapping},
	), "a source that lost a column")
	require.ErrorIs(t, checkSourceShowsTheJob(
		[]*mgmtv1alpha1.JobMapping{mapping},
		nil,
	), errSourceShowsNoMappedColumn, "a source that shows none of the job")
}

func Test_withoutNullableColumns(t *testing.T) {
	columnInfo := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.badge": {
			"code":   {IsNullable: true},
			"serial": {IsNullable: false},
			"site":   {IsNullable: false},
		},
	}
	uniqueKeys := map[string][][]string{
		"public.badge":   {{"code"}, {"site", "code"}, {"site", "serial"}, {"unknown"}},
		"public.missing": {{"id"}},
	}

	require.Equal(t, map[string][][]string{
		"public.badge": {{"site", "serial"}},
	}, withoutNullableColumns(uniqueKeys, columnInfo))
}

func Test_formatMappingColumns(t *testing.T) {
	t.Run("names a column the way the halt message does", func(t *testing.T) {
		require.Equal(t, []string{"public.users.email"}, formatMappingColumns(
			[]*mgmtv1alpha1.JobMapping{
				{Schema: "public", Table: "users", Column: "email"},
			},
		))
	})

	t.Run("sorts, because the mappings come from walking a map", func(t *testing.T) {
		require.Equal(t, []string{
			"public.users.email",
			"public.users.phone",
			"sales.orders.total",
		}, formatMappingColumns([]*mgmtv1alpha1.JobMapping{
			{Schema: "sales", Table: "orders", Column: "total"},
			{Schema: "public", Table: "users", Column: "phone"},
			{Schema: "public", Table: "users", Column: "email"},
		}))
	})

	t.Run("no mappings, no columns", func(t *testing.T) {
		require.Empty(t, formatMappingColumns(nil))
	})
}

func Test_autoMapNewColumns(t *testing.T) {
	passthrough := func(schema, table, column string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
						PassthroughConfig: &mgmtv1alpha1.Passthrough{},
					},
				},
			},
		}
	}
	generateDefault := func(schema, table, column string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
						GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
					},
				},
			},
		}
	}

	columnInfo := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.users": {
			"email":           {DataType: "character varying(255)"},
			"telephone":       {DataType: "character varying(20)"},
			"login":           {DataType: "character varying(50)"},
			"champ_libre":     {DataType: "text"},
			"email_normalise": {DataType: "character varying(255)"},
		},
	}
	constraints := &sqlmanager_shared.TableConstraints{
		UniqueConstraints: map[string][][]string{"public.users": {{"login"}}},
	}

	configOf := func(mappings []*mgmtv1alpha1.JobMapping, column string) *mgmtv1alpha1.TransformerConfig {
		for _, m := range mappings {
			if m.GetColumn() == column {
				return m.GetTransformer().GetConfig()
			}
		}
		t.Fatalf("no mapping for %s", column)
		return nil
	}

	t.Run("maps a recognised column as the catalogue does, and passes the rest through", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "email"),
			passthrough("public", "users", "telephone"),
			passthrough("public", "users", "champ_libre"),
		}, columnInfo, constraints, true)

		require.NotNil(t, configOf(out, "email").GetGenerateEmailConfig())
		// The catalogue's config, not an empty one: the phone keeps its format.
		require.True(t, configOf(out, "telephone").GetTransformPhoneNumberConfig().GetPreserveFormat())
		require.NotNil(t, configOf(out, "champ_libre").GetPassthroughConfig())
		require.Equal(t, []string{"public.users.email (email)", "public.users.telephone (phone_number)"}, anonymized)
		require.Equal(t, []string{"public.users.champ_libre"}, passedThrough)
	})

	t.Run("a unique column stays in passthrough, even when recognised", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "login"),
		}, columnInfo, constraints, true)

		require.NotNil(t, configOf(out, "login").GetPassthroughConfig())
		require.Empty(t, anonymized)
		require.Equal(t, []string{"public.users.login"}, passedThrough)
	})

	t.Run("without a derivation key, the phone is anonymized without keeping its format", func(t *testing.T) {
		// The option needs a key the run would not have: writing it to the job would fail
		// that run and every one after it on the column AutoMap had just mapped.
		out, anonymized, _ := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "telephone"),
		}, columnInfo, constraints, false)

		phone := configOf(out, "telephone").GetTransformPhoneNumberConfig()
		require.NotNil(t, phone, "the column is still anonymized")
		require.False(t, phone.GetPreserveFormat())
		require.Equal(t, []string{"public.users.telephone (phone_number)"}, anonymized)
	})

	t.Run("a column the destination recomputes keeps its GenerateDefault", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			generateDefault("public", "users", "email_normalise"),
		}, columnInfo, constraints, true)

		require.NotNil(t, configOf(out, "email_normalise").GetGenerateDefaultConfig())
		require.Empty(t, anonymized)
		require.Empty(t, passedThrough)
	})
}

func Test_constrainedColumns(t *testing.T) {
	constrained := constrainedColumns(&sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{"public.users": {"id"}},
		ForeignKeyConstraints: map[string][]*sqlmanager_shared.ForeignConstraint{
			"public.orders": {{
				Columns:    []string{"user_id"},
				ForeignKey: &sqlmanager_shared.ForeignKey{Table: "public.users", Columns: []string{"code"}},
			}},
		},
		UniqueIndexes: map[string][][]string{"public.users": {{"email", "tenant"}}},
	})
	for _, c := range []struct{ table, column string }{
		{"public.users", "id"},
		{"public.orders", "user_id"},
		{"public.users", "code"},
		{"public.users", "email"},
		{"public.users", "tenant"},
	} {
		_, ok := constrained[c.table][c.column]
		require.Truef(t, ok, "%s.%s", c.table, c.column)
	}
	_, ok := constrained["public.users"]["telephone"]
	require.False(t, ok)
	require.Empty(t, constrainedColumns(nil))
}
