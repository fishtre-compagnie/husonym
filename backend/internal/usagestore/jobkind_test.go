package usagestore

import (
	"encoding/json"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/stretchr/testify/require"
)

func Test_KindOfJob(t *testing.T) {
	piiDetect := &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{}},
	}
	sync := &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_Sync{Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{}},
	}
	syncOptions := &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: "c"}}
	for name, tc := range map[string]struct {
		options *pg_models.JobSourceOptions
		jobType *mgmtv1alpha1.JobTypeConfig
		want    JobKind
	}{
		"a synchronization":       {syncOptions, sync, JobKindSync},
		"a job with no type":      {syncOptions, nil, JobKindSync},
		"a job with no source":    {nil, nil, JobKindSync},
		"a generation":            {&pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}}, sync, JobKindGenerate},
		"a generation by a model": {&pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{}}, sync, JobKindAiGenerate},
		"a detection of PII":      {syncOptions, piiDetect, JobKindPiiDetect},
	} {
		t.Run(name, func(t *testing.T) {
			job := &db_queries.HusonymApiJob{ConnectionOptions: tc.options}
			if tc.jobType != nil {
				stored, err := json.Marshal(tc.jobType)
				require.NoError(t, err)
				job.JobtypeConfig = stored
			}
			require.Equal(t, tc.want, KindOfJob(job))
		})
	}
}

// A type that cannot be read does not hide the job: it is a synchronization.
func Test_KindOfJob_UnreadableTypeIsASynchronization(t *testing.T) {
	require.Equal(t, JobKindSync, KindOfJob(&db_queries.HusonymApiJob{JobtypeConfig: []byte("{")}))
}
