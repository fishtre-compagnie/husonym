package piidetect_workflow_register

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	piidetect_job_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/job"
	piidetect_job_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/job/activities"
	piidetect_table_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"github.com/openai/openai-go"
	tmprl "go.temporal.io/sdk/client"
)

type Worker interface {
	RegisterWorkflow(workflow any)
	RegisterActivity(activity any)
}

// Config holds the LLM and cascade configuration for the pii detect workflows.
type Config struct {
	// LLMModel is the chat completions model used by the LLM stage; empty means the default model.
	LLMModel string
	// ConfidenceThreshold is the stage-1 confidence threshold; values <= 0 fall back to the default.
	ConfidenceThreshold float32
}

func Register(
	w Worker,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	jobclient mgmtv1alpha1connect.JobServiceClient,
	openaiclient *openai.Client, // may be nil; the LLM stage is then skipped
	connectiondatabuilder connectiondata.ConnectionDataBuilder,
	eelicense license.EEInterface,
	tmprlScheduleClient tmprl.ScheduleClient,
	config *Config,
) {
	if config == nil {
		config = &Config{}
	}

	tablePiiDetectWorkflow := piidetect_table_workflow.New(&piidetect_table_workflow.Config{
		ConfidenceThreshold: config.ConfidenceThreshold,
	})
	jobPiiDetectWorkflow := piidetect_job_workflow.New(eelicense)

	w.RegisterWorkflow(tablePiiDetectWorkflow.TablePiiDetect)
	w.RegisterWorkflow(jobPiiDetectWorkflow.JobPiiDetect)

	var completionsClient piidetect_table_activities.OpenAiCompletionsClient
	if openaiclient != nil {
		completionsClient = &openaiclient.Chat.Completions
	}

	tablePiiDetectActivitites := piidetect_table_activities.New(
		connclient,
		completionsClient,
		connectiondatabuilder,
		jobclient,
		config.LLMModel,
	)
	w.RegisterActivity(tablePiiDetectActivitites.GetColumnData)
	w.RegisterActivity(tablePiiDetectActivitites.DetectPiiRegex)
	w.RegisterActivity(tablePiiDetectActivitites.DetectPiiDictionary)
	w.RegisterActivity(tablePiiDetectActivitites.DetectPiiLLM)
	w.RegisterActivity(tablePiiDetectActivitites.SaveTablePiiDetectReport)

	jobPiiDetectActivitites := piidetect_job_activities.New(
		jobclient,
		connclient,
		connectiondatabuilder,
		tmprlScheduleClient,
	)
	w.RegisterActivity(jobPiiDetectActivitites.GetPiiDetectJobDetails)
	w.RegisterActivity(jobPiiDetectActivitites.GetTablesToPiiScan)
	w.RegisterActivity(jobPiiDetectActivitites.SaveJobPiiDetectReport)
	w.RegisterActivity(jobPiiDetectActivitites.GetLastSuccessfulWorkflowId)
}
