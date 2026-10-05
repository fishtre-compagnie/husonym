package dtomaps

import (
	"encoding/json"
	"testing"

	"buf.build/go/protovalidate"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func piiDetectJobType(
	modelInput mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput,
) *mgmtv1alpha1.JobTypeConfig {
	return &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
			PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
				DataSampling: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
					IsEnabled:  true,
					ModelInput: modelInput,
				},
				TableScanFilter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
					Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll{
						IncludeAll: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_IncludeAll{},
					},
				},
			},
		},
	}
}

func jobRow(jobTypeConfig []byte) *db_queries.HusonymApiJob {
	return &db_queries.HusonymApiJob{
		JobtypeConfig: jobTypeConfig,
		ConnectionOptions: &pg_models.JobSourceOptions{
			PostgresOptions: &pg_models.PostgresSourceOptions{},
		},
	}
}

func Test_ToJobDto_PiiDetectModelInput(t *testing.T) {
	t.Parallel()

	for name, modelInput := range map[string]mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput{
		"unspecified": mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_UNSPECIFIED,
		"profiles":    mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_PROFILES,
		"values":      mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_VALUES,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stored, err := json.Marshal(piiDetectJobType(modelInput))
			require.NoError(t, err)

			job, err := ToJobDto(jobRow(stored), nil)
			require.NoError(t, err)
			require.Equal(
				t,
				modelInput,
				job.GetJobType().GetPiiDetect().GetDataSampling().GetModelInput(),
			)
		})
	}
}

func Test_ToJobDto_PiiDetectWithoutModelInputMember(t *testing.T) {
	t.Parallel()

	stored := []byte(`{"piiDetect":{"dataSampling":{"isEnabled":true},"tableScanFilter":{"includeAll":{}}}}`)

	job, err := ToJobDto(jobRow(stored), nil)
	require.NoError(t, err)

	sampling := job.GetJobType().GetPiiDetect().GetDataSampling()
	require.True(t, sampling.GetIsEnabled())
	require.Equal(
		t,
		mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_UNSPECIFIED,
		sampling.GetModelInput(),
	)
}

func Test_PiiDetectModelInput_Validation(t *testing.T) {
	t.Parallel()

	validator, err := protovalidate.New()
	require.NoError(t, err)

	t.Run("defined value is accepted", func(t *testing.T) {
		t.Parallel()
		jobType := piiDetectJobType(mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_VALUES)
		require.NoError(t, validator.Validate(jobType))
	})

	t.Run("undefined number is refused", func(t *testing.T) {
		t.Parallel()
		jobType := piiDetectJobType(mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput(99))
		require.Error(t, validator.Validate(proto.Message(jobType)))
	})
}
