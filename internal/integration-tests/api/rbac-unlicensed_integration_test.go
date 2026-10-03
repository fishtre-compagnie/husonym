package integrationtests_test

import (
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Access rules do not depend on the license: an instance without one still refuses a user
// the role-based permissions they do not hold.
func (s *IntegrationTestSuite) Test_Unlicensed_RbacIsEnforced() {
	ctx := s.ctx
	users := s.OSSUnauthenticatedUnlicensedClients.Users()
	jobs := s.OSSUnauthenticatedUnlicensedClients.Jobs()
	ownAccountId := s.createPersonalAccount(ctx, users)

	// The anonymous user cannot be made a member of an account without a role through the
	// API, so that account goes straight into the database: a member, holding no role.
	foreignAccount, err := s.HusonymQuerier.CreateTeamAccount(
		ctx,
		s.Pgcontainer.DB,
		"foreign-"+uuid.NewString(),
	)
	require.NoError(s.T(), err)
	foreignAccountId := uuid.UUID(foreignAccount.ID.Bytes).String()
	userResp, err := users.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	require.NoError(s.T(), err)
	userUuid, err := husonymdb.ToUuid(userResp.Msg.GetUserId())
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.HusonymQuerier.CreateAccountUserAssociation(
		ctx,
		s.Pgcontainer.DB,
		db_queries.CreateAccountUserAssociationParams{
			AccountID: foreignAccount.ID,
			UserID:    userUuid,
		},
	))

	s.T().Run("an account where the user holds no role is refused", func(t *testing.T) {
		resp, err := jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
			AccountId: foreignAccountId,
		}))
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
	})

	s.T().Run("the user's own account is served", func(t *testing.T) {
		resp, err := jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
			AccountId: ownAccountId,
		}))
		requireNoErrResp(t, resp, err)
	})
}
