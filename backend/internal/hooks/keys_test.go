package hooks_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// keyring is the store of API keys, holding the one key a test calls with.
type keyring struct {
	key db_queries.HusonymApiAccountApiKey
}

func (k keyring) GetAccountApiKeyByKeyValue(context.Context, db_queries.DBTX, string) (db_queries.HusonymApiAccountApiKey, error) {
	return k.key, nil
}

// method gives the procedure of the contract an operation of the table is, and its
// descriptor.
func method(t *testing.T, op *operation) (string, protoreflect.MethodDescriptor) {
	t.Helper()
	name, _, _ := strings.Cut(op.name, "/")
	service := "mgmt.v1alpha1.JobService"
	if strings.Contains(name, "Account") {
		service = "mgmt.v1alpha1.AccountHookService"
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service + "." + name))
	require.NoError(t, err)
	found, ok := descriptor.(protoreflect.MethodDescriptor)
	require.True(t, ok)
	return "/" + service + "/" + name, found
}

// enter passes a call made with an API key through what every call with a key passes first:
// the key is refused there unless it holds what the procedure declares. It gives the context
// the procedure is then called with.
func enter(t *testing.T, op *operation, accountID pgtype.UUID, scope []string) (context.Context, error) {
	t.Helper()
	procedure, descriptor := method(t, op)
	entrance := auth_apikey.New(keyring{key: db_queries.HusonymApiAccountApiKey{
		AccountID:   accountID,
		Permissions: scope,
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
	}}, nil, nil, nil)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+apikey.NewV1AccountKey())
	return entrance.InjectTokenCtx(t.Context(), header, connect.Spec{Procedure: procedure, Schema: descriptor})
}

// scopeOf is what a key has to hold to call an operation: what the procedure declares, and
// execute on jobs to enable a job hook, which the contract does not declare for the
// procedure that also disables.
func scopeOf(t *testing.T, op *operation) []string {
	t.Helper()
	_, descriptor := method(t, op)
	declared, ok := auth_apikey.Requires(descriptor)
	require.True(t, ok)
	scope := permission.Names(declared)
	if op.name == "SetJobHookEnabled/enable" {
		scope = append(scope, "job:execute")
	}
	return scope
}

func keyOperations() []*operation {
	ops := []*operation{}
	for i := range operations {
		if !strings.Contains(operations[i].name, "Slack") {
			ops = append(ops, &operations[i])
		}
	}
	return ops
}

// An API key holds what its scope names and nothing more: no permission implies another.
// A key that holds what an operation asks calls it, viewing included or not.
func TestAKeyHoldingWhatAnOperationAsksCallsIt(t *testing.T) {
	ops := keyOperations()
	require.Len(t, ops, 18)
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			w := newWorld(t)
			scope := scopeOf(t, op)
			ctx, err := enter(t, op, w.own.accountID, scope)
			require.NoError(t, err)

			_, err = op.call(ctx, w, w.own, false)

			require.NoError(t, err, "with %v", scope)
			require.Equal(t, op.cells[allGood].wrote, w.store.writes > 0)
		})
	}
}

// A key that lacks one of the permissions an operation asks is refused, and told which.
func TestAKeyLackingAPermissionIsToldWhich(t *testing.T) {
	for _, op := range keyOperations() {
		scope := scopeOf(t, op)
		for _, lacking := range scope {
			t.Run(op.name+"/without "+lacking, func(t *testing.T) {
				w := newWorld(t)
				held := slices.DeleteFunc(slices.Clone(scope), func(p string) bool { return p == lacking })

				ctx, err := enter(t, op, w.own.accountID, held)
				if err == nil {
					_, err = op.call(ctx, w, w.own, false)
				}

				requireAnswer(t, err, connect.CodePermissionDenied, "this API key lacks the permission "+lacking)
				require.Zero(t, w.store.writes)
			})
		}
	}
}

// A key of another account learns nothing: naming a job or a hook, it is answered as for
// what does not exist; naming the account, it is told the key is not of that account.
func TestAKeyOfAnotherAccountLearnsNothing(t *testing.T) {
	for _, op := range keyOperations() {
		t.Run(op.name, func(t *testing.T) {
			w := newWorld(t)
			ctx, err := enter(t, op, w.other.accountID, scopeOf(t, op))
			require.NoError(t, err)

			existingResp, existing := op.call(ctx, w, w.own, false)

			require.Zero(t, w.store.writes)
			if !op.byID {
				requireAnswer(t, existing, connect.CodePermissionDenied, "api key is not valid for account")
				return
			}
			nowhere, _ := newWorld(t).stand(absent)
			missingResp, missing := op.call(ctx, w, nowhere, false)
			require.Equal(t, connect.CodeOf(missing), connect.CodeOf(existing))
			if missing != nil {
				require.Error(t, existing)
				require.Equal(t, missing.Error(), existing.Error())
				return
			}
			require.NoError(t, existing)
			require.True(t, proto.Equal(missingResp.(proto.Message), existingResp.(proto.Message)))
		})
	}
}

// Holding what an operation asks in one account shows nothing of another account: the
// answer is the one given for what does not exist, and nothing is changed.
func TestWhatAKeyHoldsShowsNothingOfAnotherAccount(t *testing.T) {
	t.Run("delete a job hook", func(t *testing.T) {
		w := newWorld(t)
		ctx := asKey(t.Context(), w.own.accountID, "job:delete")
		_, err := w.jobs.DeleteJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: str(w.other.jobHook)}))
		require.NoError(t, err)
		require.Zero(t, w.store.writes)
		require.Contains(t, w.store.jobHooks, w.other.jobHook)
	})
	t.Run("delete an account hook", func(t *testing.T) {
		w := newWorld(t)
		ctx := asKey(t.Context(), w.own.accountID, "account:edit")
		_, err := w.account.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(w.other.webhook)}))
		require.NoError(t, err)
		require.Zero(t, w.store.writes)
		require.Contains(t, w.store.accountHooks, w.other.webhook)
	})
	t.Run("create a job hook", func(t *testing.T) {
		w := newWorld(t)
		ctx := asKey(t.Context(), w.own.accountID, "job:create", "job:execute")
		_, err := w.jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
			JobId: str(w.other.jobID),
			Hook:  &mgmtv1alpha1.NewJobHook{Name: "fresh", Description: "d", Config: sqlHook(str(w.other.connection))},
		}))
		requireAnswer(t, err, connect.CodeNotFound, jobNotFound)
		require.Zero(t, w.store.writes)
	})
	t.Run("update an account hook", func(t *testing.T) {
		w := newWorld(t)
		ctx := asKey(t.Context(), w.own.accountID, "account:view", "account:edit")
		_, err := w.account.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: str(w.other.webhook), Name: "renamed", Description: "d", Events: failedRun, Config: accountConfig(false),
		}))
		requireAnswer(t, err, connect.CodeNotFound, accountHookNotFound)
		require.Zero(t, w.store.writes)
	})
}

// A key that deletes without viewing deletes what its account holds.
func TestAKeyThatOnlyDeletesRemovesTheHook(t *testing.T) {
	w := newWorld(t)
	_, err := w.jobs.DeleteJobHook(asKey(t.Context(), w.own.accountID, "job:delete"),
		connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: str(w.own.jobHook)}))
	require.NoError(t, err)
	require.NotContains(t, w.store.jobHooks, w.own.jobHook)

	_, err = w.account.DeleteAccountHook(asKey(t.Context(), w.own.accountID, "account:edit"),
		connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: str(w.own.webhook)}))
	require.NoError(t, err)
	require.NotContains(t, w.store.accountHooks, w.own.webhook)
}
