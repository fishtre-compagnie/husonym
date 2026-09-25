package benthosbuilder_builders

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	rc "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const preflightTable = "public.article"

func mapped(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{Config: config}
}

func passthroughConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
		PassthroughConfig: &mgmtv1alpha1.Passthrough{},
	}}
}

func categoricalConfig(categories string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateCategoricalConfig{
		GenerateCategoricalConfig: &mgmtv1alpha1.GenerateCategorical{Categories: &categories},
	}}
}

func uuidConfig(hyphens *bool) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateUuidConfig{
		GenerateUuidConfig: &mgmtv1alpha1.GenerateUuid{IncludeHyphens: hyphens},
	}}
}

func defaultConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
		GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
	}}
}

func jobWithAttempts(attempts *int32) *mgmtv1alpha1.Job {
	return &mgmtv1alpha1.Job{SyncOptions: &mgmtv1alpha1.ActivityOptions{
		RetryPolicy: &mgmtv1alpha1.RetryPolicy{MaximumAttempts: attempts},
	}}
}

// planOf builds the run configs of one table the way the SQL builder does.
func planOf(t *testing.T, columns []string, constraints *sqlmanager_shared.TableConstraints) []*rc.RunConfig {
	t.Helper()
	configs, err := rc.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{},
		map[string]string{},
		constraints.PrimaryKeyConstraints,
		map[string][]string{preflightTable: columns},
		constraints.UniqueIndexes,
		constraints.UniqueConstraints,
	)
	require.NoError(t, err)
	return configs
}

func kindsOf(findings []*preflight.Finding) []mgmtv1alpha1.PreflightFinding_Kind {
	kinds := []mgmtv1alpha1.PreflightFinding_Kind{}
	for _, f := range findings {
		kinds = append(kinds, f.Kind)
	}
	return kinds
}

func Test_sourceFindings_ReadInOneStream(t *testing.T) {
	three := int32(3)
	keyless := &sqlmanager_shared.TableConstraints{}
	transformers := map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{preflightTable: {
		"niveau": mapped(passthroughConfig()), "message": mapped(passthroughConfig()),
	}}

	t.Run("a table without key, retried under Benthos, may be written twice", func(t *testing.T) {
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&three), false, true,
			planOf(t, []string{"niveau", "message"}, keyless), keyless, nil, transformers, nil)
		require.NoError(t, err)
		require.Equal(t, []mgmtv1alpha1.PreflightFinding_Kind{
			mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
			mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE,
		}, kindsOf(findings))
		assert.Equal(t, preflight.Information, findings[0].Level)
		assert.Contains(t, findings[0].Message, "it has no primary key nor unique key")
		assert.Equal(t, preflight.Warning, findings[1].Level)
		assert.Equal(t, preflightTable, findings[1].Table)
	})

	t.Run("Athanor, or a single attempt, writes it once", func(t *testing.T) {
		one := int32(1)
		for name, run := range map[string]struct {
			job         *mgmtv1alpha1.Job
			usesAthanor bool
		}{
			"athanor":        {jobWithAttempts(&three), true},
			"one attempt":    {jobWithAttempts(&one), false},
			"no retry given": {&mgmtv1alpha1.Job{}, false},
		} {
			findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), run.job, run.usesAthanor, true,
				planOf(t, []string{"niveau", "message"}, keyless), keyless, nil, transformers, nil)
			require.NoError(t, err, name)
			assert.Equal(t, []mgmtv1alpha1.PreflightFinding_Kind{
				mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
			}, kindsOf(findings), name)
		}
	})

	t.Run("unlimited attempts retry", func(t *testing.T) {
		zero := int32(0)
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&zero), false, true,
			planOf(t, []string{"niveau", "message"}, keyless), keyless, nil, transformers, nil)
		require.NoError(t, err)
		assert.Contains(t, kindsOf(findings), mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE)
	})

	t.Run("a primary key the job does not read", func(t *testing.T) {
		withKey := &sqlmanager_shared.TableConstraints{
			PrimaryKeyConstraints: map[string][]string{preflightTable: {"my_row_id"}},
		}
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&three), false, true,
			planOf(t, []string{"niveau", "message"}, withKey), withKey, nil, transformers, nil)
		require.NoError(t, err)
		// The table has a key to collide on: a retry writes nothing twice.
		require.Equal(t, []mgmtv1alpha1.PreflightFinding_Kind{
			mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
		}, kindsOf(findings))
		assert.Contains(t, findings[0].Message, "the job does not read its primary key (my_row_id)")
	})

	t.Run("a unique key holding NULL", func(t *testing.T) {
		// The SQL builder leaves nullable keys out of what may page a table, not out of the keys.
		nullable := &sqlmanager_shared.TableConstraints{
			UniqueIndexes: map[string][][]string{preflightTable: {{"message"}}},
		}
		configs := planOf(t, []string{"niveau", "message"}, &sqlmanager_shared.TableConstraints{})
		columns := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{preflightTable: {
			"niveau": {IsNullable: false}, "message": {IsNullable: true},
		}}
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&three), false, true,
			configs, nullable, columns, transformers, nil)
		require.NoError(t, err)
		// A row holding NULL in the key collides with nothing: a retry writes it again.
		require.Equal(t, []mgmtv1alpha1.PreflightFinding_Kind{
			mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
			mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE,
		}, kindsOf(findings))
		assert.Contains(t, findings[0].Message, "none of its unique keys is both read by the job and free of NULL")

		notNull := &sqlmanager_shared.TableConstraints{
			UniqueIndexes: map[string][][]string{preflightTable: {{"niveau"}}},
		}
		findings, err = sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&three), false, true,
			configs, notNull, columns, transformers, nil)
		require.NoError(t, err)
		assert.NotContains(t, kindsOf(findings), mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE)
	})

	t.Run("a table paged on its key", func(t *testing.T) {
		withKey := &sqlmanager_shared.TableConstraints{
			PrimaryKeyConstraints: map[string][]string{preflightTable: {"niveau"}},
		}
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), jobWithAttempts(&three), false, true,
			planOf(t, []string{"niveau", "message"}, withKey), withKey, nil, transformers, nil)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})
}

func Test_sourceFindings_ConstantOnUnique(t *testing.T) {
	constraints := &sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{preflightTable: {"id"}},
		UniqueIndexes:         map[string][][]string{preflightTable: {{"code"}, {"code", "libelle"}}},
	}
	columns := []string{"id", "code", "libelle"}
	run := func(t *testing.T, transformers map[string]*mgmtv1alpha1.JobMappingTransformer) []*preflight.Finding {
		t.Helper()
		findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), &mgmtv1alpha1.Job{}, false, true,
			planOf(t, columns, constraints), constraints, nil,
			map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{preflightTable: transformers}, nil)
		require.NoError(t, err)
		return findings
	}

	t.Run("one category on a unique column", func(t *testing.T) {
		findings := run(t, map[string]*mgmtv1alpha1.JobMappingTransformer{
			"id": mapped(passthroughConfig()), "code": mapped(categoricalConfig("constante")),
			"libelle": mapped(passthroughConfig()),
		})
		require.Len(t, findings, 1)
		assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_CONSTANT_ON_UNIQUE, findings[0].Kind)
		assert.Equal(t, preflight.Warning, findings[0].Level)
		assert.Equal(t, []string{"code"}, findings[0].Columns)
	})

	t.Run("one category repeated is one value", func(t *testing.T) {
		findings := run(t, map[string]*mgmtv1alpha1.JobMappingTransformer{
			"id": mapped(passthroughConfig()), "code": mapped(categoricalConfig("a,a")),
			"libelle": mapped(passthroughConfig()),
		})
		assert.Equal(t, []mgmtv1alpha1.PreflightFinding_Kind{mgmtv1alpha1.PreflightFinding_KIND_CONSTANT_ON_UNIQUE}, kindsOf(findings))
	})

	t.Run("several categories, or a key another column tells apart", func(t *testing.T) {
		assert.Empty(t, run(t, map[string]*mgmtv1alpha1.JobMappingTransformer{
			"id": mapped(passthroughConfig()), "code": mapped(categoricalConfig("a,b")),
			"libelle": mapped(passthroughConfig()),
		}))
	})

	t.Run("both columns of a composite key constant", func(t *testing.T) {
		findings := run(t, map[string]*mgmtv1alpha1.JobMappingTransformer{
			"id": mapped(passthroughConfig()), "code": mapped(categoricalConfig("a")),
			"libelle": mapped(categoricalConfig("b")),
		})
		require.Len(t, findings, 2)
		assert.Equal(t, []string{"code"}, findings[0].Columns)
		assert.Equal(t, []string{"code", "libelle"}, findings[1].Columns)
	})
}

func Test_sourceFindings_ReferenceClearedBySubset(t *testing.T) {
	constraints := &sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{preflightTable: {"id"}},
	}
	configs := planOf(t, []string{"id", "station_id"}, constraints)
	foreignKeys := map[string][]*tableplan.ForeignKey{configs[0].Id(): {
		{Columns: []string{"station_id"}, NotNull: []bool{false}, ParentSchema: "public", ParentTable: "station", ParentReduced: true},
		// Mandatory: the engine drops the row instead, nothing is cleared.
		{Columns: []string{"id"}, NotNull: []bool{true}, ParentSchema: "public", ParentTable: "station", ParentReduced: true},
		// The whole parent is copied: every reference finds its row.
		{Columns: []string{"station_id"}, NotNull: []bool{false}, ParentSchema: "public", ParentTable: "pays"},
	}}
	findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), &mgmtv1alpha1.Job{}, false, true, configs, constraints, nil,
		map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{}, foreignKeys)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET, findings[0].Kind)
	assert.Equal(t, preflight.Information, findings[0].Level)
	assert.Equal(t, []string{"station_id"}, findings[0].Columns)
	assert.Contains(t, findings[0].Message, "public.station")
}

func Test_destinationFindings(t *testing.T) {
	generated := func() *sqlmanager_shared.DatabaseSchemaRow {
		return &sqlmanager_shared.DatabaseSchemaRow{UpdateAllowed: false}
	}
	varchar := func(length int) *sqlmanager_shared.DatabaseSchemaRow {
		return &sqlmanager_shared.DatabaseSchemaRow{UpdateAllowed: true, CharacterMaximumLength: length}
	}
	config := &bb_internal.BenthosSourceConfig{
		TableSchema: "public", TableName: "article", RunType: rc.RunTypeInsert,
		Columns: []string{"total", "code"}, GeneratedColumns: []string{"total"},
	}
	run := func(
		t *testing.T, usesAthanor bool, config *bb_internal.BenthosSourceConfig,
		destination, source map[string]*sqlmanager_shared.DatabaseSchemaRow,
		transformers map[string]*mgmtv1alpha1.JobMappingTransformer,
	) []*preflight.Finding {
		t.Helper()
		findings, err := destinationFindings(context.Background(), newTransformerConfigs(nil), usesAthanor, "dest", config,
			destination, source, transformers)
		require.NoError(t, err)
		return findings
	}

	t.Run("a generated column given a value", func(t *testing.T) {
		destination := map[string]*sqlmanager_shared.DatabaseSchemaRow{"total": generated(), "code": varchar(10)}
		transformers := map[string]*mgmtv1alpha1.JobMappingTransformer{
			"total": mapped(passthroughConfig()), "code": mapped(passthroughConfig()),
		}
		benthos := run(t, false, config, destination, nil, transformers)
		require.Len(t, benthos, 1)
		assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN, benthos[0].Kind)
		assert.Equal(t, preflight.Blocking, benthos[0].Level)
		assert.Equal(t, "dest", benthos[0].ConnectionID)
		assert.Contains(t, benthos[0].Message, "Generate Default")

		athanor := run(t, true, config, destination, nil, transformers)
		require.Len(t, athanor, 1)
		assert.Equal(t, preflight.Information, athanor[0].Level)

		// Generated in the destination alone: Athanor writes it, and is refused.
		sourceWritten := &bb_internal.BenthosSourceConfig{
			TableSchema: "public", TableName: "article", RunType: rc.RunTypeInsert, Columns: []string{"total"},
		}
		refused := run(t, true, sourceWritten, destination, nil, transformers)
		require.Len(t, refused, 1)
		assert.Equal(t, preflight.Blocking, refused[0].Level)

		assert.Empty(t, run(t, false, config, destination, nil, map[string]*mgmtv1alpha1.JobMappingTransformer{
			"total": mapped(defaultConfig()), "code": mapped(passthroughConfig()),
		}))

		// A transformer of the account that stands for Generate Default writes nothing either.
		configs := newTransformerConfigs(nil)
		configs.resolved["udt"] = defaultConfig()
		udt, err := destinationFindings(context.Background(), configs, true, "dest", config, destination, nil,
			map[string]*mgmtv1alpha1.JobMappingTransformer{
				"total": mapped(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
					UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "udt"},
				}}),
				"code": mapped(passthroughConfig()),
			})
		require.NoError(t, err)
		assert.Empty(t, udt)
	})

	t.Run("an output longer than the column", func(t *testing.T) {
		noHyphens := false
		for name, test := range map[string]struct {
			transformer *mgmtv1alpha1.TransformerConfig
			column      int
			source      int
			longest     string
		}{
			"uuid":                              {uuidConfig(nil), 10, 0, "uuids of up to 36"},
			"uuid without hyphens":              {uuidConfig(&noHyphens), 10, 0, "uuids of up to 32"},
			"sha-256":                           {&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateSha256HashConfig{GenerateSha256HashConfig: &mgmtv1alpha1.GenerateSha256Hash{}}}, 10, 0, "SHA-256 hashes of up to 64"},
			"categories":                        {categoricalConfig("court,beaucoup plus long"), 10, 0, "categories of up to 18"},
			"source wider than the destination": {passthroughConfig(), 5, 40, "the values of the source of up to 40"},
		} {
			findings := run(t, false, &bb_internal.BenthosSourceConfig{
				TableSchema: "public", TableName: "article", RunType: rc.RunTypeInsert, Columns: []string{"code"},
			}, map[string]*sqlmanager_shared.DatabaseSchemaRow{"code": varchar(test.column)},
				map[string]*sqlmanager_shared.DatabaseSchemaRow{"code": varchar(test.source)},
				map[string]*mgmtv1alpha1.JobMappingTransformer{"code": mapped(test.transformer)})
			require.Len(t, findings, 1, name)
			assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG, findings[0].Kind, name)
			assert.Equal(t, preflight.Warning, findings[0].Level, name)
			assert.Contains(t, findings[0].Message, test.longest, name)
		}
	})

	t.Run("an output the column holds", func(t *testing.T) {
		noHyphens := false
		for name, test := range map[string]struct {
			transformer *mgmtv1alpha1.TransformerConfig
			column      int
			source      int
		}{
			"uuid":                                {uuidConfig(nil), 36, 0},
			"uuid without hyphens":                {uuidConfig(&noHyphens), 32, 0},
			"same width":                          {passthroughConfig(), 40, 40},
			"a type without length, such as uuid": {uuidConfig(nil), 0, 0},
		} {
			assert.Empty(t, run(t, false, &bb_internal.BenthosSourceConfig{
				TableSchema: "public", TableName: "article", RunType: rc.RunTypeInsert, Columns: []string{"code"},
			}, map[string]*sqlmanager_shared.DatabaseSchemaRow{"code": varchar(test.column)},
				map[string]*sqlmanager_shared.DatabaseSchemaRow{"code": varchar(test.source)},
				map[string]*mgmtv1alpha1.JobMappingTransformer{"code": mapped(test.transformer)}), name)
		}
	})

	t.Run("an update pass writes what the insert pass already reported", func(t *testing.T) {
		assert.Empty(t, run(t, false, &bb_internal.BenthosSourceConfig{
			TableSchema: "public", TableName: "article", RunType: rc.RunTypeUpdate, Columns: []string{"code"},
		}, map[string]*sqlmanager_shared.DatabaseSchemaRow{"code": varchar(10)}, nil,
			map[string]*mgmtv1alpha1.JobMappingTransformer{"code": mapped(uuidConfig(nil))}))
	})
}

// The subset of TRANSFERT joins STATION on the first of its keys, by name: a row whose
// arrival station is left out is left out too. The return station is only cleared.
func Test_sourceFindings_KeyTheSubsetJoinsOn(t *testing.T) {
	const transfert, station = "public.transfert", "public.station"
	toStation := func(column string) *sqlmanager_shared.ForeignConstraint {
		return &sqlmanager_shared.ForeignConstraint{
			Columns: []string{column}, NotNullable: []bool{false},
			ForeignKey: &sqlmanager_shared.ForeignKey{Table: station, Columns: []string{"id"}},
		}
	}
	configs, err := rc.BuildRunConfigs(
		map[string][]*sqlmanager_shared.ForeignConstraint{transfert: {toStation("arrivee_id"), toStation("retour_id")}},
		map[string]string{station: "id = 1"},
		map[string][]string{transfert: {"id"}, station: {"id"}},
		map[string][]string{transfert: {"id", "arrivee_id", "retour_id"}, station: {"id"}},
		map[string][][]string{}, map[string][][]string{},
	)
	require.NoError(t, err)
	foreignKeys := map[string][]*tableplan.ForeignKey{}
	for _, config := range configs {
		if config.Table() != transfert {
			continue
		}
		for _, column := range []string{"arrivee_id", "retour_id"} {
			foreignKeys[config.Id()] = append(foreignKeys[config.Id()], &tableplan.ForeignKey{
				Columns: []string{column}, NotNull: []bool{false},
				ParentSchema: "public", ParentTable: "station", ParentColumns: []string{"id"}, ParentReduced: true,
			})
		}
	}
	constraints := &sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{transfert: {"id"}, station: {"id"}},
	}
	findings, err := sourceFindings(context.Background(), newTransformerConfigs(nil), &mgmtv1alpha1.Job{}, false, true, configs, constraints, nil,
		map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{}, foreignKeys)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET, findings[0].Kind)
	assert.Equal(t, []string{"retour_id"}, findings[0].Columns)

	// Subset by the where clause of each table alone: no key is joined, both are cleared.
	findings, err = sourceFindings(context.Background(), newTransformerConfigs(nil), &mgmtv1alpha1.Job{}, false, false, configs, constraints, nil,
		map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{}, foreignKeys)
	require.NoError(t, err)
	require.Len(t, findings, 2)
}

// Each destination is compared with its own schema: two PostgreSQL destinations share the
// builder, and only the second computes total itself.
func Test_BuildDestinationConfig_FindingsPerDestination(t *testing.T) {
	sqlmanagerclient := sqlmanager.NewMockSqlManagerClient(t)
	schemaOf := func(generated bool) map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow {
		return map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{preflightTable: {
			"id":    {ColumnName: "id", DataType: "integer", UpdateAllowed: true},
			"total": {ColumnName: "total", DataType: "integer", UpdateAllowed: !generated},
		}}
	}
	for id, generated := range map[string]bool{"dest-a": false, "dest-b": true} {
		db := sqlmanager.NewMockSqlDatabase(t)
		db.EXPECT().GetSchemaColumnMap(mock.Anything).Return(schemaOf(generated), nil).Once()
		db.EXPECT().Close().Return()
		sqlmanagerclient.EXPECT().NewSqlConnection(mock.Anything, mock.Anything,
			mock.MatchedBy(func(c *mgmtv1alpha1.Connection) bool { return c.GetId() == id }), mock.Anything).
			Return(sqlmanager.NewPostgresSqlConnection(db), nil).Once()
	}

	builder := NewSqlSyncBuilder(nil, sqlmanagerclient, sqlmanager_shared.PostgresDriver, nil, 100).(*sqlSyncBuilder)
	builder.sqlSourceSchemaColumnInfoMap = schemaOf(false)
	builder.colTransformerMap = map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{preflightTable: {
		"id": mapped(passthroughConfig()), "total": mapped(passthroughConfig()),
	}}
	source := &bb_internal.BenthosSourceConfig{
		Name: preflightTable + ".insert", TableSchema: "public", TableName: "article",
		RunType: rc.RunTypeInsert, Columns: []string{"id", "total"},
	}
	var findings []*preflight.Finding
	for _, id := range []string{"dest-a", "dest-b"} {
		params := &bb_internal.DestinationParams{
			SourceConfig: source,
			Job:          &mgmtv1alpha1.Job{},
			DestinationOpts: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
			DestConnection: &mgmtv1alpha1.Connection{Id: id, ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
			}},
			Logger: testutil.GetTestLogger(t),
		}
		_, err := builder.BuildDestinationConfig(context.Background(), params)
		require.NoError(t, err)
		findings = append(findings, params.Findings...)
	}
	require.Len(t, findings, 1)
	assert.Equal(t, mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN, findings[0].Kind)
	assert.Equal(t, "dest-b", findings[0].ConnectionID)
}
