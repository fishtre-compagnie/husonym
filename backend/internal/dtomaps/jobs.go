package dtomaps

import (
	"encoding/json"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	temporalclient "go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToJobDto(
	inputJob *db_queries.HusonymApiJob,
	inputDestConnections []db_queries.HusonymApiJobDestinationConnectionAssociation,
) (*mgmtv1alpha1.Job, error) {
	mappings := []*mgmtv1alpha1.JobMapping{}
	for _, mapping := range inputJob.Mappings {
		dto, err := mapping.ToDto()
		if err != nil {
			return nil, fmt.Errorf("unable to convert job mapping to dto: %w", err)
		}
		mappings = append(mappings, dto)
	}

	virtualForeignKeys := []*mgmtv1alpha1.VirtualForeignConstraint{}
	for _, vfk := range inputJob.VirtualForeignKeys {
		virtualForeignKeys = append(virtualForeignKeys, vfk.ToDto())
	}

	destinations := []*mgmtv1alpha1.JobDestination{}
	for i := range inputDestConnections {
		dest := inputDestConnections[i]
		destinations = append(destinations, toDestinationDto(&dest))
	}

	var workflowOptions *mgmtv1alpha1.WorkflowOptions
	if inputJob.WorkflowOptions != nil {
		workflowOptions = inputJob.WorkflowOptions.ToDto()
	}

	var syncOptions *mgmtv1alpha1.ActivityOptions
	if inputJob.SyncOptions != nil {
		syncOptions = inputJob.SyncOptions.ToDto()
	}

	jobTypeConfig := &mgmtv1alpha1.JobTypeConfig{}
	if inputJob.JobtypeConfig != nil && string(inputJob.JobtypeConfig) != "{}" &&
		string(inputJob.JobtypeConfig) != "null" {
		err := json.Unmarshal(inputJob.JobtypeConfig, jobTypeConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal job type config: %w", err)
		}
	}

	sourceOptions, err := inputJob.ConnectionOptions.ToDto()
	if err != nil {
		return nil, fmt.Errorf("unable to convert job source options to dto: %w", err)
	}

	return &mgmtv1alpha1.Job{
		Id:                 husonymdb.UUIDString(inputJob.ID),
		Name:               inputJob.Name,
		CreatedAt:          timestamppb.New(inputJob.CreatedAt.Time),
		UpdatedAt:          timestamppb.New(inputJob.UpdatedAt.Time),
		CreatedByUserId:    husonymdb.UUIDString(inputJob.CreatedByID),
		UpdatedByUserId:    husonymdb.UUIDString(inputJob.UpdatedByID),
		CronSchedule:       husonymdb.ToNullableString(inputJob.CronSchedule),
		Mappings:           mappings,
		VirtualForeignKeys: virtualForeignKeys,
		Source: &mgmtv1alpha1.JobSource{
			Options: sourceOptions,
		},
		Destinations:    destinations,
		AccountId:       husonymdb.UUIDString(inputJob.AccountID),
		SyncOptions:     syncOptions,
		WorkflowOptions: workflowOptions,
		JobType:         jobTypeConfig,
	}, nil
}

func toDestinationDto(
	input *db_queries.HusonymApiJobDestinationConnectionAssociation,
) *mgmtv1alpha1.JobDestination {
	return &mgmtv1alpha1.JobDestination{
		ConnectionId: husonymdb.UUIDString(input.ConnectionID),
		Options:      input.Options.ToDto(),
		Id:           husonymdb.UUIDString(input.ID),
	}
}

func ToJobStatus(inputSchedule *temporalclient.ScheduleDescription) mgmtv1alpha1.JobStatus {
	if inputSchedule.Schedule.State.Paused {
		return mgmtv1alpha1.JobStatus_JOB_STATUS_PAUSED
	}
	return mgmtv1alpha1.JobStatus_JOB_STATUS_ENABLED
}

func ToJobRecentRunsDto(
	inputSchedule *temporalclient.ScheduleDescription,
) []*mgmtv1alpha1.JobRecentRun {
	recentRuns := []*mgmtv1alpha1.JobRecentRun{}
	if inputSchedule == nil {
		return nil
	}

	for _, run := range inputSchedule.Info.RecentActions {
		recentRuns = append(recentRuns, &mgmtv1alpha1.JobRecentRun{
			StartTime: timestamppb.New(run.ActualTime),
			JobRunId:  run.StartWorkflowResult.WorkflowID,
		})
	}
	return recentRuns
}

func ToJobNextRunsDto(inputSchedule *temporalclient.ScheduleDescription) *mgmtv1alpha1.JobNextRuns {
	nextRunTimes := []*timestamppb.Timestamp{}
	if inputSchedule == nil {
		return nil
	}
	for _, t := range inputSchedule.Info.NextActionTimes {
		nextRunTimes = append(nextRunTimes, timestamppb.New(t))
	}
	return &mgmtv1alpha1.JobNextRuns{
		NextRunTimes: nextRunTimes,
	}
}
