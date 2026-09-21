package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The strategy is read once per dialect, in three switch statements that nothing keeps in step.
// Forgetting one is silent: the job falls back to the zero value, which drops the new column
// instead of mapping it, and no test would have noticed. Hence one case per dialect.
func Test_GetSqlJobSourceOpts_AnonymizePendingReview(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
					Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview_{
								AnonymizePendingReview: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.AnonymizeNewColumns)
		// Its own path: the passthrough is only its fallback, decided column by column.
		require.False(t, opts.PassthroughOnNewColumnAddition)
		require.False(t, opts.HaltOnNewColumnAddition)
		require.False(t, opts.GenerateNewColumnTransformers)
	})

	t.Run("mysql", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
					Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview_{
								AnonymizePendingReview: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.AnonymizeNewColumns)
		require.False(t, opts.PassthroughOnNewColumnAddition)
	})

	t.Run("mssql", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Mssql{
					Mssql: &mgmtv1alpha1.MssqlSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview_{
								AnonymizePendingReview: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_AnonymizePendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.AnonymizeNewColumns)
		require.False(t, opts.PassthroughOnNewColumnAddition)
	})

	t.Run("a plain passthrough anonymizes nothing", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
					Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_Passthrough_{
								Passthrough: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_Passthrough{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.PassthroughOnNewColumnAddition)
		require.False(t, opts.AnonymizeNewColumns)
	})
}
