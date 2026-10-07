package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const everyNight = "0 0 * * *"

// A job created with a schedule runs on its own: that is a feature of the license. A job created
// with a first run and no schedule is not scheduled.
func (s *IntegrationTestSuite) Test_CreateJob_AScheduleNeedsTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("create-schedule-feature")
	cron := everyNight
	scheduled := g.jobRequest("create-schedule-feature-job")
	scheduled.CronSchedule = &cron

	s.closeFeature(license.FeatureScheduling)
	// No schedule is expected of the orchestrator: a refusal that came after asking it for one
	// would fail the test.
	_, err := g.jobs.CreateJob(ctx, connect.NewRequest(scheduled))
	requireFeatureRefusal(t, err, license.FeatureScheduling)
	stored, err := g.jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: g.accountId}))
	requireNoErrResp(t, stored, err)
	require.Empty(t, stored.Msg.GetJobs(), "a refused job is not written")

	withARun := g.jobRequest("create-run-no-schedule-job")
	withARun.InitiateJobRun = true
	s.Mocks.TemporalClientManager.EXPECT().
		TriggerSchedule(mock.Anything, g.accountId, withARun.GetJobName(), mock.Anything, mock.Anything).
		Return(nil).Once()
	s.createJobUnderValidLicense(t, g.jobs, withARun)

	s.Mocks.ExpiringLicense.SetFeatures(license.FeatureScheduling)
	job := s.createJobUnderValidLicense(t, g.jobs, scheduled)
	require.Equal(t, cron, job.GetCronSchedule())
}

func (s *IntegrationTestSuite) Test_UpdateJobSchedule_NeedsTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("schedule-feature")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("schedule-feature-job")).GetId()
	cron := everyNight
	schedule := func() (*connect.Response[mgmtv1alpha1.UpdateJobScheduleResponse], error) {
		return g.jobs.UpdateJobSchedule(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobScheduleRequest{
			Id: jobId, CronSchedule: &cron,
		}))
	}

	s.closeFeature(license.FeatureScheduling)
	_, err := schedule()
	requireFeatureRefusal(t, err, license.FeatureScheduling)
	require.NotEqual(t, cron, s.storedJob(g, jobId).GetCronSchedule(), "a refused change writes nothing")

	s.Mocks.ExpiringLicense.SetFeatures(license.FeatureScheduling)
	s.Mocks.TemporalClientManager.EXPECT().
		UpdateSchedule(mock.Anything, g.accountId, jobId, mock.Anything, mock.Anything).
		Return(nil).Once()
	updated, err := schedule()
	requireNoErrResp(t, updated, err)
	require.Equal(t, cron, updated.Msg.GetJob().GetCronSchedule())
}

// A schedule can always be taken away, whatever the license includes.
func (s *IntegrationTestSuite) Test_UpdateJobSchedule_ClearingIsAllowedWithoutTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("schedule-clearing")
	cron := everyNight
	scheduled := g.jobRequest("schedule-clearing-job")
	scheduled.CronSchedule = &cron
	jobId := s.createJobUnderValidLicense(t, g.jobs, scheduled).GetId()

	s.Mocks.ExpiringLicense.SetFeatures()

	s.Mocks.TemporalClientManager.EXPECT().
		UpdateSchedule(mock.Anything, g.accountId, jobId, mock.Anything, mock.Anything).
		Return(nil).Once()
	cleared, err := g.jobs.UpdateJobSchedule(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobScheduleRequest{Id: jobId}))
	requireNoErrResp(t, cleared, err)
	require.NotEqual(t, cron, cleared.Msg.GetJob().GetCronSchedule())
}

// Resuming a job lets it run on its schedule again.
func (s *IntegrationTestSuite) Test_PauseJob_ResumingNeedsTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("resume-feature")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("resume-feature-job")).GetId()
	resume := func() (*connect.Response[mgmtv1alpha1.PauseJobResponse], error) {
		return g.jobs.PauseJob(ctx, connect.NewRequest(&mgmtv1alpha1.PauseJobRequest{Id: jobId, Pause: false}))
	}

	s.closeFeature(license.FeatureScheduling)
	// The orchestrator is not expected to be asked to resume anything.
	_, err := resume()
	requireFeatureRefusal(t, err, license.FeatureScheduling)

	s.Mocks.ExpiringLicense.SetFeatures(license.FeatureScheduling)
	s.Mocks.TemporalClientManager.EXPECT().
		UnpauseSchedule(mock.Anything, g.accountId, jobId, mock.Anything, mock.Anything).
		Return(nil).Once()
	resumed, err := resume()
	requireNoErrResp(t, resumed, err)
}

// A job can always be stopped from running on its own, whatever the license includes.
func (s *IntegrationTestSuite) Test_PauseJob_PausingIsAllowedWithoutTheFeature() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("pause-no-feature")
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.jobRequest("pause-no-feature-job")).GetId()

	s.Mocks.ExpiringLicense.SetFeatures()

	s.Mocks.TemporalClientManager.EXPECT().
		PauseSchedule(mock.Anything, g.accountId, jobId, mock.Anything, mock.Anything).
		Return(nil).Once()
	paused, err := g.jobs.PauseJob(ctx, connect.NewRequest(&mgmtv1alpha1.PauseJobRequest{Id: jobId, Pause: true}))
	requireNoErrResp(t, paused, err)
}

// featureHeavyJob gives the ground a job that subsets, runs JavaScript and has a schedule, made
// while the license includes all of it.
func (s *IntegrationTestSuite) featureHeavyJob(g *gateGround, name string) *mgmtv1alpha1.Job {
	cron := everyNight
	req := g.subsettingJobRequest(name)
	req.Mappings = []*mgmtv1alpha1.JobMapping{javascriptMapping("name")}
	req.CronSchedule = &cron
	return s.createJobUnderValidLicense(s.T(), g.jobs, req)
}

// A job is deleted whatever it uses and whatever the license includes.
func (s *IntegrationTestSuite) Test_DeleteJob_IsNeverGated() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("delete-no-feature")
	jobId := s.featureHeavyJob(g, "delete-no-feature-job").GetId()

	s.Mocks.ExpiringLicense.SetFeatures()

	s.Mocks.TemporalClientManager.EXPECT().
		DeleteSchedule(mock.Anything, g.accountId, jobId, mock.Anything).
		Return(nil).Once()
	deleted, err := g.jobs.DeleteJob(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobRequest{Id: jobId}))
	requireNoErrResp(t, deleted, err)

	stored, err := g.jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: g.accountId}))
	requireNoErrResp(t, stored, err)
	require.Empty(t, stored.Msg.GetJobs())
}

// A job stays readable, and its runs can be stopped, whatever it uses and whatever the license
// includes.
func (s *IntegrationTestSuite) Test_JobReadsAndStops_AreNeverGated() {
	t := s.T()
	ctx := s.ctx
	g := s.newGateGround("reads-no-feature")
	jobId := s.featureHeavyJob(g, "reads-no-feature-job").GetId()

	s.Mocks.ExpiringLicense.SetFeatures()

	job := s.storedJob(g, jobId)
	require.Equal(t, "transform_javascript_config", transformerOf(t, job, "name"))
	require.NotEmpty(t, whereClausesOf(job))

	stored, err := g.jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: g.accountId}))
	requireNoErrResp(t, stored, err)
	require.Len(t, stored.Msg.GetJobs(), 1)

	jobRunId := jobId + "-2026-10-07T10:00:00Z"
	s.MockTemporalForDescribeWorkflowExecution(g.accountId, jobId, jobRunId, "Workflow")
	run, err := g.jobs.GetJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunRequest{
		AccountId: g.accountId, JobRunId: jobRunId,
	}))
	requireNoErrResp(t, run, err)

	s.MockTemporalForDescribeWorkflowExecution(g.accountId, jobId, jobRunId, "Workflow")
	s.Mocks.TemporalClientManager.EXPECT().
		CancelWorkflow(mock.Anything, g.accountId, jobRunId, mock.Anything).
		Return(nil).Once()
	canceled, err := g.jobs.CancelJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CancelJobRunRequest{
		AccountId: g.accountId, JobRunId: jobRunId,
	}))
	requireNoErrResp(t, canceled, err)

	s.MockTemporalForDescribeWorkflowExecution(g.accountId, jobId, jobRunId, "Workflow")
	s.Mocks.TemporalClientManager.EXPECT().
		TerminateWorkflow(mock.Anything, g.accountId, jobRunId, mock.Anything).
		Return(nil).Once()
	terminated, err := g.jobs.TerminateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.TerminateJobRunRequest{
		AccountId: g.accountId, JobRunId: jobRunId,
	}))
	requireNoErrResp(t, terminated, err)
}
