package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The strategy is read once per dialect, in three switch statements that nothing keeps in step.
// Forgetting one is silent: the job falls back to the zero value, which drops the new column
// instead of passing it through, and no test would have noticed. Hence one case per dialect.
func Test_GetSqlJobSourceOpts_PassthroughPendingReview(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
					Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview_{
								PassthroughPendingReview: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		// The review flag never travels alone: it qualifies a passthrough, and the data path has
		// to stay the one that is already exercised.
		require.True(t, opts.PassthroughOnNewColumnAddition)
		require.True(t, opts.PassthroughPendingReview)
		require.False(t, opts.HaltOnNewColumnAddition)
		require.False(t, opts.GenerateNewColumnTransformers)
	})

	t.Run("mysql", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
					Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview_{
								PassthroughPendingReview: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.PassthroughOnNewColumnAddition)
		require.True(t, opts.PassthroughPendingReview)
	})

	t.Run("mssql", func(t *testing.T) {
		opts, err := GetSqlJobSourceOpts(&mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Mssql{
					Mssql: &mgmtv1alpha1.MssqlSourceConnectionOptions{
						NewColumnAdditionStrategy: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy{
							Strategy: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview_{
								PassthroughPendingReview: &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy_PassthroughPendingReview{},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, opts)
		require.True(t, opts.PassthroughOnNewColumnAddition)
		require.True(t, opts.PassthroughPendingReview)
	})

	// A plain passthrough is a decision someone took, not a debt: it must not start showing up
	// in the review list because the two strategies share a data path.
	t.Run("a plain passthrough is not pending review", func(t *testing.T) {
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
		require.False(t, opts.PassthroughPendingReview)
	})
}
