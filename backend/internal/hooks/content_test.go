package hooks_test

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func (w *world) createJobHook(t *testing.T, hook *mgmtv1alpha1.NewJobHook) (*mgmtv1alpha1.JobHook, error) {
	t.Helper()
	resp, err := w.jobs.CreateJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
		JobId: str(w.own.jobID), Hook: hook,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetHook(), nil
}

func (w *world) createWebhook(t *testing.T, name string, config *mgmtv1alpha1.AccountHookConfig, events ...mgmtv1alpha1.AccountHookEvent) (*mgmtv1alpha1.AccountHook, error) {
	t.Helper()
	resp, err := w.account.CreateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
		AccountId: str(w.own.accountID),
		Hook:      &mgmtv1alpha1.NewAccountHook{Name: name, Description: "a new hook", Events: events, Config: config},
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetHook(), nil
}

func pgID(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	return pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}
}

// A configuration is stored in the JSON form of its message: what a deployment already holds
// and what this code writes read the same.
func TestAConfigurationIsStoredInItsJSONForm(t *testing.T) {
	w := newWorld(t)
	connection := str(w.own.connection)

	pre, err := w.createJobHook(t, &mgmtv1alpha1.NewJobHook{Name: "pre", Description: "d", Config: sqlHook(connection)})
	require.NoError(t, err)
	require.JSONEq(t,
		fmt.Sprintf(`{"sql":{"query":"select 1;","connectionId":%q,"timing":{"preSync":{}}}}`, connection),
		string(w.store.jobHooks[pgID(t, pre.GetId())].Config),
	)

	postConfig := sqlHook(connection)
	postConfig.GetSql().Timing = &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
		Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
	}
	post, err := w.createJobHook(t, &mgmtv1alpha1.NewJobHook{Name: "post", Description: "d", Config: postConfig})
	require.NoError(t, err)
	require.JSONEq(t,
		fmt.Sprintf(`{"sql":{"query":"select 1;","connectionId":%q,"timing":{"postSync":{}}}}`, connection),
		string(w.store.jobHooks[pgID(t, post.GetId())].Config),
	)

	plain, err := w.createWebhook(t, "plain", webhook("https://example.com/hook", "s3cret"), failedRun...)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"webhook":{"url":"https://example.com/hook","secret":"s3cret"}}`,
		string(w.store.accountHooks[pgID(t, plain.GetId())].Config),
	)
	require.Equal(t, []int32{2}, w.store.accountHooks[pgID(t, plain.GetId())].Events)

	unverified := webhook("https://example.com/hook", "s3cret")
	unverified.GetWebhook().DisableSslVerification = true
	lax, err := w.createWebhook(t, "lax", unverified, failedRun...)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"webhook":{"url":"https://example.com/hook","secret":"s3cret","disableSslVerification":true}}`,
		string(w.store.accountHooks[pgID(t, lax.GetId())].Config),
	)
}

// A stored configuration that holds no kind this version knows is read as a hook without a
// configuration: it is listed and read, turned off and deleted, and never turned on.
func TestAStoredConfigurationOfNoKnownKind(t *testing.T) {
	for name, config := range map[string]string{
		"empty":           `{}`,
		"of unknown kind": `{"discord":{"channelId":"C1"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			ctx := t.Context()
			id := w.store.addAccountHook(w.own.accountID, "odd", config, true, 0)

			list, err := w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: str(w.own.accountID)}))
			require.NoError(t, err)
			require.Len(t, list.Msg.GetHooks(), 4)

			got, err := w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(id)}))
			require.NoError(t, err)
			require.Nil(t, got.Msg.GetHook().GetConfig().GetConfig())

			_, err = w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(id), Enabled: true}))
			requireAnswer(t, err, connect.CodeInvalidArgument, "account hook has no supported configuration: update it with a webhook")

			off, err := w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(id), Enabled: false}))
			require.NoError(t, err)
			require.False(t, off.Msg.GetHook().GetEnabled())

			_, err = w.account.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(id)}))
			require.NoError(t, err)
			require.NotContains(t, w.store.accountHooks, id)
		})
	}

	t.Run("a job hook", func(t *testing.T) {
		w := newWorld(t)
		id := w.store.addJobHook(w.own.jobID, "odd", `{"shell":{"command":"true"}}`, "", true, 0)
		got, err := w.jobs.GetJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: str(id)}))
		require.NoError(t, err)
		require.Nil(t, got.Msg.GetHook().GetConfig().GetConfig())
		list, err := w.jobs.GetJobHooks(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: str(w.own.jobID)}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetHooks(), 3)
	})
}

// A webhook is called over http or https, at a host. A stored address of another form is
// still read, turned off and on, and deleted: only writing one is refused.
func TestTheAddressOfAWebhook(t *testing.T) {
	const message = "webhook url must be an http or https address"

	for _, address := range []string{
		"http://receiver.internal/hook", "https://example.com:8443/hook?x=1", "HTTPS://EXAMPLE.COM/hook",
	} {
		t.Run("accepted: "+address, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.createWebhook(t, "fresh", webhook(address, "s3cret"), failedRun...)
			require.NoError(t, err)
		})
	}
	for _, address := range []string{
		"ftp://files.example.com/hook", "https://", "example.com/hook", "javascript:alert(1)", "/hook", "", "https:///hook",
	} {
		t.Run("refused: "+address, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.createWebhook(t, "fresh", webhook(address, "s3cret"), failedRun...)
			requireAnswer(t, err, connect.CodeInvalidArgument, message)
			require.Zero(t, w.store.writes)
		})
	}

	t.Run("a stored address of another form", func(t *testing.T) {
		w := newWorld(t)
		ctx := t.Context()
		id := w.store.addAccountHook(w.own.accountID, "old", webhookConfig("ftp://files.example.com/hook"), true, 0)

		got, err := w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(id)}))
		require.NoError(t, err)
		require.Equal(t, "ftp://files.example.com/hook", got.Msg.GetHook().GetConfig().GetWebhook().GetUrl())

		list, err := w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: str(w.own.accountID)}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetHooks(), 4)

		_, err = w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(id), Enabled: false}))
		require.NoError(t, err)

		_, err = w.account.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: str(id), Name: "old", Description: "changed", Events: failedRun,
			Config: webhook("ftp://files.example.com/hook", theSecret),
		}))
		requireAnswer(t, err, connect.CodeInvalidArgument, message)

		_, err = w.account.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(id)}))
		require.NoError(t, err)
	})
}

func TestTheEventsOfAnAccountHook(t *testing.T) {
	w := newWorld(t)

	_, err := w.createWebhook(t, "odd", webhook("https://example.com/hook", "s3cret"), mgmtv1alpha1.AccountHookEvent(42))
	requireAnswer(t, err, connect.CodeInvalidArgument, "invalid event: 42")

	hook, err := w.createWebhook(t, "all", webhook("https://example.com/hook", "s3cret"),
		mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED)
	require.NoError(t, err)
	require.Equal(t, []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED}, hook.GetEvents())
}

func TestAnAccountHookNeedsAConfiguration(t *testing.T) {
	w := newWorld(t)
	const message = "account hook config is required: a webhook"

	_, err := w.createWebhook(t, "bare", nil, failedRun...)
	requireAnswer(t, err, connect.CodeInvalidArgument, message)

	_, err = w.account.CreateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{AccountId: str(w.own.accountID)}))
	requireAnswer(t, err, connect.CodeInvalidArgument, message)

	_, err = w.account.UpdateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
		Id: str(w.own.webhook), Name: "renamed", Description: "changed", Events: failedRun,
	}))
	requireAnswer(t, err, connect.CodeInvalidArgument, message)
	require.Zero(t, w.store.writes)
}

// What a job hook holds is checked before it is stored: an SQL hook, a timing, a connection
// the job uses, a priority the database can hold.
func TestTheConfigurationOfAJobHook(t *testing.T) {
	connectionOutside := uuid.NewString()
	cases := []struct {
		name    string
		change  func(w *world, hook *mgmtv1alpha1.NewJobHook)
		message string
	}{
		{"no configuration", func(_ *world, hook *mgmtv1alpha1.NewJobHook) { hook.Config = nil },
			"job hook config is required: an sql hook"},
		{"a configuration of no kind", func(_ *world, hook *mgmtv1alpha1.NewJobHook) { hook.Config = &mgmtv1alpha1.JobHookConfig{} },
			"job hook config is required: an sql hook"},
		{"no timing", func(_ *world, hook *mgmtv1alpha1.NewJobHook) { hook.GetConfig().GetSql().Timing = nil },
			"job hook timing is required: pre_sync or post_sync"},
		{"a timing of no kind", func(_ *world, hook *mgmtv1alpha1.NewJobHook) {
			hook.GetConfig().GetSql().Timing = &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{}
		}, "job hook timing is required: pre_sync or post_sync"},
		{"a connection id that is no uuid", func(_ *world, hook *mgmtv1alpha1.NewJobHook) { hook.GetConfig().GetSql().ConnectionId = "nope" },
			"connection id specified in hook is not a valid uuid"},
		{"a connection the job does not use", func(_ *world, hook *mgmtv1alpha1.NewJobHook) {
			hook.GetConfig().GetSql().ConnectionId = connectionOutside
		}, "connection id specified in hook is not a part of job"},
		{"a priority the database cannot hold", func(_ *world, hook *mgmtv1alpha1.NewJobHook) { hook.Priority = math.MaxInt32 + 1 },
			"job hook priority is out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			hook := &mgmtv1alpha1.NewJobHook{Name: "fresh", Description: "d", Config: sqlHook(str(w.own.connection))}
			tc.change(w, hook)

			_, err := w.createJobHook(t, hook)
			requireAnswer(t, err, connect.CodeInvalidArgument, tc.message)

			_, err = w.jobs.UpdateJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
				Id: str(w.own.jobHook), Name: "renamed", Description: "d", Config: hook.GetConfig(), Priority: hook.GetPriority(),
			}))
			requireAnswer(t, err, connect.CodeInvalidArgument, tc.message)
			require.Zero(t, w.store.writes)
		})
	}

	t.Run("no hook at all", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.createJobHook(t, nil)
		requireAnswer(t, err, connect.CodeInvalidArgument, "job hook config is required: an sql hook")
	})

	t.Run("the SQL is stored as given", func(t *testing.T) {
		w := newWorld(t)
		config := sqlHook(str(w.own.connection))
		config.GetSql().Query = "  DROP TABLE x; -- anything\n"
		hook, err := w.createJobHook(t, &mgmtv1alpha1.NewJobHook{Name: "raw", Description: "d", Config: config})
		require.NoError(t, err)
		require.Equal(t, "  DROP TABLE x; -- anything\n", hook.GetConfig().GetSql().GetQuery())
	})
}

// What a viewer of a job may not learn by updating a hook: which connections the job uses.
func TestTheConnectionIsCheckedAfterTheCaller(t *testing.T) {
	w := newWorld(t)
	w.role.grants = viewerGrants
	_, err := w.jobs.UpdateJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
		Id: str(w.own.jobHook), Name: "renamed", Description: "d", Config: sqlHook(uuid.NewString()),
	}))
	requireAnswer(t, err, connect.CodePermissionDenied, "user does not have permission to edit job")
}

// A name is one hook's within a job or an account: the constraint of the database decides,
// and its refusal is told as such.
func TestANameAlreadyTaken(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()

	_, err := w.createJobHook(t, &mgmtv1alpha1.NewJobHook{Name: "own-pre", Description: "d", Config: sqlHook(str(w.own.connection))})
	requireAnswer(t, err, connect.CodeAlreadyExists, `a job hook named "own-pre" already exists for this job`)

	_, err = w.jobs.UpdateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
		Id: str(w.own.jobHook), Name: "own-post", Description: "d", Config: sqlHook(str(w.own.connection)),
	}))
	requireAnswer(t, err, connect.CodeAlreadyExists, `a job hook named "own-post" already exists for this job`)

	_, err = w.jobs.UpdateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
		Id: str(w.own.jobHook), Name: "own-pre", Description: "its own name", Config: sqlHook(str(w.own.connection)),
	}))
	require.NoError(t, err)

	_, err = w.createWebhook(t, "own-webhook", webhook("https://example.com/hook", "s3cret"), failedRun...)
	requireAnswer(t, err, connect.CodeAlreadyExists, `an account hook named "own-webhook" already exists for this account`)

	_, err = w.account.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
		Id: str(w.own.webhook), Name: "own-slack", Description: "d", Events: failedRun, Config: webhook("https://example.com/hook", "s3cret"),
	}))
	requireAnswer(t, err, connect.CodeAlreadyExists, `an account hook named "own-slack" already exists for this account`)

	jobName, err := w.jobs.IsJobHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{
		JobId: str(w.own.jobID), Name: "own-pre",
	}))
	require.NoError(t, err)
	require.False(t, jobName.Msg.GetIsAvailable())
	accountName, err := w.account.IsAccountHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountHookNameAvailableRequest{
		AccountId: str(w.own.accountID), Name: "own-webhook",
	}))
	require.NoError(t, err)
	require.False(t, accountName.Msg.GetIsAvailable())
}

// Asking for the state a hook is already in asks the caller the same, and writes nothing.
func TestEnablingWritesOnlyWhatChanges(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()

	jobHook, err := w.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{Id: str(w.own.jobHook), Enabled: true}))
	require.NoError(t, err)
	require.True(t, jobHook.Msg.GetHook().GetEnabled())
	accountHook, err := w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(w.own.webhook), Enabled: true}))
	require.NoError(t, err)
	require.True(t, accountHook.Msg.GetHook().GetEnabled())
	require.Zero(t, w.store.writes)

	w.papers.valid = false
	_, err = w.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{Id: str(w.own.jobHook), Enabled: true}))
	requireAnswer(t, err, connect.CodePermissionDenied, noActiveLicense)
	_, err = w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: str(w.own.webhook), Enabled: true}))
	requireAnswer(t, err, connect.CodePermissionDenied, noActiveLicense)
}

// A hook removed between the checks and the write is answered as not found.
func TestAHookRemovedMeanwhile(t *testing.T) {
	t.Run("job hook", func(t *testing.T) {
		w := newWorld(t)
		w.store.beforeWrite = func() { delete(w.store.jobHooks, w.own.jobHook) }
		_, err := w.jobs.UpdateJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
			Id: str(w.own.jobHook), Name: "renamed", Description: "d", Config: sqlHook(str(w.own.connection)),
		}))
		requireAnswer(t, err, connect.CodeNotFound, jobHookNotFound)
		_, err = w.jobs.SetJobHookEnabled(t.Context(), connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{Id: str(w.own.jobHookOff), Enabled: true}))
		require.NoError(t, err)
	})
	t.Run("account hook", func(t *testing.T) {
		w := newWorld(t)
		w.store.beforeWrite = func() { delete(w.store.accountHooks, w.own.webhook) }
		_, err := w.account.UpdateAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: str(w.own.webhook), Name: "renamed", Description: "d", Events: failedRun, Config: webhook("https://example.com/hook", "s3cret"),
		}))
		requireAnswer(t, err, connect.CodeNotFound, accountHookNotFound)
	})
}

// A failure to decide what the caller may do is told as a failure, not as an absent object.
func TestAFailureToDecideIsNotAnAbsentObject(t *testing.T) {
	w := newWorld(t)
	w.role.fails = errors.New("the roles cannot be read")
	_, err := w.jobs.GetJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: str(w.own.jobHook)}))
	require.ErrorContains(t, err, "the roles cannot be read")
	require.NotEqual(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestAnIdThatIsNoUuid(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	_, err := w.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: "nope"}))
	requireAnswer(t, err, connect.CodeInvalidArgument, "job hook id is not a valid uuid")
	_, err = w.jobs.IsJobHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{JobId: "nope", Name: "x"}))
	requireAnswer(t, err, connect.CodeInvalidArgument, "job id is not a valid uuid")
	_, err = w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: "nope"}))
	requireAnswer(t, err, connect.CodeInvalidArgument, "account hook id is not a valid uuid")
	_, err = w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: "nope"}))
	requireAnswer(t, err, connect.CodeInvalidArgument, "account id is not a valid uuid")
}

// What the worker asks before and after a sync: the hooks that are on, of the timing asked,
// in the order they run.
func TestTheActiveJobHooksOfATiming(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	connection := w.own.connection
	late := w.store.addJobHook(w.own.jobID, "late", sqlConfig(connection, "preSync"), "preSync", true, 50)
	early := w.store.addJobHook(w.own.jobID, "early", sqlConfig(connection, "preSync"), "preSync", true, 0)
	after := w.store.addJobHook(w.own.jobID, "after", sqlConfig(connection, "postSync"), "postSync", true, 0)
	w.store.addJobHook(w.own.jobID, "off", sqlConfig(connection, "preSync"), "preSync", false, 0)

	ids := func(timing mgmtv1alpha1.GetActiveJobHooksByTimingRequest_Timing) []string {
		resp, err := w.jobs.GetActiveJobHooksByTiming(asWorker(ctx), connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
			JobId: str(w.own.jobID), Timing: timing,
		}))
		require.NoError(t, err)
		out := []string{}
		for _, hook := range resp.Msg.GetHooks() {
			require.True(t, hook.GetEnabled())
			out = append(out, hook.GetId())
		}
		return out
	}

	require.Equal(t, []string{str(early), str(w.own.jobHook), str(late)}, ids(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC))
	require.Equal(t, []string{str(after)}, ids(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC))
	require.ElementsMatch(t, []string{str(early), str(after), str(w.own.jobHook), str(late)},
		ids(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_UNSPECIFIED))

	_, err := w.jobs.GetActiveJobHooksByTiming(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
		JobId: str(w.own.jobID), Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_Timing(7),
	}))
	requireAnswer(t, err, connect.CodeInvalidArgument, "invalid hook timing: 7")
}

// What the worker asks when a run is created, fails or succeeds: the hooks that are on and
// listen to the event or to every event. Asking for no event in particular gives the hooks
// that listen to every event.
func TestTheActiveAccountHooksOfAnEvent(t *testing.T) {
	w := newWorld(t)
	ctx := asWorker(t.Context())
	succeeded := w.store.addAccountHook(w.own.accountID, "succeeded", webhookConfig("https://example.com/ok"), true, 3)
	w.store.addAccountHook(w.own.accountID, "deaf", webhookConfig("https://example.com/deaf"), true)
	w.store.addAccountHook(w.own.accountID, "failed-off", webhookConfig("https://example.com/off"), false, 2)

	ids := func(event mgmtv1alpha1.AccountHookEvent) []string {
		resp, err := w.account.GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
			AccountId: str(w.own.accountID), Event: event,
		}))
		require.NoError(t, err)
		out := []string{}
		for _, hook := range resp.Msg.GetHooks() {
			out = append(out, hook.GetId())
		}
		return out
	}

	require.Equal(t, []string{str(w.own.webhook), str(w.own.slack)}, ids(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED))
	require.Equal(t, []string{str(w.own.slack), str(succeeded)}, ids(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED))
	require.Equal(t, []string{str(w.own.slack)}, ids(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED))
	require.Equal(t, []string{str(w.own.slack)}, ids(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED))

	got, err := w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(succeeded)}))
	require.NoError(t, err)
	require.Equal(t, theSecret, secretOf(got.Msg.GetHook()), "the worker signs with the secret")
}
