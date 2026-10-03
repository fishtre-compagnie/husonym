package hooks_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// situation is where a caller stands towards what a request names.
type situation int

const (
	noAccess     situation = iota // belongs to no account
	noPermission                  // belongs to the account, as a viewer
	otherAccount                  // administers one account, names what another holds
	absent                        // administers one account, names what does not exist
	retired                       // administers the account, names or sends the retired kind
	noLicense                     // administers the account, the license is not valid
	allGood                       // administers the account, the license is valid
)

var situations = map[situation]string{
	noAccess:     "no access",
	noPermission: "access without the needed permission",
	otherAccount: "object of another account",
	absent:       "object that does not exist",
	retired:      "retired kind",
	noLicense:    "no license",
	allGood:      "all good",
}

const (
	jobNotFound         = "unable to find job id"
	jobHookNotFound     = "unable to find job hook by id"
	accountHookNotFound = "unable to find account hook by id"
	notInAccount        = "user is not in account"
	noActiveLicense     = "account does not have an active license"
	slackRetired        = "Slack account hooks are no longer supported: use a webhook instead"
)

// answer is what one operation gives in one situation.
type answer struct {
	stated  bool
	cannot  string       // why the situation does not arise for the operation
	code    connect.Code // zero when the call succeeds
	message string
	wrote   bool // the call changed what is stored
}

func cannot(why string) answer { return answer{stated: true, cannot: why} }
func served() answer           { return answer{stated: true} }
func written() answer          { return answer{stated: true, wrote: true} }
func notFound(message string) answer {
	return answer{stated: true, code: connect.CodeNotFound, message: message}
}
func denied(message string) answer {
	return answer{stated: true, code: connect.CodePermissionDenied, message: message}
}
func invalid(message string) answer {
	return answer{stated: true, code: connect.CodeInvalidArgument, message: message}
}
func unimplemented(procedure string) answer {
	return answer{stated: true, code: connect.CodeUnimplemented, message: procedure + " is not implemented"}
}
func everywhere(a answer) [7]answer { return [7]answer{a, a, a, a, a, a, a} }

const (
	noRetiredJobHook = "a job hook has no retired kind"
	viewIsEnough     = "every role that sees the owner may call this"
	noKindInRequest  = "the request names no hook and carries no configuration"
)

// operation is one procedure, how to call it on what a place holds, what it asks of its
// caller in order, and its answer in each situation.
type operation struct {
	name string
	// byID says the request names a job or a hook rather than an account: what the caller
	// may not see is then answered as what does not exist.
	byID  bool
	asks  []string
	call  func(ctx context.Context, w *world, p place, slack bool) (any, error)
	cells [7]answer
}

func out[T any](resp *connect.Response[T], err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func sqlHook(connection string) *mgmtv1alpha1.JobHookConfig {
	return &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
		Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
			Query:        "select 1;",
			ConnectionId: connection,
			Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
				Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
			},
		},
	}}
}

func webhook(url, secret string) *mgmtv1alpha1.AccountHookConfig {
	return &mgmtv1alpha1.AccountHookConfig{Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
		Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{Url: url, Secret: secret},
	}}
}

func slackHook() *mgmtv1alpha1.AccountHookConfig {
	return &mgmtv1alpha1.AccountHookConfig{Config: &mgmtv1alpha1.AccountHookConfig_Slack{
		Slack: &mgmtv1alpha1.AccountHookConfig_SlackHook{ChannelId: "C0123"},
	}}
}

func accountConfig(slack bool) *mgmtv1alpha1.AccountHookConfig {
	if slack {
		return slackHook()
	}
	return webhook("https://example.com/new", "a-new-secret")
}

var failedRun = []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED}

var (
	jobRead      = []string{"job:view"}
	accountRead  = []string{"account:view"}
	accountWrite = []string{"account:view", "account:edit"}
	accountArm   = []string{"account:view", "account:edit", "license"}
)

var operations = []operation{
	{
		name: "GetJobHooks", byID: true, asks: jobRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: str(p.jobID)})))
		},
		cells: [7]answer{
			noAccess: notFound(jobNotFound), noPermission: cannot(viewIsEnough),
			otherAccount: notFound(jobNotFound), absent: notFound(jobNotFound),
			retired: cannot(noRetiredJobHook), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetJobHook", byID: true, asks: jobRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: str(p.jobHook)})))
		},
		cells: [7]answer{
			noAccess: notFound(jobHookNotFound), noPermission: cannot(viewIsEnough),
			otherAccount: notFound(jobHookNotFound), absent: notFound(jobHookNotFound),
			retired: cannot(noRetiredJobHook), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "CreateJobHook", byID: true, asks: []string{"job:view", "job:create", "job:execute", "license"},
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
				JobId: str(p.jobID),
				Hook: &mgmtv1alpha1.NewJobHook{
					Name: "fresh", Description: "a new hook", Enabled: true, Priority: 3,
					Config: sqlHook(str(p.connection)),
				},
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobNotFound), noPermission: denied("user does not have permission to create job"),
			otherAccount: notFound(jobNotFound), absent: notFound(jobNotFound),
			retired: cannot(noRetiredJobHook), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "UpdateJobHook", byID: true, asks: []string{"job:view", "job:edit", "job:execute", "license"},
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.UpdateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
				Id: str(p.jobHook), Name: "renamed", Description: "changed", Enabled: true, Priority: 5,
				Config: sqlHook(str(p.connection)),
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobHookNotFound), noPermission: denied("user does not have permission to edit job"),
			otherAccount: notFound(jobHookNotFound), absent: notFound(jobHookNotFound),
			retired: cannot(noRetiredJobHook), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "SetJobHookEnabled/enable", byID: true, asks: []string{"job:view", "job:edit", "job:execute", "license"},
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
				Id: str(p.jobHookOff), Enabled: true,
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobHookNotFound), noPermission: denied("user does not have permission to edit job"),
			otherAccount: notFound(jobHookNotFound), absent: notFound(jobHookNotFound),
			retired: cannot(noRetiredJobHook), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "SetJobHookEnabled/disable", byID: true, asks: []string{"job:view", "job:edit"},
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
				Id: str(p.jobHook), Enabled: false,
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobHookNotFound), noPermission: denied("user does not have permission to edit job"),
			otherAccount: notFound(jobHookNotFound), absent: notFound(jobHookNotFound),
			retired: cannot(noRetiredJobHook), noLicense: written(), allGood: written(),
		},
	},
	{
		name: "DeleteJobHook", byID: true, asks: []string{"job:view", "job:delete"},
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.DeleteJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: str(p.jobHook)})))
		},
		cells: [7]answer{
			noAccess: served(), noPermission: denied("user does not have permission to delete job"),
			otherAccount: served(), absent: served(),
			retired: cannot(noRetiredJobHook), noLicense: written(), allGood: written(),
		},
	},
	{
		name: "IsJobHookNameAvailable", byID: true, asks: jobRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.IsJobHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{
				JobId: str(p.jobID), Name: "a-free-name",
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobNotFound), noPermission: cannot(viewIsEnough),
			otherAccount: notFound(jobNotFound), absent: notFound(jobNotFound),
			retired: cannot(noRetiredJobHook), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetActiveJobHooksByTiming", byID: true, asks: jobRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.jobs.GetActiveJobHooksByTiming(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
				JobId: str(p.jobID), Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
			})))
		},
		cells: [7]answer{
			noAccess: notFound(jobNotFound), noPermission: cannot(viewIsEnough),
			otherAccount: notFound(jobNotFound), absent: notFound(jobNotFound),
			retired: cannot(noRetiredJobHook), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetAccountHooks", asks: accountRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: str(p.accountID)})))
		},
		cells: [7]answer{
			noAccess: denied(notInAccount), noPermission: cannot(viewIsEnough),
			otherAccount: denied(notInAccount), absent: denied(notInAccount),
			retired: served(), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetAccountHook", byID: true, asks: accountRead,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			id := p.webhook
			if slack {
				id = p.slack
			}
			return out(w.account.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(id)})))
		},
		cells: [7]answer{
			noAccess: notFound(accountHookNotFound), noPermission: cannot(viewIsEnough),
			otherAccount: notFound(accountHookNotFound), absent: notFound(accountHookNotFound),
			retired: served(), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "CreateAccountHook", asks: accountArm,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			return out(w.account.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
				AccountId: str(p.accountID),
				Hook: &mgmtv1alpha1.NewAccountHook{
					Name: "fresh", Description: "a new hook", Events: failedRun, Enabled: true,
					Config: accountConfig(slack),
				},
			})))
		},
		cells: [7]answer{
			noAccess: denied(notInAccount), noPermission: denied("user does not have permission to edit account"),
			otherAccount: denied(notInAccount), absent: denied(notInAccount),
			retired: invalid(slackRetired), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "UpdateAccountHook", byID: true, asks: accountArm,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			return out(w.account.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
				Id: str(p.webhook), Name: "renamed", Description: "changed", Events: failedRun, Enabled: true,
				Config: accountConfig(slack),
			})))
		},
		cells: [7]answer{
			noAccess: notFound(accountHookNotFound), noPermission: denied("user does not have permission to edit account"),
			otherAccount: notFound(accountHookNotFound), absent: notFound(accountHookNotFound),
			retired: invalid(slackRetired), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "SetAccountHookEnabled/enable", byID: true, asks: accountArm,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			id := p.webhookOff
			if slack {
				id = p.slack
			}
			return out(w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
				Id: str(id), Enabled: true,
			})))
		},
		cells: [7]answer{
			noAccess: notFound(accountHookNotFound), noPermission: denied("user does not have permission to edit account"),
			otherAccount: notFound(accountHookNotFound), absent: notFound(accountHookNotFound),
			retired: invalid(slackRetired), noLicense: denied(noActiveLicense), allGood: written(),
		},
	},
	{
		name: "SetAccountHookEnabled/disable", byID: true, asks: accountWrite,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			id := p.webhook
			if slack {
				id = p.slack
			}
			return out(w.account.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
				Id: str(id), Enabled: false,
			})))
		},
		cells: [7]answer{
			noAccess: notFound(accountHookNotFound), noPermission: denied("user does not have permission to edit account"),
			otherAccount: notFound(accountHookNotFound), absent: notFound(accountHookNotFound),
			retired: written(), noLicense: written(), allGood: written(),
		},
	},
	{
		name: "DeleteAccountHook", byID: true, asks: accountWrite,
		call: func(ctx context.Context, w *world, p place, slack bool) (any, error) {
			id := p.webhook
			if slack {
				id = p.slack
			}
			return out(w.account.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(id)})))
		},
		cells: [7]answer{
			noAccess: served(), noPermission: denied("user does not have permission to edit account"),
			otherAccount: served(), absent: served(),
			retired: written(), noLicense: written(), allGood: written(),
		},
	},
	{
		name: "IsAccountHookNameAvailable", asks: accountRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.IsAccountHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountHookNameAvailableRequest{
				AccountId: str(p.accountID), Name: "a-free-name",
			})))
		},
		cells: [7]answer{
			noAccess: denied(notInAccount), noPermission: cannot(viewIsEnough),
			otherAccount: denied(notInAccount), absent: denied(notInAccount),
			retired: cannot(noKindInRequest), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetActiveAccountHooksByEvent", asks: accountRead,
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
				AccountId: str(p.accountID), Event: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			})))
		},
		cells: [7]answer{
			noAccess: denied(notInAccount), noPermission: cannot(viewIsEnough),
			otherAccount: denied(notInAccount), absent: denied(notInAccount),
			retired: served(), noLicense: served(), allGood: served(),
		},
	},
	{
		name: "GetSlackConnectionUrl",
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.GetSlackConnectionUrl(ctx, connect.NewRequest(&mgmtv1alpha1.GetSlackConnectionUrlRequest{AccountId: str(p.accountID)})))
		},
		cells: everywhere(unimplemented("mgmt.v1alpha1.AccountHookService/GetSlackConnectionUrl")),
	},
	{
		name: "HandleSlackOAuthCallback",
		call: func(ctx context.Context, w *world, _ place, _ bool) (any, error) {
			return out(w.account.HandleSlackOAuthCallback(ctx, connect.NewRequest(&mgmtv1alpha1.HandleSlackOAuthCallbackRequest{State: "state", Code: "code"})))
		},
		cells: everywhere(unimplemented("mgmt.v1alpha1.AccountHookService/HandleSlackOAuthCallback")),
	},
	{
		name: "TestSlackConnection",
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.TestSlackConnection(ctx, connect.NewRequest(&mgmtv1alpha1.TestSlackConnectionRequest{AccountId: str(p.accountID)})))
		},
		cells: everywhere(unimplemented("mgmt.v1alpha1.AccountHookService/TestSlackConnection")),
	},
	{
		name: "SendSlackMessage",
		call: func(ctx context.Context, w *world, p place, _ bool) (any, error) {
			return out(w.account.SendSlackMessage(ctx, connect.NewRequest(&mgmtv1alpha1.SendSlackMessageRequest{AccountHookId: str(p.slack)})))
		},
		cells: everywhere(unimplemented("mgmt.v1alpha1.AccountHookService/SendSlackMessage")),
	},
}

// stand puts the caller of a world in a situation, and gives what the request should name.
func (w *world) stand(s situation) (target place, slack bool) {
	switch s {
	case noAccess:
		w.people.members = map[string]bool{}
		return w.own, false
	case noPermission:
		w.role.grants = viewerGrants
		return w.own, false
	case otherAccount:
		return w.other, false
	case absent:
		return place{
			accountID: newID(), jobID: newID(), connection: newID(), jobHook: newID(), jobHookOff: newID(),
			webhook: newID(), webhookOff: newID(), slack: newID(),
		}, false
	case retired:
		return w.own, true
	case noLicense:
		w.papers.valid = false
		return w.own, false
	default:
		return w.own, false
	}
}

// asked is what an operation asks of its caller before it gives its answer in a situation.
func asked(op *operation, s situation, a answer) []string {
	switch {
	case s == noAccess, s == otherAccount, s == absent:
		// Whoever is not of the account is refused before any permission is looked at.
		return []string{}
	case s == noPermission:
		for i, ask := range op.asks {
			if !viewerGrants[ask] {
				return op.asks[:i+1]
			}
		}
		return op.asks
	case s == retired && a.code != 0:
		return slices.DeleteFunc(slices.Clone(op.asks), func(ask string) bool { return ask == "license" })
	default:
		return op.asks
	}
}

// Every operation checks its caller in one order: who may see the owner, then each permission,
// then what the object refuses, then the license. The table states the answer of each
// operation in each situation; the trail states what was asked before that answer.
func TestCheckOrder(t *testing.T) {
	require.Len(t, operations, 22, "sixteen operations, enabling and disabling apart, and the four retired ones")
	for i := range operations {
		op := &operations[i]
		for s, name := range situations {
			cell := op.cells[s]
			t.Run(op.name+"/"+name, func(t *testing.T) {
				require.True(t, cell.stated, "the table says nothing of this situation")
				if cell.cannot != "" {
					t.Skip("cannot occur: " + cell.cannot)
				}
				w := newWorld(t)
				target, slack := w.stand(s)

				_, err := op.call(t.Context(), w, target, slack)

				if cell.code == 0 {
					require.NoError(t, err)
				} else {
					requireAnswer(t, err, cell.code, cell.message)
				}
				require.Equal(t, cell.wrote, w.store.writes > 0, "writes: %d", w.store.writes)

				want := append([]string{}, asked(op, s, cell)...)
				if cell.code != 0 {
					require.Equal(t, want, append([]string{}, w.trail...), "what was asked before the refusal")
				} else {
					require.GreaterOrEqual(t, len(w.trail), len(want))
					require.Equal(t, want, append([]string{}, w.trail[:len(want)]...), "what was asked before the answer")
				}
			})
		}
	}
}

// A caller who may not see what a request names learns nothing of it: the answer is the one
// given for what does not exist, in its code, its message and its response.
func TestWhatIsHiddenAnswersAsAbsent(t *testing.T) {
	for i := range operations {
		op := &operations[i]
		if !op.byID {
			continue
		}
		t.Run(op.name, func(t *testing.T) {
			hidden := newWorld(t)
			hidden.people.members = map[string]bool{}
			hiddenResp, hiddenErr := op.call(t.Context(), hidden, hidden.own, false)

			missing := newWorld(t)
			missing.people.members = map[string]bool{}
			nowhere, _ := missing.stand(absent)
			missingResp, missingErr := op.call(t.Context(), missing, nowhere, false)

			require.Equal(t, connect.CodeOf(missingErr), connect.CodeOf(hiddenErr))
			if missingErr != nil {
				require.Error(t, hiddenErr)
				require.Equal(t, missingErr.Error(), hiddenErr.Error())
			} else {
				require.NoError(t, hiddenErr)
				require.True(t, proto.Equal(missingResp.(proto.Message), hiddenResp.(proto.Message)))
			}
			require.Zero(t, hidden.store.writes)
		})
	}
}

// A member who holds no role may do nothing in the account, seeing included.
func TestAMemberWithoutARoleSeesNothing(t *testing.T) {
	w := newWorld(t)
	w.role.grants = map[string]bool{}
	_, err := w.jobs.GetJobHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: str(w.own.jobHook)}))
	requireAnswer(t, err, connect.CodeNotFound, jobHookNotFound)
	_, err = w.account.GetAccountHook(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: str(w.own.webhook)}))
	requireAnswer(t, err, connect.CodeNotFound, accountHookNotFound)
}

// Writing a job hook takes more than editing the job: its SQL runs on the job's connections.
func TestAJobHookIsWrittenByWhoMayExecuteTheJob(t *testing.T) {
	w := newWorld(t)
	w.role.grants = map[string]bool{"job:view": true, "job:create": true, "job:edit": true}

	_, err := operations[2].call(t.Context(), w, w.own, false)
	requireAnswer(t, err, connect.CodePermissionDenied, "user does not have permission to execute job")
	require.Zero(t, w.store.writes)
}
