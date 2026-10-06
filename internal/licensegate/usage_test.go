package licensegate

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func piiText(anonymizers ...*mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.TransformerConfig {
	entities := map[string]*mgmtv1alpha1.PiiAnonymizer{}
	for i, config := range anonymizers {
		entities[string(rune('A'+i))] = &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
			Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: config},
		}}
	}
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
		TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{EntityAnonymizers: entities},
	}}
}

func userDefined(id string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: id},
	}}
}

func transformJavascript() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: "return value;"},
	}}
}

func generateJavascript() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateJavascriptConfig{
		GenerateJavascriptConfig: &mgmtv1alpha1.GenerateJavascript{Code: "return 1;"},
	}}
}

func passthrough() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
		PassthroughConfig: &mgmtv1alpha1.Passthrough{},
	}}
}

func mappingWith(configs ...*mgmtv1alpha1.TransformerConfig) []*mgmtv1alpha1.JobMapping {
	mappings := make([]*mgmtv1alpha1.JobMapping, 0, len(configs))
	for _, config := range configs {
		mappings = append(mappings, &mgmtv1alpha1.JobMapping{
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: config},
		})
	}
	return mappings
}

func sourceWith(options *mgmtv1alpha1.JobSourceOptions) *mgmtv1alpha1.JobSource {
	return &mgmtv1alpha1.JobSource{Options: options}
}

func ptr(s string) *string { return &s }

func postgresWhere(where *string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			Schemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{{
				Schema: "public",
				Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: "a"}, {Table: "b", WhereClause: where}},
			}},
		},
	}})
}

func mysqlWhere(where *string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
		Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
			Schemas: []*mgmtv1alpha1.MysqlSourceSchemaOption{{
				Schema: "db",
				Tables: []*mgmtv1alpha1.MysqlSourceTableOption{{Table: "a", WhereClause: where}},
			}},
		},
	}})
}

func mssqlWhere(where *string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mssql{
		Mssql: &mgmtv1alpha1.MssqlSourceConnectionOptions{
			Schemas: []*mgmtv1alpha1.MssqlSourceSchemaOption{{
				Schema: "dbo",
				Tables: []*mgmtv1alpha1.MssqlSourceTableOption{{Table: "a", WhereClause: where}},
			}},
		},
	}})
}

func dynamodbWhere(where *string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Dynamodb{
		Dynamodb: &mgmtv1alpha1.DynamoDBSourceConnectionOptions{
			Tables: []*mgmtv1alpha1.DynamoDBSourceTableOption{{Table: "a", WhereClause: where}},
		},
	}})
}

func dynamodbUnmapped(unmapped *mgmtv1alpha1.DynamoDBSourceUnmappedTransformConfig) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Dynamodb{
		Dynamodb: &mgmtv1alpha1.DynamoDBSourceConnectionOptions{UnmappedTransforms: unmapped},
	}})
}

func transformerOf(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{Config: config}
}

func piiDetectType() *mgmtv1alpha1.JobTypeConfig {
	return &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
		PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
	}}
}

// lookupOf serves the configs of known user-defined transformers, and counts the calls per id.
func lookupOf(known map[string]*mgmtv1alpha1.TransformerConfig, calls map[string]int) TransformerLookup {
	return func(_ context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
		calls[id]++
		return known[id], nil
	}
}

func Test_FeaturesUsedBy(t *testing.T) {
	known := map[string]*mgmtv1alpha1.TransformerConfig{
		"pii":   piiText(),
		"plain": passthrough(),
	}

	tests := []struct {
		name  string
		facts JobFacts
		want  []license.Feature
	}{
		{
			name:  "enabled hooks",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{}, HasEnabledHooks: true},
			want:  []license.Feature{license.FeatureJobHooks},
		},
		{
			name:  "pii text mapping",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(passthrough(), piiText())}},
			want:  []license.Feature{license.FeaturePiiText},
		},
		{
			name:  "pii text through a user-defined transformer",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("pii"))}},
			want:  []license.Feature{license.FeaturePiiText, license.FeatureCustomTransformers},
		},
		{
			name:  "pii text through a user-defined transformer nested in the anonymizers of a pii text",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(piiText(userDefined("plain")))}},
			want:  []license.Feature{license.FeaturePiiText, license.FeatureCustomTransformers},
		},
		{
			name:  "user-defined transformer that is not pii text",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("plain"))}},
			want:  []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:  "transform javascript",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(transformJavascript())}},
			want:  []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:  "generate javascript",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Mappings: mappingWith(generateJavascript())}},
			want:  []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:  "dynamodb unmapped default b is pii text",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: dynamodbUnmapped(&mgmtv1alpha1.DynamoDBSourceUnmappedTransformConfig{B: transformerOf(piiText())})}},
			want:  []license.Feature{license.FeaturePiiText},
		},
		{
			name:  "dynamodb unmapped default boolean is javascript",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: dynamodbUnmapped(&mgmtv1alpha1.DynamoDBSourceUnmappedTransformConfig{Boolean: transformerOf(transformJavascript())})}},
			want:  []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:  "dynamodb unmapped default n is a user-defined pii text",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: dynamodbUnmapped(&mgmtv1alpha1.DynamoDBSourceUnmappedTransformConfig{N: transformerOf(userDefined("pii"))})}},
			want:  []license.Feature{license.FeaturePiiText, license.FeatureCustomTransformers},
		},
		{
			name:  "dynamodb unmapped default s is a generate javascript",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: dynamodbUnmapped(&mgmtv1alpha1.DynamoDBSourceUnmappedTransformConfig{S: transformerOf(generateJavascript())})}},
			want:  []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:  "postgres where clause",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: postgresWhere(ptr("id > 10"))}},
			want:  []license.Feature{license.FeatureSubsetting},
		},
		{
			name:  "mysql where clause",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: mysqlWhere(ptr("id > 10"))}},
			want:  []license.Feature{license.FeatureSubsetting},
		},
		{
			name:  "mssql where clause",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: mssqlWhere(ptr("id > 10"))}},
			want:  []license.Feature{license.FeatureSubsetting},
		},
		{
			name:  "dynamodb where clause",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: dynamodbWhere(ptr("pk = 1"))}},
			want:  []license.Feature{license.FeatureSubsetting},
		},
		{
			name:  "blank where clause is not subsetting",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: postgresWhere(ptr("  \t "))}},
			want:  nil,
		},
		{
			name:  "empty where clause is not subsetting",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{Source: mysqlWhere(ptr(""))}},
			want:  nil,
		},
		{
			name:  "pii detection job",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{JobType: piiDetectType()}},
			want:  []license.Feature{license.FeaturePiiDetection},
		},
		{
			name: "pii detection job reports what else its definition shows",
			facts: JobFacts{Job: &mgmtv1alpha1.Job{
				JobType:  piiDetectType(),
				Source:   mssqlWhere(ptr("id > 1")),
				Mappings: mappingWith(transformJavascript()),
			}},
			want: []license.Feature{license.FeaturePiiDetection, license.FeatureCustomTransformers, license.FeatureSubsetting},
		},
		{
			name: "everything, in the order of the features, each once",
			facts: JobFacts{
				HasEnabledHooks: true,
				Job: &mgmtv1alpha1.Job{
					JobType:  piiDetectType(),
					Source:   postgresWhere(ptr("id > 1")),
					Mappings: mappingWith(piiText(), userDefined("pii"), transformJavascript(), piiText()),
				},
			},
			want: []license.Feature{
				license.FeatureJobHooks,
				license.FeaturePiiText,
				license.FeaturePiiDetection,
				license.FeatureCustomTransformers,
				license.FeatureSubsetting,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FeaturesUsedBy(context.Background(), tt.facts, lookupOf(known, map[string]int{}))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_FeaturesUsedBy_PlainSyncJobUsesNothing(t *testing.T) {
	job := &mgmtv1alpha1.Job{
		JobType: &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_Sync{
			Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{},
		}},
		Source:   postgresWhere(nil),
		Mappings: mappingWith(passthrough(), passthrough()),
	}

	got, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, lookupOf(nil, map[string]int{}))

	require.NoError(t, err)
	require.Empty(t, got)
}

func Test_FeaturesUsedBy_ForeignKeySubsetFlagAloneIsNotSubsetting(t *testing.T) {
	job := &mgmtv1alpha1.Job{Source: sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			SubsetByForeignKeyConstraints: true,
			Schemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{{
				Schema: "public",
				Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: "a"}},
			}},
		},
	}})}

	got, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, nil)

	require.NoError(t, err)
	require.Empty(t, got)
}

func Test_FeaturesUsedBy_ADeletedUserDefinedTransformerIsNotAnError(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("gone"))}
	lookup := func(context.Context, string) (*mgmtv1alpha1.TransformerConfig, error) { return nil, nil }

	got, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, lookup)

	require.NoError(t, err)
	require.Equal(t, []license.Feature{license.FeatureCustomTransformers}, got)
}

func Test_FeaturesUsedBy_ALookupFailureFailsTheCall(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("a"))}
	boom := errors.New("boom")
	lookup := func(context.Context, string) (*mgmtv1alpha1.TransformerConfig, error) { return nil, boom }

	got, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, lookup)

	require.ErrorIs(t, err, boom)
	require.Nil(t, got)
}

func Test_FeaturesUsedBy_LooksUpEachUserDefinedTransformerOnce(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("a"), userDefined("a"), piiText(userDefined("a"), userDefined("b")))}
	calls := map[string]int{}

	_, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, lookupOf(map[string]*mgmtv1alpha1.TransformerConfig{}, calls))

	require.NoError(t, err)
	require.Equal(t, map[string]int{"a": 1, "b": 1}, calls)
}

func Test_FeaturesUsedBy_ANilLookupResolvesNothing(t *testing.T) {
	job := &mgmtv1alpha1.Job{Mappings: mappingWith(userDefined("a"))}

	got, err := FeaturesUsedBy(context.Background(), JobFacts{Job: job}, nil)

	require.NoError(t, err)
	require.Equal(t, []license.Feature{license.FeatureCustomTransformers}, got)
}

func Test_MissingFeatures(t *testing.T) {
	used := []license.Feature{license.FeaturePiiText, license.FeatureSubsetting, license.FeatureJobHooks}
	lic := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureSubsetting))

	require.Equal(t, []license.Feature{license.FeaturePiiText, license.FeatureJobHooks}, MissingFeatures(lic, used))
	require.Empty(t, MissingFeatures(lic, nil))
}

func Test_MissingFeatures_WildcardMissesNothing(t *testing.T) {
	used := license.AllFeatures()
	lic := testutil.NewFakeEELicense(testutil.WithIsValid())

	require.Empty(t, MissingFeatures(lic, used))
}

func Test_MissingFeatures_NoLicenseMissesEverything(t *testing.T) {
	used := []license.Feature{license.FeaturePiiText, license.FeatureSubsetting}

	require.Equal(t, used, MissingFeatures(nil, used))
}

func Test_RefusalMessage(t *testing.T) {
	require.Equal(t,
		"this job uses features the license does not include: pii_text, subsetting",
		RefusalMessage([]license.Feature{license.FeaturePiiText, license.FeatureSubsetting}),
	)
	require.Equal(t,
		"this job uses features the license does not include: pii_text",
		RefusalMessage([]license.Feature{license.FeaturePiiText}),
	)
}
