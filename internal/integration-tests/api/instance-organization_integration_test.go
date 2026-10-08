package integrationtests_test

import (
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	pkg_utils "github.com/fishtre-compagnie/husonym/backend/pkg/utils"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// otherIssuer is a provider the deployment is not configured with.
const otherIssuer = "https://idp.elsewhere.husonym.dev/"

// person signs somebody in with the provider of the deployment, as the web does before it
// enters the instance.
func (s *IntegrationTestSuite) person(token string) mgmtv1alpha1connect.UserAccountServiceClient {
	client := s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId(token))
	s.setUser(s.ctx, client)
	return client
}

func (s *IntegrationTestSuite) enterInstance(client mgmtv1alpha1connect.UserAccountServiceClient) string {
	s.T().Helper()
	resp, err := client.EnterInstance(s.ctx, connect.NewRequest(&mgmtv1alpha1.EnterInstanceRequest{}))
	requireNoErrResp(s.T(), resp, err)
	require.NotEmpty(s.T(), resp.Msg.GetAccountId())
	return resp.Msg.GetAccountId()
}

// retainedOrganization gives what the system information says of the organization: its account,
// and whether one is retained. It is asked without signing in, as the web asks it.
func (s *IntegrationTestSuite) retainedOrganization() (string, bool) {
	s.T().Helper()
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetSystemInformation(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemInformationRequest{}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetInstanceOrganizationAccountId(), resp.Msg.InstanceOrganizationAccountId != nil
}

func (s *IntegrationTestSuite) accountsOf(
	client mgmtv1alpha1connect.UserAccountServiceClient,
) map[string]*mgmtv1alpha1.UserAccount {
	s.T().Helper()
	resp, err := client.GetUserAccounts(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserAccountsRequest{}))
	requireNoErrResp(s.T(), resp, err)
	accounts := map[string]*mgmtv1alpha1.UserAccount{}
	for _, account := range resp.Msg.GetAccounts() {
		accounts[account.GetId()] = account
	}
	return accounts
}

// mayOnAccount says whether the person may do the action on the account itself: reading it is
// what a viewer may, updating it what an administrator may.
func (s *IntegrationTestSuite) mayOnAccount(
	client mgmtv1alpha1connect.UserAccountServiceClient,
	accountId string,
	action mgmtv1alpha1.ResourcePermission_Action,
) bool {
	s.T().Helper()
	resp, err := client.HasPermission(s.ctx, connect.NewRequest(&mgmtv1alpha1.HasPermissionRequest{
		AccountId: accountId,
		Resource: &mgmtv1alpha1.ResourcePermission{
			Type: mgmtv1alpha1.ResourcePermission_TYPE_ACCOUNT, Id: accountId, Action: action,
		},
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetHasPermission()
}

func (s *IntegrationTestSuite) requireAdminOf(client mgmtv1alpha1connect.UserAccountServiceClient, accountId string) {
	s.T().Helper()
	require.True(s.T(), s.mayOnAccount(client, accountId, mgmtv1alpha1.ResourcePermission_ACTION_UPDATE))
}

func (s *IntegrationTestSuite) requireViewerOf(client mgmtv1alpha1connect.UserAccountServiceClient, accountId string) {
	s.T().Helper()
	require.True(s.T(), s.mayOnAccount(client, accountId, mgmtv1alpha1.ResourcePermission_ACTION_READ))
	require.False(s.T(), s.mayOnAccount(client, accountId, mgmtv1alpha1.ResourcePermission_ACTION_UPDATE))
}

// membersOf counts the members of an account, people and keys alike, in the database.
func (s *IntegrationTestSuite) membersOf(accountId string) int {
	s.T().Helper()
	var members int
	require.NoError(s.T(), s.Pgcontainer.DB.QueryRow(s.ctx,
		"SELECT count(*) FROM husonym_api.account_user_associations WHERE account_id = $1::uuid", accountId,
	).Scan(&members))
	return members
}

// apiKeyOf gives a client that calls with an API key of the account, holding the permissions.
func (s *IntegrationTestSuite) apiKeyOf(
	accountId, creatorId string,
	permissions ...string,
) mgmtv1alpha1connect.UserAccountServiceClient {
	s.T().Helper()
	accountUuid, err := husonymdb.ToUuid(accountId)
	require.NoError(s.T(), err)
	creatorUuid, err := husonymdb.ToUuid(creatorId)
	require.NoError(s.T(), err)
	key := apikey.NewV1AccountKey()
	_, err = husonymdb.New(s.Pgcontainer.DB, s.HusonymQuerier).CreateAccountApikey(s.ctx, &husonymdb.CreateAccountApiKeyRequest{
		KeyName: "instance-organization", KeyValue: pkg_utils.ToSha256(key),
		AccountUuid: accountUuid, CreatedByUserUuid: creatorUuid,
		ExpiresAt:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
		Permissions: permissions,
	})
	require.NoError(s.T(), err)
	return s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId(key))
}

func (s *IntegrationTestSuite) setInstanceOrganization(
	client mgmtv1alpha1connect.UserAccountServiceClient,
	accountId, name string,
) (*connect.Response[mgmtv1alpha1.SetInstanceOrganizationResponse], error) {
	return client.SetInstanceOrganization(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetInstanceOrganizationRequest{
		AccountId: accountId, Name: name,
	}))
}

// Without authentication there is one anonymous user and their personal account, as before.
func (s *IntegrationTestSuite) Test_EnterInstance_AuthenticationOff_IsThePersonalAccount() {
	t := s.T()
	anonymous := s.OSSUnauthenticatedLicensedClients.Users()

	entered := s.enterInstance(anonymous)

	require.Equal(t, s.createPersonalAccount(s.ctx, anonymous), entered)
	accounts := s.accountsOf(anonymous)
	require.Len(t, accounts, 1)
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL, accounts[entered].GetType())
	_, retained := s.retainedOrganization()
	require.False(t, retained)
}

// On an instance that has no account, the first person to enter creates the organization and
// administers it; the next ones enter it as viewers; entering again changes nothing, and never
// the role somebody holds.
func (s *IntegrationTestSuite) Test_EnterInstance_NewInstance_OneOrganizationForEverybody() {
	t := s.T()
	_, retained := s.retainedOrganization()
	require.False(t, retained, "nothing is retained before anybody entered")

	first := s.person("organization-first")
	organization := s.enterInstance(first)

	retainedId, retained := s.retainedOrganization()
	require.True(t, retained)
	require.Equal(t, organization, retainedId)
	accounts := s.accountsOf(first)
	require.Len(t, accounts, 1, "no personal account beside the organization")
	require.Equal(t, "organization", accounts[organization].GetName())
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_TEAM, accounts[organization].GetType())
	s.requireAdminOf(first, organization)

	second := s.person("organization-second")
	require.Equal(t, organization, s.enterInstance(second))
	require.Len(t, s.accountsOf(second), 1, "no personal account beside the organization")
	s.requireViewerOf(second, organization)
	require.Equal(t, 2, s.membersOf(organization))

	// The administrator promotes the second person: their next entries leave them that role.
	secondId := s.setUser(s.ctx, second)
	promoted, err := first.SetUserRole(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRoleRequest{
		AccountId: organization, UserId: secondId, Role: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	}))
	requireNoErrResp(t, promoted, err)

	for range 2 {
		require.Equal(t, organization, s.enterInstance(first))
		require.Equal(t, organization, s.enterInstance(second))
	}
	s.requireAdminOf(first, organization)
	s.requireAdminOf(second, organization)
	require.Equal(t, 2, s.membersOf(organization))
	require.Len(t, s.accountsOf(second), 1)
}

// An instance that already has accounts does not change by itself: everybody keeps getting a
// personal account until an administrator designates the organization.
func (s *IntegrationTestSuite) Test_EnterInstance_InstanceWithAccounts_IsThePersonalAccount() {
	t := s.T()
	earlier := s.person("accounts-earlier")
	personal := s.createPersonalAccount(s.ctx, earlier)

	require.Equal(t, personal, s.enterInstance(earlier))
	newcomer := s.person("accounts-newcomer")
	entered := s.enterInstance(newcomer)

	require.NotEqual(t, personal, entered)
	accounts := s.accountsOf(newcomer)
	require.Len(t, accounts, 1)
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL, accounts[entered].GetType())
	_, retained := s.retainedOrganization()
	require.False(t, retained)
}

// The organization is for the people the provider of the deployment vouches for. Somebody
// another provider vouches for, and an API key, get a personal account as before, on a new
// instance as on one that retains an organization: neither creates it, neither enters it.
func (s *IntegrationTestSuite) Test_EnterInstance_AnotherIssuerOrAnApiKey_NeverEntersTheOrganization() {
	t := s.T()
	elsewhere := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("organization-elsewhere"),
		integrationtests_test.WithIssuer(otherIssuer),
	)
	s.setUser(s.ctx, elsewhere)

	// On an instance that has no account yet, they do not create the organization.
	personal := s.enterInstance(elsewhere)
	_, retained := s.retainedOrganization()
	require.False(t, retained)
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL, s.accountsOf(elsewhere)[personal].GetType())

	// Somebody of the deployment designates an organization.
	owner := s.person("organization-owner")
	ownerId := s.setUser(s.ctx, owner)
	organization := s.createTeamAccount(s.ctx, owner, "the-organization")
	designated, err := s.setInstanceOrganization(owner, organization, "")
	requireNoErrResp(t, designated, err)
	members := s.membersOf(organization)

	// With an organization retained, they still do not enter it.
	require.Equal(t, personal, s.enterInstance(elsewhere))
	require.Len(t, s.accountsOf(elsewhere), 1)

	key := s.apiKeyOf(organization, ownerId, "account:create")
	keyAccount := s.enterInstance(key)
	require.NotEqual(t, organization, keyAccount)
	set, err := key.SetPersonalAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetPersonalAccountRequest{}))
	requireNoErrResp(t, set, err)
	require.Equal(t, set.Msg.GetAccountId(), keyAccount, "an API key gets what SetPersonalAccount gives it")

	require.Equal(t, members, s.membersOf(organization), "the organization gained a member")
	retainedId, _ := s.retainedOrganization()
	require.Equal(t, organization, retainedId)
}

// A token the provider of the deployment issued to an application is no person, even when a user
// exists for its subject: it gets what SetPersonalAccount gives, and does not enter.
func (s *IntegrationTestSuite) Test_EnterInstance_AnApplicationToken_NeverEntersTheOrganization() {
	t := s.T()
	organization := s.enterInstance(s.person("application-first"))
	members := s.membersOf(organization)
	// The user of the subject exists, as one made before application tokens were refused.
	s.person("application-principal")
	application := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("application-principal"),
		integrationtests_test.WithApplicationToken(),
	)

	entered := s.enterInstance(application)

	require.NotEqual(t, organization, entered)
	set, err := application.SetPersonalAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetPersonalAccountRequest{}))
	requireNoErrResp(t, set, err)
	require.Equal(t, set.Msg.GetAccountId(), entered)
	accounts := s.accountsOf(application)
	require.Len(t, accounts, 1)
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL, accounts[entered].GetType())
	require.Equal(t, members, s.membersOf(organization), "the organization gained a member")
}

// Nor does it designate the organization, although the user of its subject administers the
// account: the same subject, signed in as a person, does.
func (s *IntegrationTestSuite) Test_SetInstanceOrganization_AnApplicationTokenIsRefused() {
	t := s.T()
	person := s.person("application-designation")
	personal := s.createPersonalAccount(s.ctx, person)
	application := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("application-designation"),
		integrationtests_test.WithApplicationToken(),
	)

	resp, err := s.setInstanceOrganization(application, personal, "acme")

	requireErrResp(t, resp, err)
	requireConnectError(t, err, connect.CodePermissionDenied)
	_, retained := s.retainedOrganization()
	require.False(t, retained)

	designated, err := s.setInstanceOrganization(person, personal, "acme")
	requireNoErrResp(t, designated, err)
}

// The schema holds the retained account, so it only goes missing once the constraint is gone:
// it is dropped here, in a schema this test alone uses. Nobody is then let in anywhere: neither
// a second organization nor a personal account is made.
func (s *IntegrationTestSuite) Test_EnterInstance_RetainedAccountMissing_IsRefused() {
	t := s.T()
	someone := s.person("organization-missing")
	_, err := s.Pgcontainer.DB.Exec(s.ctx,
		"ALTER TABLE husonym_api.instance DROP CONSTRAINT instance_organization_account_id_fkey")
	require.NoError(t, err)
	gone := uuid.NewString()
	_, err = s.Pgcontainer.DB.Exec(s.ctx, "UPDATE husonym_api.instance SET organization_account_id = $1", gone)
	require.NoError(t, err)

	resp, err := someone.EnterInstance(s.ctx, connect.NewRequest(&mgmtv1alpha1.EnterInstanceRequest{}))

	requireErrResp(t, resp, err)
	requireConnectError(t, err, connect.CodeInternal)
	require.NotContains(t, err.Error(), gone)
	require.Empty(t, s.accountsOf(someone))
	retainedId, _ := s.retainedOrganization()
	require.Equal(t, gone, retainedId)
}

func (s *IntegrationTestSuite) Test_SetPersonalAccount_StillGivesAPersonalAccountOnceAnOrganizationIsRetained() {
	t := s.T()
	first := s.person("personal-after-organization")
	organization := s.enterInstance(first)

	personal := s.createPersonalAccount(s.ctx, first)

	require.NotEqual(t, organization, personal)
	accounts := s.accountsOf(first)
	require.Len(t, accounts, 2)
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_PERSONAL, accounts[personal].GetType())
	require.Equal(t, personal, s.createPersonalAccount(s.ctx, first), "the same one when asked again")
	retainedId, _ := s.retainedOrganization()
	require.Equal(t, organization, retainedId)
	// Entering still lands in the organization.
	require.Equal(t, organization, s.enterInstance(first))
}

// The designation is the gesture of a person of the deployment who administers the account,
// made once.
func (s *IntegrationTestSuite) Test_SetInstanceOrganization() {
	t := s.T()
	owner := s.person("designation-owner")
	ownerId := s.setUser(s.ctx, owner)
	personal := s.createPersonalAccount(s.ctx, owner)
	team := s.createTeamAccount(s.ctx, owner, "designation-team")
	viewer := s.OSSAuthenticatedLicensedClients.Users(s.addMember(
		&hookGround{accountId: team, users: owner}, "designation-viewer", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
	))

	refused := func(client mgmtv1alpha1connect.UserAccountServiceClient, accountId string, code connect.Code) {
		t.Helper()
		resp, err := s.setInstanceOrganization(client, accountId, "acme")
		requireErrResp(t, resp, err)
		requireConnectError(t, err, code)
		_, retained := s.retainedOrganization()
		require.False(t, retained)
	}

	// An API key that may edit the account is no person.
	refused(s.apiKeyOf(personal, ownerId, "account:edit"), personal, connect.CodePermissionDenied)
	// A viewer of the account may not edit it.
	refused(viewer, team, connect.CodePermissionDenied)
	// Somebody another provider vouches for is not of the deployment.
	elsewhere := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("designation-elsewhere"),
		integrationtests_test.WithIssuer(otherIssuer),
	)
	s.setUser(s.ctx, elsewhere)
	refused(elsewhere, s.createPersonalAccount(s.ctx, elsewhere), connect.CodePermissionDenied)
	// Without authentication there is nobody to make the gesture.
	anonymous := s.OSSUnauthenticatedLicensedClients.Users()
	refused(anonymous, s.createPersonalAccount(s.ctx, anonymous), connect.CodePermissionDenied)

	// The owner designates the personal account: it becomes a team account under the name, and
	// keeps its identifier. No personal account is made beside it.
	designated, err := s.setInstanceOrganization(owner, personal, "acme")
	requireNoErrResp(t, designated, err)
	require.Equal(t, personal, designated.Msg.GetAccountId())
	retainedId, retained := s.retainedOrganization()
	require.True(t, retained)
	require.Equal(t, personal, retainedId)
	accounts := s.accountsOf(owner)
	require.Len(t, accounts, 2, "the organization and the team, and no new personal account")
	require.Equal(t, "acme", accounts[personal].GetName())
	require.Equal(t, mgmtv1alpha1.UserAccountType_USER_ACCOUNT_TYPE_TEAM, accounts[personal].GetType())
	s.requireAdminOf(owner, personal)

	// A second designation is refused, whoever asks and whichever account.
	for _, accountId := range []string{personal, team} {
		resp, err := s.setInstanceOrganization(owner, accountId, "")
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodeFailedPrecondition)
	}
	retainedId, _ = s.retainedOrganization()
	require.Equal(t, personal, retainedId)

	// From then on, whoever signs in enters it as a viewer.
	require.Equal(t, personal, s.enterInstance(viewer))
	s.requireViewerOf(viewer, personal)
	require.Equal(t, personal, s.enterInstance(owner))
	s.requireAdminOf(owner, personal)
}
