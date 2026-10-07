package integrationtests_test

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// rolesGround is a team account of the expiring mode, whose license a test restricts, with an
// administrator.
type rolesGround struct {
	accountId string
	admin     mgmtv1alpha1connect.UserAccountServiceClient
	hooks     *hookGround
}

func (s *IntegrationTestSuite) newRolesGround(name string) *rolesGround {
	adminOpt := integrationtests_test.WithUserId(name + "-admin")
	admin := s.OSSAuthenticatedExpiringClients.Users(adminOpt)
	s.T().Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
	s.setUser(s.ctx, admin)
	accountId := s.createTeamAccount(s.ctx, admin, name+"-"+uuid.NewString())
	return &rolesGround{
		accountId: accountId,
		admin:     admin,
		// addMember needs the account and the client of its administrator, and nothing else.
		hooks: &hookGround{accountId: accountId, users: admin},
	}
}

// member makes another person a member of the account, with the role.
func (g *rolesGround) member(s *IntegrationTestSuite, token string, role mgmtv1alpha1.AccountRole) integrationtests_test.ClientConfigOption {
	return s.addMember(g.hooks, token, role)
}

func (g *rolesGround) setRole(s *IntegrationTestSuite, memberId string, role mgmtv1alpha1.AccountRole) error {
	_, err := g.admin.SetUserRole(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRoleRequest{
		AccountId: g.accountId, UserId: memberId, Role: role,
	}))
	return err
}

// invite sends an invitation, naming no role when role is nil.
func (g *rolesGround) invite(s *IntegrationTestSuite, email string, role *mgmtv1alpha1.AccountRole) error {
	_, err := g.admin.InviteUserToTeamAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.InviteUserToTeamAccountRequest{
		AccountId: g.accountId, Email: email, Role: role,
	}))
	return err
}

func (g *rolesGround) invites(s *IntegrationTestSuite) int {
	resp, err := g.admin.GetTeamAccountInvites(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetTeamAccountInvitesRequest{
		AccountId: g.accountId,
	}))
	requireNoErrResp(s.T(), resp, err)
	return len(resp.Msg.GetInvites())
}

// editsTheAccount is an action that takes the right to edit the account.
func (g *rolesGround) editsTheAccount(s *IntegrationTestSuite, as integrationtests_test.ClientConfigOption) error {
	_, err := s.OSSAuthenticatedExpiringClients.AccountSettings(as).SetAccountSetting(s.ctx, connect.NewRequest(
		&mgmtv1alpha1.SetAccountSettingRequest{AccountId: g.accountId, Config: consistencySetting("a-key")},
	))
	return err
}

func (g *rolesGround) readsTheJobs(s *IntegrationTestSuite, as integrationtests_test.ClientConfigOption) error {
	_, err := s.OSSAuthenticatedExpiringClients.Jobs(as).GetJobs(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
		AccountId: g.accountId,
	}))
	return err
}

func memberIdOf(s *IntegrationTestSuite, as integrationtests_test.ClientConfigOption) string {
	resp, err := s.OSSAuthenticatedExpiringClients.Users(as).GetUser(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetUserId()
}

// Giving a role other than administrator is rbac: by SetUserRole, and by an invitation that names
// one. The way back to everyone being an administrator, and an invitation that names no role, are
// not.
func (s *IntegrationTestSuite) Test_Roles_AreGatedByRbac() {
	t := s.T()
	g := s.newRolesGround("roles-gate")
	member := g.member(s, "roles-gate-member", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER)
	memberId := memberIdOf(s, member)
	viewer, admin := mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN

	// While the license includes everything, a role is given by an invitation as it always was.
	require.NoError(t, g.invite(s, "open@example.com", &viewer))
	require.Equal(t, 1, g.invites(s))

	s.closeFeature(license.FeatureRbac)

	t.Run("giving a role other than administrator is refused", func(t *testing.T) {
		requireFeatureRefusal(t, g.setRole(s, memberId, viewer), license.FeatureRbac)
	})

	t.Run("an invitation naming a role other than administrator is refused and writes nothing", func(t *testing.T) {
		requireFeatureRefusal(t, g.invite(s, "refused@example.com", &viewer), license.FeatureRbac)
		require.Equal(t, 1, g.invites(s))
	})

	t.Run("an invitation naming no role is served", func(t *testing.T) {
		require.NoError(t, g.invite(s, "no-role@example.com", nil))
		require.Equal(t, 2, g.invites(s))
	})

	t.Run("an invitation naming the administrator is served", func(t *testing.T) {
		require.NoError(t, g.invite(s, "admin@example.com", &admin))
		require.Equal(t, 3, g.invites(s))
	})

	t.Run("making a member an administrator is served", func(t *testing.T) {
		require.NoError(t, g.setRole(s, memberId, admin))
		require.NoError(t, g.editsTheAccount(s, member))
	})
}

// A role assigned while the license included rbac keeps applying once the feature is closed: the
// member is refused what the role does not allow, and still served what it does.
func (s *IntegrationTestSuite) Test_Role_StillAppliesWithoutTheFeature() {
	t := s.T()
	g := s.newRolesGround("roles-keep")
	viewer := g.member(s, "roles-keep-viewer", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	require.Error(t, g.editsTheAccount(s, viewer), "the role holds while the feature is allowed")

	s.closeFeature(license.FeatureRbac)

	err := g.editsTheAccount(s, viewer)
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.NotContains(t, err.Error(), "this license does not include", "refused by the role, not by the license")
	require.NoError(t, g.readsTheJobs(s, viewer))
}
