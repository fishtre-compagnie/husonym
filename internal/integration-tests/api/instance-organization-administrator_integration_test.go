package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/require"
)

func (s *IntegrationTestSuite) setUserRole(
	by mgmtv1alpha1connect.UserAccountServiceClient,
	accountId, userId string,
	role mgmtv1alpha1.AccountRole,
) error {
	_, err := by.SetUserRole(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRoleRequest{
		AccountId: accountId, UserId: userId, Role: role,
	}))
	return err
}

func (s *IntegrationTestSuite) removeMember(
	by mgmtv1alpha1connect.UserAccountServiceClient,
	accountId, userId string,
) error {
	_, err := by.RemoveTeamAccountMember(s.ctx, connect.NewRequest(&mgmtv1alpha1.RemoveTeamAccountMemberRequest{
		AccountId: accountId, UserId: userId,
	}))
	return err
}

// requireAnAdministratorIsKept is the refusal of a change that would leave the organization with
// no administrator.
func (s *IntegrationTestSuite) requireAnAdministratorIsKept(err error) {
	s.T().Helper()
	require.Error(s.T(), err)
	requireConnectError(s.T(), err, connect.CodeFailedPrecondition)
	require.Contains(s.T(), err.Error(), "the organization of this instance must keep an administrator")
}

// The organization is the only account of the people of an instance, and nothing replaces it:
// its only administrator can neither leave it nor be demoted. Once there is another, either can.
func (s *IntegrationTestSuite) Test_InstanceOrganization_KeepsAnAdministrator() {
	t := s.T()
	const (
		admin     = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN
		viewer    = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER
		developer = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER
	)
	first := s.person("administrator-first")
	firstId := s.setUser(s.ctx, first)
	organization := s.enterInstance(first)
	second := s.person("administrator-second")
	secondId := s.setUser(s.ctx, second)
	require.Equal(t, organization, s.enterInstance(second))

	// The only administrator does not leave, and is not demoted.
	s.requireAnAdministratorIsKept(s.removeMember(first, organization, firstId))
	s.requireAnAdministratorIsKept(s.setUserRole(first, organization, firstId, viewer))
	s.requireAdminOf(first, organization)
	require.Equal(t, 2, s.membersOf(organization))
	require.Len(t, s.accountsOf(first), 1)

	// Who administers nothing is given another role as before.
	require.NoError(t, s.setUserRole(first, organization, secondId, developer))
	s.requireAdminOf(first, organization)

	// With two administrators, one is demoted.
	require.NoError(t, s.setUserRole(first, organization, secondId, admin))
	require.NoError(t, s.setUserRole(first, organization, firstId, viewer))
	s.requireViewerOf(first, organization)
	s.requireAdminOf(second, organization)

	// The one left is now the only one.
	s.requireAnAdministratorIsKept(s.setUserRole(second, organization, secondId, viewer))
	s.requireAnAdministratorIsKept(s.removeMember(second, organization, secondId))
	s.requireAdminOf(second, organization)

	// Who administers nothing is removed as before.
	require.NoError(t, s.removeMember(second, organization, firstId))
	require.Equal(t, 1, s.membersOf(organization))
	require.Empty(t, s.accountsOf(first))
}

func (s *IntegrationTestSuite) Test_InstanceOrganization_OfTwoAdministratorsOneIsRemoved() {
	t := s.T()
	first := s.person("administrators-first")
	firstId := s.setUser(s.ctx, first)
	organization := s.enterInstance(first)
	second := s.person("administrators-second")
	secondId := s.setUser(s.ctx, second)
	require.Equal(t, organization, s.enterInstance(second))
	require.NoError(t, s.setUserRole(first, organization, secondId, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))

	require.NoError(t, s.removeMember(second, organization, firstId))

	require.Equal(t, 1, s.membersOf(organization))
	require.Empty(t, s.accountsOf(first))
	s.requireAdminOf(second, organization)
	s.requireAnAdministratorIsKept(s.removeMember(second, organization, secondId))

	// Who was removed comes back as a viewer, as anybody who signs in does.
	require.Equal(t, organization, s.enterInstance(first))
	s.requireViewerOf(first, organization)
}

// A team account the instance does not retain is as it was: its only administrator is demoted,
// or removed, when asked.
func (s *IntegrationTestSuite) Test_InstanceOrganization_AnotherTeamAccountKeepsNothing() {
	t := s.T()
	owner := s.person("another-team-owner")
	ownerId := s.setUser(s.ctx, owner)
	organization := s.enterInstance(owner)

	demoted := s.createTeamAccount(s.ctx, owner, "another-team-demoted")
	s.addMember(&hookGround{accountId: demoted, users: owner}, "another-team-member", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	require.NoError(t, s.setUserRole(owner, demoted, ownerId, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER))
	s.requireViewerOf(owner, demoted)

	left := s.createTeamAccount(s.ctx, owner, "another-team-left")
	member := s.OSSAuthenticatedLicensedClients.Users(s.addMember(
		&hookGround{accountId: left, users: owner}, "another-team-other-member", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
	))
	require.NoError(t, s.removeMember(owner, left, ownerId))
	require.Equal(t, 1, s.membersOf(left))
	s.requireViewerOf(member, left)
	require.NotContains(t, s.accountsOf(owner), left)

	// And the organization still has its administrator.
	s.requireAdminOf(owner, organization)
}
