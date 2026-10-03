package hooks

import (
	"context"
	"slices"
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// retiredProcedures are the procedures of the Slack kind: they check nothing and answer that
// they are not implemented.
var retiredProcedures = []string{
	mgmtv1alpha1connect.AccountHookServiceGetSlackConnectionUrlProcedure,
	mgmtv1alpha1connect.AccountHookServiceHandleSlackOAuthCallbackProcedure,
	mgmtv1alpha1connect.AccountHookServiceTestSlackConnectionProcedure,
	mgmtv1alpha1connect.AccountHookServiceSendSlackMessageProcedure,
}

func hookProcedures(t *testing.T) []string {
	t.Helper()
	procedures := []string{}
	collect := func(service protoreflect.ServiceDescriptor, keep func(name string) bool) {
		methods := service.Methods()
		for i := range methods.Len() {
			if name := string(methods.Get(i).Name()); keep(name) {
				procedures = append(procedures, "/"+string(service.FullName())+"/"+name)
			}
		}
	}
	jobService := mgmtv1alpha1.File_mgmt_v1alpha1_job_proto.Services().ByName("JobService")
	require.NotNil(t, jobService)
	collect(jobService, func(name string) bool { return strings.Contains(name, "JobHook") })
	accountHookService := mgmtv1alpha1.File_mgmt_v1alpha1_account_hook_proto.Services().ByName("AccountHookService")
	require.NotNil(t, accountHookService)
	collect(accountHookService, func(string) bool { return true })
	return procedures
}

// A hook procedure the contract declares has a rule, or is one of the retired ones: adding a
// procedure without saying what it asks of its caller fails here.
func TestEveryHookProcedureHasARule(t *testing.T) {
	procedures := hookProcedures(t)
	require.Len(t, procedures, 20)

	for _, procedure := range procedures {
		_, ruled := rules[procedure]
		isRetired := slices.Contains(retiredProcedures, procedure)
		require.True(t, ruled != isRetired, "%s must have a rule, or be retired, and not both", procedure)
	}
	require.Len(t, rules, 16)
	for procedure := range rules {
		require.Contains(t, procedures, procedure, "a rule names a procedure of the contract")
	}
}

// What makes an object visible is the first thing a rule asks, and a rule asks about one kind
// of object only.
func TestEveryRuleStartsWithWhoMaySee(t *testing.T) {
	for procedure, r := range rules {
		require.NotNil(t, r.view, procedure)
		require.Equal(t, "view", r.view.String(), procedure)
		for _, action := range slices.Concat(r.actions, r.arming) {
			require.Equal(t, r.view.Kind(), action.Kind(), "%s asks about one kind of object", procedure)
		}
	}
}

// A rule asks what the contract declares for its procedure: a procedure that reads asks view,
// any other asks the actions listed. Enabling a job hook asks execute on top of it.
func TestEveryRuleAsksWhatTheContractDeclares(t *testing.T) {
	for procedure, r := range rules {
		service, name, _ := strings.Cut(strings.TrimPrefix(procedure, "/"), "/")
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service + "." + name))
		require.NoError(t, err, procedure)
		declared, ok := auth_apikey.Requires(descriptor.(protoreflect.MethodDescriptor))
		require.True(t, ok, procedure)

		asked := r.actions
		if len(asked) == 0 {
			asked = []rbac.Action{r.view}
		}
		names := []string{}
		for _, action := range asked {
			names = append(names, permission.Name(permission.Of(action)))
		}
		require.ElementsMatch(t, permission.Names(declared), names, procedure)

		arming := []string{}
		for _, action := range r.arming {
			arming = append(arming, permission.Name(permission.Of(action)))
		}
		if procedure == mgmtv1alpha1connect.JobServiceSetJobHookEnabledProcedure {
			require.Equal(t, []string{"job:execute"}, arming)
		} else {
			require.Empty(t, arming, procedure)
		}
	}
}

// A target with no owner and no answer for an absent object is a fault of the operation.
func TestATargetWithNothingToActOnIsAFault(t *testing.T) {
	g := gate{users: nobody{}}
	admitted, err := g.admit(t.Context(), mgmtv1alpha1connect.JobServiceGetJobHookProcedure, target{}, intent{})
	require.Nil(t, admitted)
	require.ErrorContains(t, err, "was given nothing to act on")
}

type nobody struct{}

func (nobody) GetUser(context.Context) (*userdata.User, error) { return &userdata.User{}, nil }

func TestAnOperationWithoutARuleIsRefused(t *testing.T) {
	_, err := gate{}.admit(t.Context(), "/mgmt.v1alpha1.JobService/NoSuchProcedure", target{}, intent{})
	require.ErrorContains(t, err, "no rule")
}
