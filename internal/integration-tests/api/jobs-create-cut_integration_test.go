package integrationtests_test

import (
	"context"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A job is written, then its schedule is created. A caller that gives up meanwhile — it closed
// its connection, or reached its time limit — leaves no job behind: the job is removed though
// the call it was created in has ended, and so is the schedule, which the orchestrator may
// have created all the same.
func (s *IntegrationTestSuite) Test_CreateJob_ACallerThatGivesUpLeavesNoJob() {
	t := s.T()
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	connclient := s.OSSUnauthenticatedLicensedClients.Connections()
	jobclient := s.OSSUnauthenticatedLicensedClients.Jobs()
	source := s.createPostgresConnection(connclient, accountId, "source", "test")
	destination := s.createPostgresConnection(connclient, accountId, "destination", "test2")

	s.Mocks.TemporalClientManager.
		On("DoesAccountHaveNamespace", mock.Anything, accountId, mock.Anything).
		Return(true, nil).Once()
	s.Mocks.TemporalClientManager.
		On("GetSyncJobTaskQueue", mock.Anything, accountId, mock.Anything).
		Return("sync-job", nil).Once()
	// The schedule is being created when the caller gives up.
	creating := make(chan struct{})
	s.Mocks.TemporalClientManager.
		On("CreateSchedule", mock.Anything, accountId, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(creating)
			<-args.Get(0).(context.Context).Done()
		}).
		Return("", context.Canceled).Once()
	scheduleRemoved := make(chan context.Context, 1)
	s.Mocks.TemporalClientManager.
		On("DeleteSchedule", mock.Anything, accountId, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { scheduleRemoved <- args.Get(0).(context.Context) }).
		Return(nil).Once()

	ctx, giveUp := context.WithCancel(s.ctx)
	defer giveUp()
	failed := make(chan error, 1)
	go func() {
		_, err := jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   "cut-short",
			Mappings:  []*mgmtv1alpha1.JobMapping{},
			Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
					ConnectionId: source.GetId(),
				}},
			}},
			Destinations: []*mgmtv1alpha1.CreateJobDestination{{
				ConnectionId: destination.GetId(),
				Options: &mgmtv1alpha1.JobDestinationOptions{
					Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
						PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
					},
				},
			}},
		}))
		failed <- err
	}()

	select {
	case <-creating:
	case err := <-failed:
		require.FailNow(t, "the job was not created as far as its schedule", "%v", err)
	case <-time.After(30 * time.Second):
		require.FailNow(t, "the job was not created as far as its schedule")
	}
	giveUp()
	require.Error(t, <-failed)

	select {
	case cleanup := <-scheduleRemoved:
		require.NoError(t, cleanup.Err(), "the schedule is removed in a call that has ended")
	case <-time.After(30 * time.Second):
		require.FailNow(t, "the schedule the orchestrator may have created was not removed")
	}
	require.Eventually(t, func() bool {
		jobs, err := jobclient.GetJobs(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: accountId}))
		return err == nil && len(jobs.Msg.GetJobs()) == 0
	}, 30*time.Second, 100*time.Millisecond, "a job without a schedule was left behind")
}
