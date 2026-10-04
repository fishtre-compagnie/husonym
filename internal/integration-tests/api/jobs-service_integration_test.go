package integrationtests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	piidetect_report "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (s *IntegrationTestSuite) Test_GetJobs_Empty() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Jobs().
		GetJobs(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
			AccountId: accountId,
		}))
	requireNoErrResp(s.T(), resp, err)
	jobs := resp.Msg.GetJobs()
	require.Empty(s.T(), jobs)
}

func (s *IntegrationTestSuite) Test_CreateJob_Ok() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	srcconn := s.createPostgresConnection(
		s.OSSUnauthenticatedLicensedClients.Connections(),
		accountId,
		"source",
		"test",
	)
	destconn := s.createPostgresConnection(
		s.OSSUnauthenticatedLicensedClients.Connections(),
		accountId,
		"dest",
		"test2",
	)

	s.MockTemporalForCreateJob("test-id")

	resp, err := s.OSSUnauthenticatedLicensedClients.Jobs().
		CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   "test",
			Mappings:  []*mgmtv1alpha1.JobMapping{},
			Source: &mgmtv1alpha1.JobSource{
				Options: &mgmtv1alpha1.JobSourceOptions{
					Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
						Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
							ConnectionId: srcconn.GetId(),
						},
					},
				},
			},
			Destinations: []*mgmtv1alpha1.CreateJobDestination{
				{
					ConnectionId: destconn.GetId(),
					Options: &mgmtv1alpha1.JobDestinationOptions{
						Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
							PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
						},
					}},
			},
			InitiateJobRun: false,
		}))
	requireNoErrResp(s.T(), resp, err)
	require.NotNil(s.T(), resp.Msg.GetJob())
}

// Jobs are the paid surface: without an active license the account can still read and
// wind down, but it cannot create or run anything.
func (s *IntegrationTestSuite) Test_JobService_RequiresLicense() {
	t := s.T()
	ctx := s.ctx
	users := s.OSSUnauthenticatedUnlicensedClients.Users()
	jobs := s.OSSUnauthenticatedUnlicensedClients.Jobs()
	accountId := s.createPersonalAccount(ctx, users)

	t.Run("CreateJob is denied", func(t *testing.T) {
		resp, err := jobs.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   "unlicensed",
			Mappings:  []*mgmtv1alpha1.JobMapping{},
		}))
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
	})

	t.Run("CreateJobRun is denied", func(t *testing.T) {
		resp, err := jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{
			JobId: uuid.NewString(),
		}))
		requireErrResp(t, resp, err)
		require.Error(t, err)
	})

	// Reading must keep working: a lapsed license is not a reason to lock an account out
	// of its own configuration.
	t.Run("GetJobs still allowed", func(t *testing.T) {
		resp, err := jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
			AccountId: accountId,
		}))
		requireNoErrResp(t, resp, err)
		require.Empty(t, resp.Msg.GetJobs())
	})
}

// Exercises the usage caps carried in the license against a real database, using the
// limited harness variant: 1 job, 2 connections, postgres only.
//
// The caps only bite on creation, so nothing here touches anything already running.
func (s *IntegrationTestSuite) Test_License_UsageLimits() {
	t := s.T()
	ctx := s.ctx
	conns := s.OSSUnauthenticatedLimitedClients.Connections()
	jobs := s.OSSUnauthenticatedLimitedClients.Jobs()
	accountId := s.createPersonalAccount(ctx, s.OSSUnauthenticatedLimitedClients.Users())

	createPg := func(name string) (*connect.Response[mgmtv1alpha1.CreateConnectionResponse], error) {
		return conns.CreateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
			AccountId: accountId,
			Name:      name,
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
					PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
						ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Url{
							Url: "postgres://user:pass@localhost:5432/" + name,
						},
					},
				},
			},
		}))
	}

	// A type outside the allowlist is refused regardless of how many connections exist,
	// and the check runs before the count so the error names the real reason.
	t.Run("a connection type outside the allowlist is refused", func(t *testing.T) {
		resp, err := conns.CreateConnection(ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
			AccountId: accountId,
			Name:      "mysql-not-allowed",
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{
					MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
						ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Url{
							Url: "mysql://user:pass@localhost:3306/db",
						},
					},
				},
			},
		}))
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
		require.Contains(t, err.Error(), "mysql")
	})

	var srcId, destId string
	t.Run("connections up to the cap are allowed", func(t *testing.T) {
		src, err := createPg("source")
		requireNoErrResp(t, src, err)
		srcId = src.Msg.GetConnection().GetId()

		dest, err := createPg("dest")
		requireNoErrResp(t, dest, err)
		destId = dest.Msg.GetConnection().GetId()
	})

	t.Run("one connection past the cap is refused", func(t *testing.T) {
		resp, err := createPg("third")
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
		// The message must name the limit and the count, so a customer knows what to ask
		// for rather than just being told no.
		require.Contains(t, err.Error(), "2 connection(s)")
	})

	createJob := func(name string) (*connect.Response[mgmtv1alpha1.CreateJobResponse], error) {
		return jobs.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   name,
			Mappings:  []*mgmtv1alpha1.JobMapping{},
			Source: &mgmtv1alpha1.JobSource{
				Options: &mgmtv1alpha1.JobSourceOptions{
					Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
						Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{ConnectionId: srcId},
					},
				},
			},
			Destinations: []*mgmtv1alpha1.CreateJobDestination{
				{
					ConnectionId: destId,
					Options: &mgmtv1alpha1.JobDestinationOptions{
						Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
							PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
						},
					},
				},
			},
		}))
	}

	t.Run("the first job is allowed", func(t *testing.T) {
		s.MockTemporalForCreateJob("limited-job")
		resp, err := createJob("first")
		requireNoErrResp(t, resp, err)
	})

	t.Run("a second job is refused", func(t *testing.T) {
		resp, err := createJob("second")
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
		require.Contains(t, err.Error(), "1 job(s)")
	})

	// Hitting a cap must not affect what already exists.
	t.Run("existing resources stay readable at the cap", func(t *testing.T) {
		resp, err := jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
			AccountId: accountId,
		}))
		requireNoErrResp(t, resp, err)
		require.Len(t, resp.Msg.GetJobs(), 1)
	})
}

func (s *IntegrationTestSuite) Test_JobService_JobHooks() {
	t := s.T()
	ctx := s.ctx

	t.Run("OSS-authenticated-licensed", func(t *testing.T) {
		client := s.OSSAuthenticatedLicensedClients.Jobs(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		s.setUser(
			ctx,
			s.OSSAuthenticatedLicensedClients.Users(
				integrationtests_test.WithUserId(testAuthUserId),
			),
		)
		accountId := s.createPersonalAccount(
			ctx,
			s.OSSAuthenticatedLicensedClients.Users(
				integrationtests_test.WithUserId(testAuthUserId),
			),
		)

		srcconn := s.createPostgresConnection(
			s.OSSAuthenticatedLicensedClients.Connections(
				integrationtests_test.WithUserId(testAuthUserId),
			),
			accountId,
			"source",
			"test",
		)
		destconn := s.createPostgresConnection(
			s.OSSAuthenticatedLicensedClients.Connections(
				integrationtests_test.WithUserId(testAuthUserId),
			),
			accountId,
			"dest",
			"test2",
		)

		s.MockTemporalForCreateJob("test-id")
		jobResp, err := client.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			JobName:   "oss-testjob-1",
			AccountId: accountId,
			Mappings:  []*mgmtv1alpha1.JobMapping{},
			Source: &mgmtv1alpha1.JobSource{
				Options: &mgmtv1alpha1.JobSourceOptions{
					Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
						Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
							ConnectionId: srcconn.GetId(),
						},
					},
				},
			},
			Destinations: []*mgmtv1alpha1.CreateJobDestination{
				{
					ConnectionId: destconn.GetId(),
					Options: &mgmtv1alpha1.JobDestinationOptions{
						Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
							PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
						},
					},
				},
			},
		}))
		requireNoErrResp(t, jobResp, err)

		t.Run("GetJobHooks", func(t *testing.T) {
			createdHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"getjobhooks-1",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
				},
			)

			t.Run("ok", func(t *testing.T) {
				resp, err := client.GetJobHooks(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{
						JobId: createdHook.GetJobId(),
					}),
				)
				requireNoErrResp(t, resp, err)
				hooks := resp.Msg.GetHooks()
				require.NotEmpty(t, hooks)
			})
		})

		t.Run("GetJobHook", func(t *testing.T) {
			createdHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"getjobhook-1",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
				},
			)

			t.Run("ok", func(t *testing.T) {
				resp, err := client.GetJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{
						Id: createdHook.GetId(),
					}),
				)
				requireNoErrResp(t, resp, err)
				hook := resp.Msg.GetHook()
				require.NotNil(t, hook)
				require.Equal(t, createdHook.GetId(), hook.GetId())
			})
			t.Run("not_found", func(t *testing.T) {
				resp, err := client.GetJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{
						Id: uuid.NewString(),
					}),
				)
				requireErrResp(t, resp, err)
				requireConnectError(t, err, connect.CodeNotFound)
			})
		})

		t.Run("CreateJobHook", func(t *testing.T) {
			t.Run("ok", func(t *testing.T) {
				resp, err := client.CreateJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
						JobId: jobResp.Msg.GetJob().GetId(),
						Hook: &mgmtv1alpha1.NewJobHook{
							Name:        "createjobhook-1",
							Description: "createjobhook ok",
							Enabled:     true,
							Priority:    0,
							Config: &mgmtv1alpha1.JobHookConfig{
								Config: &mgmtv1alpha1.JobHookConfig_Sql{
									Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
										Query:        "foo",
										ConnectionId: srcconn.GetId(),
										Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
											Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
										},
									},
								},
							},
						},
					}),
				)
				requireNoErrResp(t, resp, err)
			})

			t.Run("job_not_found", func(t *testing.T) {
				resp, err := client.CreateJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
						JobId: uuid.NewString(), // job id does not exist
						Hook: &mgmtv1alpha1.NewJobHook{
							Name:        "createjobhook-2",
							Description: "createjobhook job not found",
							Enabled:     true,
							Priority:    0,
							Config: &mgmtv1alpha1.JobHookConfig{
								Config: &mgmtv1alpha1.JobHookConfig_Sql{
									Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
										Query:        "foo",
										ConnectionId: srcconn.GetId(),
										Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
											Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
										},
									},
								},
							},
						},
					}),
				)
				requireErrResp(t, resp, err)
				requireConnectError(t, err, connect.CodeNotFound)
			})
			t.Run("connection_not_in_job", func(t *testing.T) {
				resp, err := client.CreateJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
						JobId: jobResp.Msg.GetJob().GetId(),
						Hook: &mgmtv1alpha1.NewJobHook{
							Name:        "createjobhook-3",
							Description: "createjobhook connection not in job",
							Enabled:     true,
							Priority:    0,
							Config: &mgmtv1alpha1.JobHookConfig{
								Config: &mgmtv1alpha1.JobHookConfig_Sql{
									Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
										Query:        "foo",
										ConnectionId: uuid.NewString(), // job does not have this connection id
										Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
											Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
										},
									},
								},
							},
						},
					}),
				)
				requireErrResp(t, resp, err)
				requireConnectError(t, err, connect.CodeInvalidArgument)
			})
			t.Run("invalid_timing", func(t *testing.T) {
				resp, err := client.CreateJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
						JobId: jobResp.Msg.GetJob().GetId(),
						Hook: &mgmtv1alpha1.NewJobHook{
							Name:        "createjobhook-4",
							Description: "createjobhook bad timing",
							Enabled:     true,
							Priority:    0,
							Config: &mgmtv1alpha1.JobHookConfig{
								Config: &mgmtv1alpha1.JobHookConfig_Sql{
									Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
										Query:        "foo",
										ConnectionId: srcconn.GetId(),
										Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
											Timing: nil,
										},
									},
								},
							},
						},
					}),
				)
				requireErrResp(t, resp, err)
				requireConnectError(t, err, connect.CodeInvalidArgument)
			})
		})

		t.Run("DeleteJobHook", func(t *testing.T) {
			t.Run("ok", func(t *testing.T) {
				createdHook := s.createSqlJobHook(
					ctx,
					t,
					client,
					"deletejobhook-1",
					jobResp.Msg.GetJob().GetId(),
					srcconn.GetId(),
					true,
					&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
						Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
					},
				)
				resp, err := client.DeleteJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: createdHook.GetId()}),
				)
				requireNoErrResp(t, resp, err)

				getResp, err := client.GetJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: createdHook.GetId()}),
				)
				requireErrResp(t, getResp, err)
				requireConnectError(t, err, connect.CodeNotFound)
			})
			t.Run("non_existent", func(t *testing.T) {
				resp, err := client.DeleteJobHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: uuid.NewString()}),
				)
				requireNoErrResp(t, resp, err)
			})
		})

		t.Run("IsJobHookNameAvailable", func(t *testing.T) {
			t.Run("yes", func(t *testing.T) {
				resp, err := client.IsJobHookNameAvailable(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{
						JobId: jobResp.Msg.GetJob().GetId(),
						Name:  "isjobhooknameavailable-1",
					}),
				)
				requireNoErrResp(t, resp, err)
				require.True(t, resp.Msg.GetIsAvailable())
			})
			t.Run("no", func(t *testing.T) {
				createdHook := s.createSqlJobHook(
					ctx,
					t,
					client,
					"isjobhooknameavail-2",
					jobResp.Msg.GetJob().GetId(),
					srcconn.GetId(),
					true,
					&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
						Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
					},
				)

				resp, err := client.IsJobHookNameAvailable(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{
						JobId: jobResp.Msg.GetJob().GetId(),
						Name:  createdHook.GetName(),
					}),
				)
				requireNoErrResp(t, resp, err)
				require.False(t, resp.Msg.GetIsAvailable())
			})
		})

		t.Run("SetJobHookEnabled", func(t *testing.T) {
			createdHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"setjobhookenabled-1",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
				},
			)
			require.True(t, createdHook.GetEnabled())
			resp, err := client.SetJobHookEnabled(
				ctx,
				connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
					Id:      createdHook.GetId(),
					Enabled: false,
				}),
			)
			requireNoErrResp(t, resp, err)
			require.False(t, resp.Msg.GetHook().GetEnabled())
			resp, err = client.SetJobHookEnabled(
				ctx,
				connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
					Id:      createdHook.GetId(),
					Enabled: true,
				}),
			)
			requireNoErrResp(t, resp, err)
			require.True(t, resp.Msg.GetHook().GetEnabled())
		})

		t.Run("UpdateJobHook", func(t *testing.T) {
			createdHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"updatejobhook-1",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
				},
			)
			resp, err := client.UpdateJobHook(
				ctx,
				connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
					Id:          createdHook.GetId(),
					Name:        fmt.Sprintf("%s-updated", createdHook.GetName()),
					Description: fmt.Sprintf("%s-updated", createdHook.GetDescription()),
					Enabled:     !createdHook.GetEnabled(),
					Priority:    createdHook.GetPriority() + 1,
					Config: &mgmtv1alpha1.JobHookConfig{
						Config: &mgmtv1alpha1.JobHookConfig_Sql{
							Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
								Query:        "foobar",
								ConnectionId: destconn.GetId(),
								Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
									Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
								},
							},
						},
					},
				}),
			)
			requireNoErrResp(t, resp, err)
			updatedHook := resp.Msg.GetHook()
			require.Equal(
				t,
				fmt.Sprintf("%s-updated", createdHook.GetName()),
				updatedHook.GetName(),
			)
			require.Equal(
				t,
				fmt.Sprintf("%s-updated", createdHook.GetDescription()),
				updatedHook.GetDescription(),
			)
			require.Equal(t, !createdHook.GetEnabled(), updatedHook.GetEnabled())
			require.Equal(t, createdHook.GetPriority()+1, updatedHook.GetPriority())
			sqlhook := updatedHook.GetConfig().GetSql()
			require.NotNil(t, sqlhook)
			require.Equal(t, "foobar", sqlhook.GetQuery())
			require.Equal(t, destconn.GetId(), sqlhook.GetConnectionId())
			require.NotNil(t, sqlhook.GetTiming().GetPostSync())
		})

		t.Run("GetActiveJobHooksByTiming", func(t *testing.T) {
			createdPreSyncHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"getactivejobhooksbytiming-pre",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
				},
			)
			createdPostSyncHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"getactivejobhooksbytiming-post",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				true,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
				},
			)
			disabledHook := s.createSqlJobHook(
				ctx,
				t,
				client,
				"getactivejobhooksbytiming-disabled",
				jobResp.Msg.GetJob().GetId(),
				srcconn.GetId(),
				false,
				&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
					Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
				},
			)
			t.Run("unspecified", func(t *testing.T) {
				resp, err := client.GetActiveJobHooksByTiming(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
						JobId:  jobResp.Msg.GetJob().GetId(),
						Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_UNSPECIFIED,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.NotEmpty(t, resp.Msg.GetHooks())
				hasDisabledHook := false
				for _, hook := range resp.Msg.GetHooks() {
					if hook.GetId() == disabledHook.GetId() {
						hasDisabledHook = true
						break
					}
				}
				require.False(
					t,
					hasDisabledHook,
					"GetActiveHooksByTiming should never return disabled hooks!",
				)
			})

			t.Run("presync", func(t *testing.T) {
				resp, err := client.GetActiveJobHooksByTiming(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
						JobId:  jobResp.Msg.GetJob().GetId(),
						Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.NotEmpty(t, resp.Msg.GetHooks())
				hasCreatedHook := false
				for _, hook := range resp.Msg.GetHooks() {
					if hook.GetId() == createdPreSyncHook.GetId() {
						hasCreatedHook = true
						break
					}
				}
				require.True(t, hasCreatedHook)
			})

			t.Run("postsync", func(t *testing.T) {
				resp, err := client.GetActiveJobHooksByTiming(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
						JobId:  jobResp.Msg.GetJob().GetId(),
						Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.NotEmpty(t, resp.Msg.GetHooks())
				hasCreatedHook := false
				for _, hook := range resp.Msg.GetHooks() {
					if hook.GetId() == createdPostSyncHook.GetId() {
						hasCreatedHook = true
						break
					}
				}
				require.True(t, hasCreatedHook)
			})
		})
	})
}

func (s *IntegrationTestSuite) createSqlJobHook(
	ctx context.Context,
	t testing.TB,
	jobclient mgmtv1alpha1connect.JobServiceClient,
	name string,
	jobId string,
	connectionId string,
	enabled bool,
	timing *mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing,
) *mgmtv1alpha1.JobHook {
	createResp, err := jobclient.CreateJobHook(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
			JobId: jobId,
			Hook: &mgmtv1alpha1.NewJobHook{
				Name:        name,
				Description: "sql job hook test",
				Enabled:     enabled,
				Priority:    100,
				Config: &mgmtv1alpha1.JobHookConfig{
					Config: &mgmtv1alpha1.JobHookConfig_Sql{
						Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
							Query:        "truncate table public.users;",
							ConnectionId: connectionId,
							Timing:       timing,
						},
					},
				},
			},
		}),
	)
	requireNoErrResp(t, createResp, err)
	return createResp.Msg.GetHook()
}

func (s *IntegrationTestSuite) createPostgresConnection(
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	accountId string,
	name string,
	pgurl string,
) *mgmtv1alpha1.Connection {
	resp, err := connclient.CreateConnection(
		s.ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
			AccountId: accountId,
			Name:      name,
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
					PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
						ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Url{
							Url: pgurl,
						},
					},
				},
			},
		}),
	)
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetConnection()
}

func (s *IntegrationTestSuite) Test_ValidateSchema() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	destconn := s.createPostgresConnection(
		s.OSSUnauthenticatedLicensedClients.Connections(),
		accountId,
		"dest",
		s.Pgcontainer.URL,
	)

	s.T().Run("MissingTables", func(t *testing.T) {
		Mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "addresses",
				Column: "order_id",
				Transformer: &mgmtv1alpha1.JobMappingTransformer{
					Config: &mgmtv1alpha1.TransformerConfig{
						Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
							PassthroughConfig: &mgmtv1alpha1.Passthrough{},
						},
					},
				},
			},
			{
				Schema: "public",
				Table:  "customers",
				Column: "id",
				Transformer: &mgmtv1alpha1.JobMappingTransformer{
					Config: &mgmtv1alpha1.TransformerConfig{
						Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
							PassthroughConfig: &mgmtv1alpha1.Passthrough{},
						},
					},
				},
			},
		}

		resp, err := s.OSSUnauthenticatedLicensedClients.Jobs().
			ValidateSchema(s.ctx, connect.NewRequest(&mgmtv1alpha1.ValidateSchemaRequest{
				ConnectionId: destconn.GetId(),
				Mappings:     Mappings,
			}))
		requireNoErrResp(t, resp, err)
		require.Len(t, resp.Msg.MissingTables, 2)
	})

	s.T().Run("Ok", func(t *testing.T) {
		Mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "husonym_api",
				Table:  "users",
				Column: "id",
				Transformer: &mgmtv1alpha1.JobMappingTransformer{
					Config: &mgmtv1alpha1.TransformerConfig{
						Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
							PassthroughConfig: &mgmtv1alpha1.Passthrough{},
						},
					},
				},
			},
		}

		resp, err := s.OSSUnauthenticatedLicensedClients.Jobs().
			ValidateSchema(s.ctx, connect.NewRequest(&mgmtv1alpha1.ValidateSchemaRequest{
				ConnectionId: destconn.GetId(),
				Mappings:     Mappings,
			}))
		requireNoErrResp(t, resp, err)
		require.Len(t, resp.Msg.MissingTables, 0)
	})
}

func (s *IntegrationTestSuite) Test_GetPiiDetectionReport() {
	userclient := s.OSSUnauthenticatedLicensedClients.Users()
	s.setUser(s.ctx, userclient)
	accountId := s.createPersonalAccount(s.ctx, userclient)

	connclient := s.OSSUnauthenticatedLicensedClients.Connections()
	jobclient := s.OSSUnauthenticatedLicensedClients.Jobs()

	srcconn := s.createPostgresConnection(
		connclient,
		accountId,
		"pii-detect-src",
		"postgres://postgres:postgres@localhost:5432/postgres",
	)

	s.MockTemporalForCreateJob("test-id")
	jobResp, err := jobclient.CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "pii-detection-test" + uuid.NewString(),
		Source: &mgmtv1alpha1.JobSource{
			Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
					Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
						ConnectionId: srcconn.GetId(),
					},
				},
			},
		},
		JobType: &mgmtv1alpha1.JobTypeConfig{
			JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
				PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
					DataSampling: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
						IsEnabled: true,
					},
					TableScanFilter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
						Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll{},
					},
				},
			},
		},
	}))
	requireNoErrResp(s.T(), jobResp, err)

	jobId := jobResp.Msg.GetJob().GetId()

	s.T().Run("found", func(t *testing.T) {
		jobRunId := fmt.Sprintf("%s-%s", jobId, time.Now().Format(time.RFC3339))

		report := piidetect_report.TableReport{
			TableSchema: "public",
			TableName:   "users",
			ColumnReports: []piidetect_report.ColumnReport{
				{
					ColumnName: "age",
					Report: piidetect_report.Combined{
						Regex: &piidetect_report.RuleFinding{
							Category: piidetect_report.Personal,
						},
					},
				},
			},
		}
		reportBytes, err := json.Marshal(report)
		require.NoError(s.T(), err)

		setResp, err := jobclient.SetRunContext(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
				Id: &mgmtv1alpha1.RunContextKey{
					AccountId: accountId,
					JobRunId:  jobRunId,
					ExternalId: piidetect_report.TableReportExternalId(
						"public",
						"users",
					),
				},
				Value: reportBytes,
			}),
		)
		requireNoErrResp(t, setResp, err)

		s.MockTemporalForDescribeWorkflowExecution(accountId, jobId, jobRunId, "JobPiiDetect")

		getResp, err := jobclient.GetPiiDetectionReport(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.GetPiiDetectionReportRequest{
				JobRunId:  jobRunId,
				AccountId: accountId,
			}),
		)
		requireNoErrResp(t, getResp, err)
		require.NotNil(t, getResp.Msg.GetReport())
		tables := getResp.Msg.GetReport().GetTables()
		require.Len(t, tables, 1)

		table := tables[0]
		require.Equal(t, "public", table.Schema)
		require.Equal(t, "users", table.Table)
		require.Len(t, table.Columns, 1)

		columnReport := table.Columns[0]
		require.Equal(t, "age", columnReport.Column)
		require.Equal(
			t,
			string(piidetect_report.Personal),
			columnReport.RegexReport.Category,
		)
	})

	// store writes a value under a key of a run, as a worker does.
	store := func(t *testing.T, jobRunId, externalId, value string) {
		t.Helper()
		setResp, err := jobclient.SetRunContext(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
				Id:    &mgmtv1alpha1.RunContextKey{AccountId: accountId, JobRunId: jobRunId, ExternalId: externalId},
				Value: []byte(value),
			}),
		)
		requireNoErrResp(t, setResp, err)
	}
	read := func(t *testing.T, jobRunId string) []*mgmtv1alpha1.PiiDetectionReport_TableReport {
		t.Helper()
		s.MockTemporalForDescribeWorkflowExecution(accountId, jobId, jobRunId, "JobPiiDetect")
		getResp, err := jobclient.GetPiiDetectionReport(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.GetPiiDetectionReportRequest{JobRunId: jobRunId, AccountId: accountId}),
		)
		requireNoErrResp(t, getResp, err)
		return getResp.Msg.GetReport().GetTables()
	}
	tableKey := func(jobRunId, table string) string {
		return fmt.Sprintf(
			`{"jobRunId":%q,"externalId":"public.%s--table-pii-report","accountId":%q}`, jobRunId, table, accountId,
		)
	}

	// A table report and an index that hold only the members every report has, as stored
	// rows may: they are read as they always were.
	s.T().Run("a report stored without the optional members", func(t *testing.T) {
		jobRunId := fmt.Sprintf("%s-%s-plain", jobId, time.Now().Format(time.RFC3339))
		store(t, jobRunId, "public.users--table-pii-report", `{
			"table_schema": "public", "table_name": "users",
			"column_reports": [
				{"column_name": "email", "report": {"regex": {"category": "contact"}, "llm": {"category": "contact", "confidence": 0.95}}},
				{"column_name": "ref", "report": {"regex": null, "llm": {"category": "national_id", "confidence": 0.7}}}
			],
			"scanned_columns": ["id", "email", "ref"]
		}`)

		// While the run has no index, its table reports are found by their suffix.
		tables := read(t, jobRunId)
		require.Len(t, tables, 1)

		store(t, jobRunId, jobId+"--job-pii-report", `{"successfulTableReports":[{
			"tableSchema": "public", "tableName": "users",
			"reportKey": `+tableKey(jobRunId, "users")+`,
			"scanFingerprint": "0a1b2c"
		}]}`)
		tables = read(t, jobRunId)
		require.Len(t, tables, 1)
		require.Equal(t, "public", tables[0].GetSchema())
		require.Equal(t, "users", tables[0].GetTable())
		require.Len(t, tables[0].GetColumns(), 2)
		email, ref := tables[0].GetColumns()[0], tables[0].GetColumns()[1]
		require.Equal(t, "email", email.GetColumn())
		require.Equal(t, "contact", email.GetRegexReport().GetCategory())
		require.Equal(t, "contact", email.GetLlmReport().GetCategory())
		require.InDelta(t, 0.95, email.GetLlmReport().GetConfidence(), 1e-6)
		require.Equal(t, "ref", ref.GetColumn())
		require.Nil(t, ref.GetRegexReport())
		require.Equal(t, "national_id", ref.GetLlmReport().GetCategory())
	})

	// The members a worker may add to a report and to an index do not change what is read.
	s.T().Run("a report stored with every member", func(t *testing.T) {
		jobRunId := fmt.Sprintf("%s-%s-full", jobId, time.Now().Format(time.RFC3339))
		table, err := json.Marshal(&piidetect_report.TableReport{
			TableSchema: "public",
			TableName:   "users",
			ColumnReports: []piidetect_report.ColumnReport{{
				ColumnName: "iban",
				Report: piidetect_report.Combined{
					Regex: &piidetect_report.RuleFinding{Category: piidetect_report.Financial, Evidence: "values:iban 0.97"},
				},
			}},
			ScannedColumns: []string{"id", "iban"},
			Scan: &piidetect_report.Scan{
				SampledRows: 200, Input: piidetect_report.InputProfiles, Model: "local-model",
				ModelStatus: piidetect_report.ModelPartial, Unanswered: []string{"id"},
				BelowThreshold: []piidetect_report.Dismissed{
					{ColumnName: "id", Category: piidetect_report.Personal, Confidence: 0.2},
				},
			},
		})
		require.NoError(t, err)
		store(t, jobRunId, piidetect_report.TableReportExternalId("public", "users"), string(table))
		store(t, jobRunId, piidetect_report.JobReportExternalId(jobId), `{
			"successfulTableReports": [{
				"tableSchema": "public", "tableName": "users",
				"reportKey": `+tableKey(jobRunId, "users")+`,
				"scanFingerprint": "0a1b2c", "incomplete": true
			}],
			"failedTables": [{"tableSchema": "public", "tableName": "orders", "reason": "the columns cannot be read"}]
		}`)

		tables := read(t, jobRunId)
		require.Len(t, tables, 1)
		require.Equal(t, "users", tables[0].GetTable())
		require.Len(t, tables[0].GetColumns(), 1)
		require.Equal(t, "iban", tables[0].GetColumns()[0].GetColumn())
		require.Equal(t, "financial", tables[0].GetColumns()[0].GetRegexReport().GetCategory())
		require.Nil(t, tables[0].GetColumns()[0].GetLlmReport())
	})

	s.T().Run("empty", func(t *testing.T) {
		jobRunId := fmt.Sprintf("%s-%s-empty", jobId, time.Now().Format(time.RFC3339))
		s.MockTemporalForDescribeWorkflowExecution(accountId, jobId, jobRunId, "JobPiiDetect")

		getResp, err := jobclient.GetPiiDetectionReport(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.GetPiiDetectionReportRequest{
				JobRunId:  jobRunId,
				AccountId: accountId,
			}),
		)
		requireNoErrResp(t, getResp, err)
		require.Empty(t, getResp.Msg.GetReport().GetTables())
	})
}
