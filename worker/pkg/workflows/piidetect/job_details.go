package piidetect

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

type GetPiiDetectJobDetailsRequest struct {
	JobId string
}

type GetPiiDetectJobDetailsResponse struct {
	AccountId          string
	PiiDetectConfig    *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect
	SourceConnectionId string
	// TablesAtOnce is how many tables the run scans at once, as the worker that ran this
	// activity is set. It is read here, and not by the workflow, so that the history of
	// the run holds it.
	TablesAtOnce int `json:",omitempty"`
	// ModelInput is "values" when the job sends sample values to the model. It sits
	// beside the config, which is returned without it: the config is serialized in its
	// proto form, which a reader that does not know one of its members refuses whole.
	ModelInput string `json:",omitempty"`
}

// GetPiiDetectJobDetails reads the job: its account, what it scans and how. It refuses a
// job that is not a PII detection job, and a source whose tables cannot be scanned.
func (a *Activities) GetPiiDetectJobDetails(
	ctx context.Context,
	req *GetPiiDetectJobDetailsRequest,
) (*GetPiiDetectJobDetailsResponse, error) {
	resp, err := a.jobs.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.JobId}))
	if err != nil {
		return nil, fmt.Errorf("the job cannot be read: %w", err)
	}
	job := resp.Msg.GetJob()

	config := job.GetJobType().GetPiiDetect()
	if config == nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"unsupported job type, must be PiiDetect",
			errorTypeNotPiiDetectJob,
			nil,
		)
	}
	connectionId, err := sourceConnectionId(job.GetSource().GetOptions())
	if err != nil {
		return nil, err
	}

	details := &GetPiiDetectJobDetailsResponse{
		AccountId:          job.GetAccountId(),
		PiiDetectConfig:    config,
		SourceConnectionId: connectionId,
		TablesAtOnce:       a.tablesAtOnce,
	}
	sampling := config.GetDataSampling()
	if sampling.GetIsEnabled() &&
		sampling.GetModelInput() == mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_VALUES {
		details.ModelInput = report.InputValues
	}
	if sampling.GetModelInput() != mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_UNSPECIFIED {
		withoutInput, _ := proto.Clone(config).(*mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect)
		withoutInput.DataSampling.ModelInput = mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_UNSPECIFIED
		details.PiiDetectConfig = withoutInput
	}
	return details, nil
}

// sourceConnectionId returns the connection whose tables the job scans. A generation job
// is scanned through the connection its foreign keys come from.
func sourceConnectionId(source *mgmtv1alpha1.JobSourceOptions) (string, error) {
	switch options := source.GetConfig().(type) {
	case *mgmtv1alpha1.JobSourceOptions_Postgres:
		return options.Postgres.GetConnectionId(), nil
	case *mgmtv1alpha1.JobSourceOptions_Mysql:
		return options.Mysql.GetConnectionId(), nil
	case *mgmtv1alpha1.JobSourceOptions_Mssql:
		return options.Mssql.GetConnectionId(), nil
	case *mgmtv1alpha1.JobSourceOptions_Generate:
		if id := options.Generate.GetFkSourceConnectionId(); id != "" {
			return id, nil
		}
		return "", errUnsupportedSource("a data generation without a source connection")
	case *mgmtv1alpha1.JobSourceOptions_Mongodb:
		return "", errUnsupportedSource("a MongoDB database")
	case *mgmtv1alpha1.JobSourceOptions_Dynamodb:
		return "", errUnsupportedSource("a DynamoDB database")
	case *mgmtv1alpha1.JobSourceOptions_AiGenerate:
		return "", errUnsupportedSource("a generation by a language model")
	case *mgmtv1alpha1.JobSourceOptions_AwsS3:
		return "", errUnsupportedSource("an object storage")
	}
	return "", errUnsupportedSource("of a kind that has no table to read")
}
