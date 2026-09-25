package datasync_workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	destinationtriggers_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/destination-triggers"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	jobhooks_by_timing_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/jobhooks-by-timing"
	posttablesync_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/post-table-sync"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	referentialintegrity_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/referential-integrity"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	syncrediscleanup_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-redis-clean-up"
	schemainit_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/schemainit/workflow"
	workflow_shared "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared"
	sync_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/activities/sync"
	tablesync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/workflow"
	"github.com/spf13/viper"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"

	"go.temporal.io/sdk/workflow"
)

type WorkflowRequest struct {
	JobId string
}

type WorkflowResponse struct{}

type Workflow struct {
	eelicense license.EEInterface
}

func New(eelicense license.EEInterface) *Workflow {
	return &Workflow{
		eelicense: eelicense,
	}
}

var (
	errInvalidAccountStatusError = errors.New("exiting workflow due to invalid account status")
)

func withGenerateBenthosConfigsActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
		HeartbeatTimeout: 1 * time.Minute,
	})
}

func withCheckAccountStatusActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
		HeartbeatTimeout: 1 * time.Minute,
	})
}

func withJobHookTimingActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
		HeartbeatTimeout: 1 * time.Minute,
	})
}

func (w *Workflow) Workflow(ctx workflow.Context, req *WorkflowRequest) (*WorkflowResponse, error) {
	logger := workflow.GetLogger(ctx)
	getAccountId := func() (string, error) {
		actOptResp, err := retrieveActivityOptions(ctx, req.JobId, logger)
		if err != nil {
			return "", err
		}
		return actOptResp.AccountId, nil
	}
	runWorkflow := func(ctx workflow.Context, logger log.Logger) (*WorkflowResponse, error) {
		return executeWorkflow(ctx, req)
	}
	wfinfo := workflow.GetInfo(ctx)
	return workflow_shared.HandleWorkflowEventLifecycle(
		ctx,
		w.eelicense,
		req.JobId,
		wfinfo.WorkflowExecution.ID,
		logger,
		getAccountId,
		runWorkflow,
	)
}

func executeWorkflow(wfctx workflow.Context, req *WorkflowRequest) (*WorkflowResponse, error) {
	ctx, cancelHandler := workflow.WithCancel(wfctx)
	logger := workflow.GetLogger(ctx)

	logger = log.With(logger, "jobId", req.JobId)
	logger.Info("data sync workflow starting")

	actOptResp, err := retrieveActivityOptions(ctx, req.JobId, logger)
	if err != nil {
		return nil, err
	}
	logger = log.With(
		logger,
		"accountId", actOptResp.AccountId,
	)

	if actOptResp.RequestedRecordCount != nil && *actOptResp.RequestedRecordCount > 0 {
		logger.Info(fmt.Sprintf("requested record count of %d", *actOptResp.RequestedRecordCount))
	}
	var initialCheckAccountStatusResponse *accountstatus_activity.CheckAccountStatusResponse
	var a *accountstatus_activity.Activity
	err = workflow.ExecuteActivity(
		withCheckAccountStatusActivityOptions(ctx),
		a.CheckAccountStatus,
		&accountstatus_activity.CheckAccountStatusRequest{
			AccountId:            actOptResp.AccountId,
			RequestedRecordCount: actOptResp.RequestedRecordCount,
		},
	).
		Get(ctx, &initialCheckAccountStatusResponse)
	if err != nil {
		logger.Error("encountered error while checking account status", "error", err)
		cancelHandler()
		return nil, fmt.Errorf(
			"unable to continue workflow due to error when checking account status: %w",
			err,
		)
	}
	if !initialCheckAccountStatusResponse.IsValid {
		logger.Warn("account is no longer is valid state")
		cancelHandler()
		reason := "no reason provided"
		if initialCheckAccountStatusResponse.Reason != nil {
			reason = *initialCheckAccountStatusResponse.Reason
		}
		return nil, fmt.Errorf(
			"halting job run due to account in invalid state. Reason: %q: %w",
			reason,
			errInvalidAccountStatusError,
		)
	}

	// Version 2 checks the privileges once the configs are generated, on the very tables
	// and columns the run writes; version 1 checked them before, from the job mappings.
	// Version 3 runs the pre-flight check there instead: the privileges and what the plan
	// tells of the run, kept as the report of the run.
	privilegesVersion := workflow.GetVersion(ctx, "run-privilege-check", workflow.DefaultVersion, 3)
	if privilegesVersion == 1 {
		if err := runPrivilegeCheck(ctx, logger, req.JobId, nil); err != nil {
			return nil, err
		}
	}

	info := workflow.GetInfo(ctx)
	var bcResp *genbenthosconfigs_activity.GenerateBenthosConfigsResponse
	logger.Info("scheduling GenerateBenthosConfigs for execution.")
	var genbenthosactivity *genbenthosconfigs_activity.Activity
	err = workflow.ExecuteActivity(
		withGenerateBenthosConfigsActivityOptions(ctx),
		genbenthosactivity.GenerateBenthosConfigs,
		&genbenthosconfigs_activity.GenerateBenthosConfigsRequest{
			JobId:    req.JobId,
			JobRunId: info.WorkflowExecution.ID,
		}).
		Get(ctx, &bcResp)
	if err != nil {
		return nil, err
	}

	if len(bcResp.BenthosConfigs) == 0 {
		logger.Info("found 0 benthos configs, ending workflow.")
		return &WorkflowResponse{}, nil
	}

	// Generating the configs reads metadata only: nothing is read from the tables nor
	// written yet, and the hooks, the schema init and the emptying of the destination come
	// after the check.
	switch {
	case privilegesVersion == 2:
		if err := runPrivilegeCheck(ctx, logger, req.JobId, bcResp.BenthosConfigs); err != nil {
			return nil, err
		}
	case privilegesVersion >= 3:
		if err := runPreflightCheck(ctx, logger, req.JobId, info.WorkflowExecution.ID, bcResp); err != nil {
			return nil, err
		}
	}

	err = execRunJobHooksByTiming(
		ctx,
		&jobhooks_by_timing_activity.RunJobHooksByTimingRequest{
			JobId:  req.JobId,
			Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
		},
		logger,
	)
	if err != nil {
		return nil, err
	}

	err = runSchemaInitWorkflowByDestination(
		ctx,
		logger,
		actOptResp.AccountId,
		req.JobId,
		info.WorkflowExecution.ID,
		actOptResp.Destinations,
	)
	if err != nil {
		return nil, err
	}

	// Version 2 puts the triggers back on every way out of the run, not only on success.
	triggersVersion := workflow.GetVersion(ctx, "destination-triggers", workflow.DefaultVersion, 2)
	triggersRestored := false
	if triggersVersion >= 2 {
		defer func() {
			if triggersRestored {
				return
			}
			// The run is failing or canceled: its context may be done already.
			detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
			if err := restoreDestinationTriggers(detachedCtx, logger, triggersVersion, req.JobId, actOptResp.AccountId); err != nil {
				logger.Error("destination triggers could not be restored on the way out of the run: "+
					"the next run of the job will put them back", "error", err)
			}
		}()
	}
	err = suspendDestinationTriggers(ctx, logger, triggersVersion, req.JobId, actOptResp.AccountId, bcResp.BenthosConfigs)
	if err != nil {
		return nil, err
	}

	// spawn account status checker in loop
	stopChan := workflow.NewNamedChannel(ctx, "account-status")
	if initialCheckAccountStatusResponse.ShouldPoll {
		accountStatusTimerDuration := getAccountStatusTimerDuration()
		workflow.GoNamed(
			ctx,
			"account-status-check",
			func(ctx workflow.Context) {
				shouldStop := false
				for {
					selector := workflow.NewNamedSelector(ctx, "account-status-select")
					timer := workflow.NewTimer(ctx, accountStatusTimerDuration)
					selector.AddFuture(timer, func(f workflow.Future) {
						err := f.Get(ctx, nil)
						if err != nil {
							logger.Error("time receive failed", "error", err)
							return
						}

						var result *accountstatus_activity.CheckAccountStatusResponse
						var a *accountstatus_activity.Activity
						err = workflow.ExecuteActivity(
							withCheckAccountStatusActivityOptions(ctx),
							a.CheckAccountStatus,
							&accountstatus_activity.CheckAccountStatusRequest{
								AccountId: actOptResp.AccountId,
							},
						).
							Get(ctx, &result)
						if err != nil {
							logger.Error(
								"encountered error while checking account status",
								"error",
								err,
							)
							stopChan.Send(ctx, true)
							shouldStop = true
							cancelHandler()
							return
						}
						if !result.IsValid {
							logger.Warn("account is no longer is valid state")
							stopChan.Send(ctx, true)
							shouldStop = true
							cancelHandler()
							return
						}
					})

					selector.Select(ctx)

					if shouldStop {
						logger.Warn("exiting account status check")
						return
					}
					if ctx.Err() != nil {
						logger.Warn(
							"workflow canceled due to error or stop signal",
							"error",
							ctx.Err(),
						)
						return
					}
				}
			})
	}

	workselector := workflow.NewSelector(ctx)
	var activityErr error

	workselector.AddReceive(stopChan, func(c workflow.ReceiveChannel, more bool) {
		// Stop signal received, exit the routing
		logger.Warn("received signal to stop workflow based on account status")
		activityErr = errInvalidAccountStatusError
		cancelHandler()
	})

	splitConfigs := splitBenthosConfigs(bcResp.BenthosConfigs)
	if len(splitConfigs.Root) == 0 && len(splitConfigs.Dependents) > 0 {
		return nil, fmt.Errorf("root config not found. unable to process configs")
	}

	// Log the config split for debugging
	rootNames := make([]string, len(splitConfigs.Root))
	for i, bc := range splitConfigs.Root {
		rootNames[i] = bc.Name
	}
	dependentNames := make([]string, len(splitConfigs.Dependents))
	for i, bc := range splitConfigs.Dependents {
		dependentNames[i] = bc.Name
	}
	logger.Info("config split completed",
		"totalConfigs", len(bcResp.BenthosConfigs),
		"rootCount", len(splitConfigs.Root),
		"dependentCount", len(splitConfigs.Dependents),
		"rootConfigs", rootNames,
		"dependentConfigs", dependentNames,
	)

	maxConcurrency := getTableSyncMaxConcurrency()
	inFlight := 0
	completedCount := 0
	started := sync.Map{}

	// Build execution groups to handle circular dependencies
	executionGroups := buildExecutionGroups(bcResp.BenthosConfigs)
	groupTracker := NewGroupCompletionTracker(executionGroups)

	logger.Info(
		"execution groups created",
		"totalGroups", len(executionGroups),
		"totalConfigs", len(bcResp.BenthosConfigs),
	)

	executeSyncActivity := func(bc *benthosbuilder.BenthosConfigResponse, logger log.Logger) {
		future := invokeSync(
			bc,
			ctx,
			&started,
			groupTracker,
			logger,
			&bcResp.AccountId,
			actOptResp.SyncActivityOptions,
		)
		inFlight++
		workselector.AddFuture(future, func(f workflow.Future) {
			var wfResult tablesync_workflow.TableSyncResponse
			err := f.Get(ctx, &wfResult)
			inFlight--
			completedCount++
			if err != nil {
				logger.Error("activity did not complete", "err", err)
				activityErr = err
				cancelHandler()

				detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
				redisErr := runRedisCleanUpActivity(
					detachedCtx,
					logger,
					req.JobId,
					bcResp.BenthosConfigs,
				)
				if redisErr != nil {
					logger.Error("redis clean up activity did not complete")
				}
				return
			}
			logger.Info("config sync completed", "name", bc.Name)
			err = runPostTableSyncActivity(ctx, logger, actOptResp, bc.Name)
			if err != nil {
				logger.Error(
					fmt.Sprintf("post table sync activity did not complete: %s", err.Error()),
					"schema",
					bc.TableSchema,
					"table",
					bc.TableName,
				)
			}
		})
	}

	for _, bc := range splitConfigs.Root {
		// Ensures concurrency limits are respected.
		for inFlight >= maxConcurrency {
			logger.Debug("max concurrency reached; blocking until one sync finishes")
			workselector.Select(ctx)
			if activityErr != nil {
				return nil, activityErr
			}
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil, fmt.Errorf("workflow canceled due to error/stop: %w", ctx.Err())
				}
				return nil, ctx.Err()
			}
		}
		logger := log.With(logger, withBenthosConfigResponseLoggerTags(bc)...)
		executeSyncActivity(bc, logger)

		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, fmt.Errorf(
					"workflow canceled due to error or stop signal: %w",
					ctx.Err(),
				)
			}
			return nil, ctx.Err()
		}
	}

	logger.Info(
		"all root tables spawned, moving on to children",
		"totalConfigs", len(bcResp.BenthosConfigs),
		"completedCount", completedCount,
		"inFlight", inFlight,
	)
	for {
		// Ensures that the select statement below does not block indefinitely
		if len(bcResp.BenthosConfigs) == completedCount {
			logger.Info("all configs completed", "total", len(bcResp.BenthosConfigs), "completed", completedCount)
			break
		}
		workselector.Select(ctx)
		if activityErr != nil {
			return nil, activityErr
		}

		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, fmt.Errorf(
					"workflow canceled due to error or stop signal: %w",
					ctx.Err(),
				)
			}
			return nil, fmt.Errorf("exiting workflow in root sync due to err: %w", ctx.Err())
		}

		// todo: deadlock detection
		for _, bc := range splitConfigs.Dependents {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil, fmt.Errorf(
						"workflow canceled due to error or stop signal: %w",
						ctx.Err(),
					)
				}
				return nil, fmt.Errorf("exiting workflow in dependent sync due err: %w", ctx.Err())
			}
			bc := bc
			if _, configStarted := started.Load(bc.Name); configStarted {
				continue
			}
			isReady, err := isConfigReady(bc, groupTracker)
			if err != nil {
				return nil, err
			}

			if !isReady {
				// Determine if it's a self-reference
				currentTable := husonym_benthos.BuildBenthosTable(bc.TableSchema, bc.TableName)
				depType := "external"
				for _, dep := range bc.DependsOn {
					if dep.Table == currentTable {
						depType = "self-reference"
						break
					}
				}
				logger.Debug(
					"config not ready, waiting for dependencies",
					"config", bc.Name,
					"depends_on", bc.DependsOn,
					"dependency_type", depType,
				)
				continue
			}
			logger.Info("config is ready, starting execution", "config", bc.Name)

			// Ensures concurrency limits are respected.
			if inFlight >= maxConcurrency {
				logger.Debug(
					"max concurrency reached; blocking until one sync finishes for a dependent",
				)
				workselector.Select(ctx)
				if activityErr != nil {
					return nil, activityErr
				}
				if ctx.Err() != nil {
					if errors.Is(ctx.Err(), context.Canceled) {
						return nil, fmt.Errorf(
							"workflow canceled due to error or stop signal: %w",
							ctx.Err(),
						)
					}
					return nil, fmt.Errorf(
						"exiting workflow in dependent sync due to err: %w",
						ctx.Err(),
					)
				}
			}

			executeSyncActivity(bc, log.With(logger, withBenthosConfigResponseLoggerTags(bc)...))
		}
	}

	logger.Info("data syncs completed")

	err = runReferentialIntegrityCheck(ctx, logger, req.JobId, bcResp.BenthosConfigs)
	if err != nil {
		return nil, err
	}

	err = restoreDestinationTriggers(ctx, logger, triggersVersion, req.JobId, actOptResp.AccountId)
	if err != nil {
		// The deferred restore, on a context of its own, is the one chance left: the flag
		// stays down so that it runs.
		return nil, err
	}
	triggersRestored = true

	err = execRunJobHooksByTiming(
		ctx,
		&jobhooks_by_timing_activity.RunJobHooksByTimingRequest{
			JobId:  req.JobId,
			Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC,
		},
		logger,
	)
	if err != nil {
		return nil, err
	}

	err = runRedisCleanUpActivity(ctx, logger, req.JobId, bcResp.BenthosConfigs)
	if err != nil {
		return nil, err
	}

	logger.Info("data sync workflow completed")
	return &WorkflowResponse{}, nil
}

func execRunJobHooksByTiming(
	ctx workflow.Context,
	req *jobhooks_by_timing_activity.RunJobHooksByTimingRequest,
	logger log.Logger,
) error {
	logger.Info(fmt.Sprintf("scheduling %q RunJobHooksByTiming for execution", req.Timing))
	var resp *jobhooks_by_timing_activity.RunJobHooksByTimingResponse
	var timingActivity *jobhooks_by_timing_activity.Activity
	err := workflow.ExecuteActivity(
		withJobHookTimingActivityOptions(ctx),
		timingActivity.RunJobHooksByTiming,
		req,
	).Get(ctx, &resp)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("completed %d %q RunJobHooksByTiming", resp.ExecCount, req.Timing))
	return nil
}

func runSchemaInitWorkflowByDestination(
	ctx workflow.Context,
	logger log.Logger,
	accountId, jobId, jobRunId string,
	destinations []*mgmtv1alpha1.JobDestination,
) error {
	initSchemaActivityOptions := &workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
		HeartbeatTimeout: 1 * time.Minute,
	}
	for _, destination := range destinations {
		schemaDrift := shouldUseSchemaDrift(destination)
		logger.Info(
			"scheduling Schema Initialization workflow for execution.",
			"destinationId",
			destination.GetId(),
		)
		siWf := &schemainit_workflow.Workflow{}
		var wfResult schemainit_workflow.SchemaInitResponse
		id := fmt.Sprintf("init-schema-%s", destination.GetId())
		err := workflow.ExecuteChildWorkflow(workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:    workflow_shared.BuildChildWorkflowId(jobRunId, id, workflow.Now(ctx)),
			StaticSummary: fmt.Sprintf("Initializing Schema for %s", destination.GetId()),
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 1,
			},
		}), siWf.SchemaInit, &schemainit_workflow.SchemaInitRequest{
			AccountId:                 accountId,
			JobId:                     jobId,
			SchemaInitActivityOptions: initSchemaActivityOptions,
			JobRunId:                  jobRunId,
			DestinationId:             destination.GetId(),
			UseSchemaDrift:            schemaDrift,
		}).
			Get(ctx, &wfResult)
		if err != nil {
			return err
		}
		logger.Info(
			"completed Schema Initialization workflow.",
			"destinationId",
			destination.GetId(),
		)
	}
	return nil
}

// shouldUseSchemaDrift tells whether a destination is reconciled with the source — columns,
// constraints and triggers added, changed and dropped until it matches — rather than only
// having its missing tables created.
//
// Husonym keeps a destination in step with its source, with sampling and anonymization on top:
// when the source changes, the destination follows, and that includes what the source no longer
// has. PostgreSQL was left out behind a switch hard-coded to false since the work landed
// upstream (NEOS-1790, spring 2025) and never flipped, although it was complete and tested; a
// new column then failed the write on any table the destination already had, where MySQL has
// always taken it in. Reconciliation still only touches a destination whose schema Husonym is
// asked to initialize: the schema managers return early when init_table_schema is off.
func shouldUseSchemaDrift(destination *mgmtv1alpha1.JobDestination) bool {
	return destination.GetOptions().GetPostgresOptions() != nil ||
		destination.GetOptions().GetMysqlOptions() != nil
}

func retrieveActivityOptions(
	ctx workflow.Context,
	jobId string,
	logger log.Logger,
) (*syncactivityopts_activity.RetrieveActivityOptionsResponse, error) {
	logger.Info("scheduling RetrieveActivityOptions for execution.")

	var actOptResp *syncactivityopts_activity.RetrieveActivityOptionsResponse
	var activityOptsActivity *syncactivityopts_activity.Activity
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 1 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 2,
			},
			HeartbeatTimeout: 1 * time.Minute,
		}),
		activityOptsActivity.RetrieveActivityOptions,
		&syncactivityopts_activity.RetrieveActivityOptionsRequest{
			JobId: jobId,
		}).
		Get(ctx, &actOptResp)
	if err != nil {
		return nil, err
	}
	logger.Info("completed RetrieveActivityOptions.")
	return actOptResp, nil
}

func runPostTableSyncActivity(
	ctx workflow.Context,
	logger log.Logger,
	actOptResp *syncactivityopts_activity.RetrieveActivityOptionsResponse,
	name string,
) error {
	logger.Debug("executing post table sync activity")
	var resp *posttablesync_activity.RunPostTableSyncResponse
	var postTableSyncActivity *posttablesync_activity.Activity
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 2,
			},
			HeartbeatTimeout: 1 * time.Minute,
		}),
		postTableSyncActivity.RunPostTableSync,
		&posttablesync_activity.RunPostTableSyncRequest{
			AccountId: actOptResp.AccountId,
			Name:      name,
		}).Get(ctx, &resp)
	if err != nil {
		return err
	}
	return nil
}

// runPreflightCheck keeps the report of what the run will meet, and stops the run before
// anything is read or written on a blocking finding.
func runPreflightCheck(
	ctx workflow.Context,
	logger log.Logger,
	jobId, jobRunId string,
	generated *genbenthosconfigs_activity.GenerateBenthosConfigsResponse,
) error {
	logger.Info("scheduling pre-flight check")
	var resp *preflight_activity.RunPreflightResponse
	var preflightActivity *preflight_activity.Activity
	return workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			// It only reads, and keeps its report under one key: a failure to ask a
			// connection or the API is asked again. A blocking finding is not retried.
			RetryPolicy:      &temporal.RetryPolicy{MaximumAttempts: 3},
			HeartbeatTimeout: 1 * time.Minute,
		}),
		preflightActivity.RunPreflight,
		&preflight_activity.RunPreflightRequest{
			JobId:     jobId,
			JobRunId:  jobRunId,
			AccountId: generated.AccountId,
			Tables:    runTables(generated.BenthosConfigs),
			Findings:  generated.Findings,
		},
	).Get(ctx, &resp)
}

// runPrivilegeCheck stops the run before anything is read or written when a connection
// lacks what its role in the job needs, on the tables and columns of the configs. Runs of
// version 1 pass no configs.
func runPrivilegeCheck(
	ctx workflow.Context,
	logger log.Logger,
	jobId string,
	configs []*benthosbuilder.BenthosConfigResponse,
) error {
	tables := runTables(configs)
	logger.Info("scheduling privilege check")
	var resp *preflight_activity.CheckRunPrivilegesResponse
	var privilegesActivity *preflight_activity.Activity
	return workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
			HeartbeatTimeout:    1 * time.Minute,
		}),
		privilegesActivity.CheckRunPrivileges,
		&preflight_activity.CheckRunPrivilegesRequest{JobId: jobId, Tables: tables},
	).Get(ctx, &resp)
}

// runTables returns the tables of the configs, with the columns the run writes into each.
func runTables(configs []*benthosbuilder.BenthosConfigResponse) []*preflight_activity.TableColumns {
	var tables []*preflight_activity.TableColumns
	byName := map[string]*preflight_activity.TableColumns{}
	for _, cfg := range configs {
		key := cfg.TableSchema + "." + cfg.TableName
		table, ok := byName[key]
		if !ok {
			table = &preflight_activity.TableColumns{Schema: cfg.TableSchema, Table: cfg.TableName}
			byName[key] = table
			tables = append(tables, table)
		}
		for _, column := range cfg.Columns {
			// A generated column is computed by the destination, never written by the run.
			if !slices.Contains(table.Columns, column) && !slices.Contains(cfg.GeneratedColumns, column) {
				table.Columns = append(table.Columns, column)
			}
		}
	}
	return tables
}

// suspendDestinationTriggers takes the triggers of the destinations out of the way of the
// run. A trigger firing on what the run writes adds rows nothing read in the source, and
// MySQL has no way to suspend one for a session. Runs started before this existed replay
// without it.
func suspendDestinationTriggers(
	ctx workflow.Context,
	logger log.Logger,
	version workflow.Version,
	jobId, accountId string,
	configs []*benthosbuilder.BenthosConfigResponse,
) error {
	if version == workflow.DefaultVersion {
		return nil
	}
	var tables []destinationtriggers_activity.TableRef
	seen := map[string]bool{}
	for _, cfg := range configs {
		key := cfg.TableSchema + "." + cfg.TableName
		if seen[key] {
			continue
		}
		seen[key] = true
		tables = append(tables, destinationtriggers_activity.TableRef{Schema: cfg.TableSchema, Table: cfg.TableName})
	}
	if len(tables) == 0 {
		return nil
	}
	var resp *destinationtriggers_activity.SuspendTriggersResponse
	var triggersActivity *destinationtriggers_activity.Activity
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
		}),
		triggersActivity.SuspendTriggers,
		&destinationtriggers_activity.SuspendTriggersRequest{JobId: jobId, AccountId: accountId, Tables: tables},
	).Get(ctx, &resp)
	if err != nil {
		return err
	}
	if resp != nil && resp.Suspended > 0 {
		logger.Info("destination triggers suspended for the time of the run", "triggers", resp.Suspended)
	}
	return nil
}

// restoreDestinationTriggers puts back what the job has out of its way: what this run took,
// and what an earlier run of the job stopped before putting back.
func restoreDestinationTriggers(
	ctx workflow.Context,
	logger log.Logger,
	version workflow.Version,
	jobId, accountId string,
) error {
	if version == workflow.DefaultVersion {
		return nil
	}
	var resp *destinationtriggers_activity.RestoreTriggersResponse
	var triggersActivity *destinationtriggers_activity.Activity
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		}),
		triggersActivity.RestoreTriggers,
		&destinationtriggers_activity.RestoreTriggersRequest{JobId: jobId, AccountId: accountId},
	).Get(ctx, &resp)
	if err != nil {
		return err
	}
	if resp != nil && resp.Restored > 0 {
		logger.Info("destination triggers restored", "triggers", resp.Restored)
	}
	return nil
}

// runReferentialIntegrityCheck verifies, once every table is written, that no destination
// row references a missing parent. Runs started before the check existed replay without it.
func runReferentialIntegrityCheck(
	ctx workflow.Context,
	logger log.Logger,
	jobId string,
	configs []*benthosbuilder.BenthosConfigResponse,
) error {
	version := workflow.GetVersion(ctx, "referential-integrity-check", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		return nil
	}
	var tables []*referentialintegrity_activity.TableForeignKeys
	for _, cfg := range configs {
		if cfg.RunType == runconfigs.RunTypeInsert && len(cfg.ForeignKeys) > 0 {
			tables = append(tables, &referentialintegrity_activity.TableForeignKeys{
				Schema: cfg.TableSchema, Table: cfg.TableName, ForeignKeys: cfg.ForeignKeys,
			})
		}
	}
	if len(tables) == 0 {
		return nil
	}
	logger.Info("scheduling referential integrity check", "tables", len(tables))
	var resp *referentialintegrity_activity.CheckReferentialIntegrityResponse
	var integrityActivity *referentialintegrity_activity.Activity
	return workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
			HeartbeatTimeout:    1 * time.Minute,
		}),
		integrityActivity.CheckReferentialIntegrity,
		&referentialintegrity_activity.CheckReferentialIntegrityRequest{JobId: jobId, Tables: tables},
	).Get(ctx, &resp)
}

func runRedisCleanUpActivity(
	ctx workflow.Context,
	logger log.Logger,
	jobId string,
	configs []*benthosbuilder.BenthosConfigResponse,
) error {
	for _, cfg := range configs {
		for _, redisCfg := range cfg.RedisConfig {
			logger.Debug("executing redis clean up activity", "hashKey", redisCfg.Key)
			var resp *syncrediscleanup_activity.DeleteRedisHashResponse
			var redisCleanUpActivity *syncrediscleanup_activity.Activity
			err := workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
					StartToCloseTimeout: 2 * time.Minute,
					RetryPolicy: &temporal.RetryPolicy{
						MaximumAttempts: 2,
					},
					HeartbeatTimeout: 1 * time.Minute,
				}),
				redisCleanUpActivity.DeleteRedisHash,
				&syncrediscleanup_activity.DeleteRedisHashRequest{
					JobId:   jobId,
					HashKey: redisCfg.Key,
				}).Get(ctx, &resp)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func withBenthosConfigResponseLoggerTags(bc *benthosbuilder.BenthosConfigResponse) []any {
	keyvals := []any{}

	if bc.Name != "" {
		keyvals = append(keyvals, "name", bc.Name)
	}
	if bc.TableSchema != "" {
		keyvals = append(keyvals, "schema", bc.TableSchema)
	}
	if bc.TableName != "" {
		keyvals = append(keyvals, "table", bc.TableName)
	}

	return keyvals
}

func getSyncMetadata(config *benthosbuilder.BenthosConfigResponse) *sync_activity.SyncMetadata {
	return &sync_activity.SyncMetadata{Schema: config.TableSchema, Table: config.TableName}
}

func invokeSync(
	config *benthosbuilder.BenthosConfigResponse,
	ctx workflow.Context,
	started *sync.Map,
	groupTracker *GroupCompletionTracker,
	logger log.Logger,
	accountId *string,
	syncActivityOptions *workflow.ActivityOptions,
) workflow.Future {
	info := workflow.GetInfo(ctx)
	metadata := getSyncMetadata(config)
	_ = metadata
	future, settable := workflow.NewFuture(ctx)
	logger.Debug("triggering config sync")
	started.Store(config.Name, struct{}{})
	workflow.GoNamed(ctx, config.Name, func(ctx workflow.Context) {
		var accId string
		if accountId != nil && *accountId != "" {
			accId = *accountId
		}
		logger.Info("scheduling Sync for execution.")

		tsWf := &tablesync_workflow.Workflow{}
		var wfResult tablesync_workflow.TableSyncResponse

		err := workflow.ExecuteChildWorkflow(workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:    workflow_shared.BuildChildWorkflowId(info.WorkflowExecution.ID, config.Name, workflow.Now(ctx)),
			StaticSummary: fmt.Sprintf("Syncing %s.%s", config.TableSchema, config.TableName),
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 1,
			},
		}), tsWf.TableSync, &tablesync_workflow.TableSyncRequest{
			AccountId:             accId,
			Id:                    config.Name,
			SyncActivityOptions:   syncActivityOptions,
			ContinuationToken:     nil,
			JobRunId:              info.WorkflowExecution.ID,
			TableSchema:           config.TableSchema,
			TableName:             config.TableName,
			ColumnIdentityCursors: config.ColumnIdentityCursors,
		}).
			Get(ctx, &wfResult)
		if err == nil {
			markErr := groupTracker.MarkConfigComplete(config.Name)
			if markErr != nil {
				logger.Error("failed to mark config complete", "config", config.Name, "error", markErr)
				settable.Set(wfResult, markErr)
				return
			}
			logger.Info("config sync completed", "config", config.Name)
		}
		settable.Set(wfResult, err)
	})
	return future
}

func isConfigReady(
	config *benthosbuilder.BenthosConfigResponse,
	groupTracker *GroupCompletionTracker,
) (bool, error) {
	if groupTracker == nil {
		return false, fmt.Errorf("group tracker is nil: cannot determine if config is ready")
	}
	if config == nil {
		return false, nil
	}

	// Use the group tracker's built-in logic to check if config can start
	// This handles:
	// - Group dependencies (waiting for dependent groups to complete)
	// - Cycle-aware execution (INSERT phase before UPDATE phase in cycles)
	return groupTracker.CanConfigStart(config), nil
}

type SplitConfigs struct {
	Root       []*benthosbuilder.BenthosConfigResponse
	Dependents []*benthosbuilder.BenthosConfigResponse
}

func splitBenthosConfigs(configs []*benthosbuilder.BenthosConfigResponse) *SplitConfigs {
	out := &SplitConfigs{
		Root:       []*benthosbuilder.BenthosConfigResponse{},
		Dependents: []*benthosbuilder.BenthosConfigResponse{},
	}
	for _, cfg := range configs {
		if len(cfg.DependsOn) == 0 {
			out.Root = append(out.Root, cfg)
		} else {
			out.Dependents = append(out.Dependents, cfg)
		}
	}

	return out
}

func getAccountStatusTimerDuration() time.Duration {
	envtime := viper.GetInt("CHECK_ACCOUNT_TIMER_SECONDS")
	if envtime == 0 {
		return 5 * time.Second
	}
	return time.Duration(envtime) * time.Second
}

func getTableSyncMaxConcurrency() int {
	maxConcurrency := viper.GetInt("TABLESYNC_MAX_CONCURRENCY")
	if maxConcurrency <= 0 {
		return 3 // default max concurrency
	}
	return maxConcurrency
}
