package husonymdb

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
)

type IntegrationTestSuite struct {
	suite.Suite

	ctx context.Context

	pgcontainer   *tcpostgres.PostgresTestContainer
	migrationsDir string

	db *husonymdb.HusonymDb
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.ctx = context.Background()

	pgcontainer, err := tcpostgres.NewPostgresTestContainer(s.ctx)
	if err != nil {
		panic(err)
	}
	s.pgcontainer = pgcontainer

	s.migrationsDir = "../../../backend/sql/postgresql/schema"

	s.db = husonymdb.New(s.pgcontainer.DB, db_queries.New())
}

// Runs before each test
func (s *IntegrationTestSuite) SetupTest() {
	err := neomigrate.Up(s.ctx, s.pgcontainer.URL, s.migrationsDir, testutil.GetTestLogger(s.T()))
	if err != nil {
		panic(err)
	}
}

func (s *IntegrationTestSuite) TearDownTest() {
	// Dropping here because 1) more efficient and 2) we have a bad down migration
	// _jobs-connection-id-null.down that breaks due to having a null connection_id column.
	// we should do something about that at some point. Running this single drop is easier though
	_, err := s.pgcontainer.DB.Exec(s.ctx, "DROP SCHEMA IF EXISTS husonym_api CASCADE")
	if err != nil {
		panic(err)
	}
	_, err = s.pgcontainer.DB.Exec(s.ctx, "DROP TABLE IF EXISTS public.schema_migrations")
	if err != nil {
		panic(err)
	}
}

func (s *IntegrationTestSuite) TearDownSuite() {
	if s.pgcontainer != nil {
		err := s.pgcontainer.TearDown(s.ctx)
		if err != nil {
			panic(err)
		}
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	ok := testutil.ShouldRunIntegrationTest()
	if !ok {
		return
	}
	suite.Run(t, new(IntegrationTestSuite))
}

const testIssuer = "https://idp.example.com/"

// testIdentity is what every existing case means: the deployment's own issuer, which is
// the only one that may adopt a row recorded before issuers were.
func testIdentity(subject string) husonymdb.Identity {
	return husonymdb.Identity{Issuer: testIssuer, Subject: subject, MayAdoptLegacy: true}
}

func (s *IntegrationTestSuite) Test_SetUserByAuth0Id() {
	t := s.T()

	t.Run("new user", func(t *testing.T) {
		resp, err := s.db.SetUserByIdentity(s.ctx, testIdentity("foo"), nil)
		requireNoErrResp(t, resp, err)
		require.NotNil(t, resp.ID)
	})

	t.Run("idempotent", func(t *testing.T) {
		resp, err := s.db.SetUserByIdentity(s.ctx, testIdentity("myid"), nil)
		requireNoErrResp(t, resp, err)

		resp2, err := s.db.SetUserByIdentity(s.ctx, testIdentity("myid"), nil)
		requireNoErrResp(t, resp2, err)

		uid1 := husonymdb.UUIDString(resp.ID)
		uid2 := husonymdb.UUIDString(resp2.ID)
		require.Equal(t, uid1, uid2)
	})
}

func (s *IntegrationTestSuite) Test_SetUserByIdentity_IdentityProfile() {
	t := s.T()

	t.Run("stores what the provider sent, on the first sign-in", func(t *testing.T) {
		sub := "profile-first-signin"
		resp, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Name:          "Ada Lovelace",
			Email:         "ada@example.com",
			EmailVerified: true,
			Picture:       "https://example.com/ada.png",
		})
		requireNoErrResp(t, resp, err)

		association, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.Equal(t, "Ada Lovelace", association.Name.String)
		require.Equal(t, "ada@example.com", association.Email.String)
		require.True(t, association.EmailVerified)
		require.Equal(t, "https://example.com/ada.png", association.Picture.String)
	})

	t.Run("refreshes it on the next sign-in", func(t *testing.T) {
		sub := "profile-refresh"
		_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Name:          "Ada Lovelace",
			Email:         "ada@example.com",
			EmailVerified: true,
		})
		require.NoError(t, err)

		_, err = s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Name:          "Ada King",
			Email:         "ada.king@example.com",
			EmailVerified: true,
		})
		require.NoError(t, err)

		association, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.Equal(t, "Ada King", association.Name.String)
		require.Equal(t, "ada.king@example.com", association.Email.String)
	})

	t.Run("an assertion that disappears lowers email_verified back to false", func(t *testing.T) {
		sub := "profile-unverified"
		_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Email:         "ada@example.com",
			EmailVerified: true,
		})
		require.NoError(t, err)

		_, err = s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Email:         "ada@example.com",
			EmailVerified: false,
		})
		require.NoError(t, err)

		association, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.False(t, association.EmailVerified)
	})

	t.Run("a claim the provider did not send is an absence, not an erasure", func(t *testing.T) {
		sub := "profile-partial"
		_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Name:    "Ada Lovelace",
			Email:   "ada@example.com",
			Picture: "https://example.com/ada.png",
		})
		require.NoError(t, err)

		// A provider that answers only the address this time.
		_, err = s.db.SetUserByIdentity(s.ctx, testIdentity(sub), &authmgmt.User{
			Email: "ada@example.com",
		})
		require.NoError(t, err)

		association, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.Equal(t, "Ada Lovelace", association.Name.String)
		require.Equal(t, "https://example.com/ada.png", association.Picture.String)
	})

	// The path SetUser takes on every page load. It must not write, or two tabs of the
	// same user would collide on the row.
	t.Run("an unchanged profile writes nothing", func(t *testing.T) {
		sub := "profile-unchanged"
		profile := &authmgmt.User{
			Name:          "Ada Lovelace",
			Email:         "ada@example.com",
			EmailVerified: true,
			Picture:       "https://example.com/ada.png",
		}
		_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), profile)
		require.NoError(t, err)

		before, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)

		_, err = s.db.SetUserByIdentity(s.ctx, testIdentity(sub), profile)
		require.NoError(t, err)

		after, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: sub, ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.Equal(
			t,
			before.UpdatedAt.Time,
			after.UpdatedAt.Time,
			"an identical profile must leave the row untouched",
		)
	})

	t.Run("concurrent sign-ins of the same user do not collide", func(t *testing.T) {
		sub := "profile-concurrent"
		profile := &authmgmt.User{Name: "Ada Lovelace", Email: "ada@example.com"}
		_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), profile)
		require.NoError(t, err)

		group := new(errgroup.Group)
		for range 8 {
			group.Go(func() error {
				_, err := s.db.SetUserByIdentity(s.ctx, testIdentity(sub), profile)
				return err
			})
		}
		require.NoError(t, group.Wait(), "serialization failure on the sign-in path")
	})

	t.Run("concurrent first sign-ins of a new identity give one user", func(t *testing.T) {
		identity := testIdentity("first-sign-in-concurrent")

		group := new(errgroup.Group)
		uids := make([]string, 8)
		for i := range uids {
			group.Go(func() error {
				user, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
				if err != nil {
					return err
				}
				uids[i] = husonymdb.UUIDString(user.ID)
				return nil
			})
		}
		require.NoError(t, group.Wait(), "first sign-ins at once of the same identity")
		for _, uid := range uids {
			require.Equal(t, uids[0], uid, "first sign-ins at once of the same identity gave it several users")
		}
	})

	t.Run("no profile at all still signs the user in", func(t *testing.T) {
		resp, err := s.db.SetUserByIdentity(s.ctx, testIdentity("profile-absent"), nil)
		requireNoErrResp(t, resp, err)

		association, err := s.db.Q.GetUserAssociationByIdentity(
			s.ctx,
			s.db.Db,
			db_queries.GetUserAssociationByIdentityParams{ProviderSub: "profile-absent", ProviderIss: testIssuer},
		)
		require.NoError(t, err)
		require.False(t, association.Name.Valid)
		require.False(t, association.EmailVerified)
	})
}

func (s *IntegrationTestSuite) setUser(
	t testing.TB,
	ctx context.Context,
	sub string,
) *db_queries.HusonymApiUser {
	resp, err := s.db.SetUserByIdentity(ctx, testIdentity(sub), nil)
	requireNoErrResp(t, resp, err)
	return resp
}

func (s *IntegrationTestSuite) Test_SetPersonalAccount() {
	t := s.T()

	t.Run("new account", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "foo")
		maxAllowed := int64(100)
		resp, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowed)
		requireNoErrResp(t, resp, err)
	})

	t.Run("idempotent", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "foo1")
		maxAllowed := int64(100)

		resp, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowed)
		requireNoErrResp(t, resp, err)

		resp2, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowed)
		requireNoErrResp(t, resp2, err)

		uid1 := husonymdb.UUIDString(resp.ID)
		uid2 := husonymdb.UUIDString(resp2.ID)
		require.Equal(t, uid1, uid2)
	})

	t.Run("idempotent - parallel", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "foo2")
		maxAllowed := int64(100)

		// Neither call is cut short by the failure of the other: each one reports its own.
		errgrp := new(errgroup.Group)
		uids := make([]string, 8)
		for i := range uids {
			errgrp.Go(func() error {
				resp, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowed)
				if err != nil {
					return err
				}
				uids[i] = husonymdb.UUIDString(resp.ID)
				return nil
			})
		}

		require.NoError(t, errgrp.Wait(), "calls at once for the same user")
		for _, uid := range uids {
			require.Equal(t, uids[0], uid, "calls at once for the same user gave it several personal accounts")
		}
	})

	t.Run("a user that does not exist is refused", func(t *testing.T) {
		unknown, err := husonymdb.ToUuid(uuid.NewString())
		require.NoError(t, err)
		countAccounts := func() int {
			var count int
			require.NoError(t, s.pgcontainer.DB.QueryRow(s.ctx, "SELECT count(*) FROM husonym_api.accounts").Scan(&count))
			return count
		}
		before := countAccounts()

		resp, err := s.db.SetPersonalAccount(s.ctx, unknown, nil)
		requireErrResp(t, resp, err)
		require.True(t, husonymerrors.IsNotFound(err), "refused for another reason: %v", err)
		require.Equal(t, before, countAccounts(), "an account was left without a user")
	})
}

func (s *IntegrationTestSuite) Test_CreateTeamAccount() {
	t := s.T()

	t.Run("new account", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "foo")

		account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)
	})

	t.Run("already exists", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "foo1")

		account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)

		account1, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
		requireErrResp(t, account1, err)
		alreadyExists := husonymerrors.NewAlreadyExists("")
		require.ErrorAs(t, err, &alreadyExists)
	})
}

func (s *IntegrationTestSuite) Test_ConvertPersonalToTeamAccount() {
	t := s.T()

	t.Run("success", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "convertPtoT1")
		maxAllowedRecords := int64(100)
		account, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowedRecords)
		requireNoErrResp(t, account, err)

		newTeamName := "newteam"
		resp, err := s.db.ConvertPersonalToTeamAccount(
			s.ctx,
			&husonymdb.ConvertPersonalToTeamAccountRequest{
				UserId:            user.ID,
				PersonalAccountId: account.ID,
				TeamName:          newTeamName,
			},
			testutil.GetTestLogger(t),
		)
		requireNoErrResp(t, resp, err)

		require.Equal(
			t,
			husonymdb.UUIDString(account.ID),
			husonymdb.UUIDString(resp.TeamAccount.ID),
			"the new team account must be the same id as the old account",
		)
		require.Equal(
			t,
			husonymdb.AccountType_Team,
			husonymdb.AccountType(resp.TeamAccount.AccountType),
		)
		require.Equal(t, newTeamName, resp.TeamAccount.AccountSlug)
		require.NotEqual(
			t,
			husonymdb.UUIDString(account.ID),
			husonymdb.UUIDString(resp.PersonalAccount.ID),
			"the new personal account must not have the same id as the old one",
		)
		require.False(
			t,
			resp.TeamAccount.MaxAllowedRecords.Valid,
			"team account must not have any max allowed records set",
		)
		require.Equal(
			t,
			maxAllowedRecords,
			resp.PersonalAccount.MaxAllowedRecords.Int64,
			"max allowed records must persist on new personal account",
		)
	})

	t.Run("invalid account type", func(t *testing.T) {
		user := s.setUser(t, s.ctx, "convertPtoT2")
		account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)

		resp, err := s.db.ConvertPersonalToTeamAccount(
			s.ctx,
			&husonymdb.ConvertPersonalToTeamAccountRequest{
				UserId:            user.ID,
				PersonalAccountId: account.ID,
				TeamName:          "myteam2",
			},
			testutil.GetTestLogger(t),
		)
		requireErrResp(t, resp, err)
		badreqerror := husonymerrors.NewBadRequest("")
		require.ErrorAs(t, err, &badreqerror)
	})
}

func (s *IntegrationTestSuite) Test_UpsertStripeCustomerId() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")

	t.Run("new customer id", func(t *testing.T) {
		account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)
		require.False(t, account.StripeCustomerID.Valid)

		account, err = s.db.UpsertStripeCustomerId(
			s.ctx,
			account.ID,
			func(ctx context.Context, account db_queries.HusonymApiAccount) (string, error) {
				return "testid", nil
			},
			testutil.GetTestLogger(t),
		)
		requireNoErrResp(t, account, err)
		require.True(t, account.StripeCustomerID.Valid)
		require.Equal(t, "testid", account.StripeCustomerID.String)
	})

	t.Run("only first one", func(t *testing.T) {
		account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)
		require.False(t, account.StripeCustomerID.Valid)

		firstid := "testid"
		account, err = s.db.UpsertStripeCustomerId(
			s.ctx,
			account.ID,
			func(ctx context.Context, account db_queries.HusonymApiAccount) (string, error) {
				return firstid, nil
			},
			testutil.GetTestLogger(t),
		)
		requireNoErrResp(t, account, err)
		require.True(t, account.StripeCustomerID.Valid)
		require.Equal(t, firstid, account.StripeCustomerID.String)

		account, err = s.db.UpsertStripeCustomerId(
			s.ctx,
			account.ID,
			func(ctx context.Context, account db_queries.HusonymApiAccount) (string, error) {
				return "secondid", nil
			},
			testutil.GetTestLogger(t),
		)
		requireNoErrResp(t, account, err)
		require.True(t, account.StripeCustomerID.Valid)
		require.Equal(t, firstid, account.StripeCustomerID.String)
	})

	t.Run("personal not allowed", func(t *testing.T) {
		account, err := s.db.SetPersonalAccount(s.ctx, user.ID, nil)
		requireNoErrResp(t, account, err)
		require.False(t, account.StripeCustomerID.Valid)

		account, err = s.db.UpsertStripeCustomerId(
			s.ctx,
			account.ID,
			func(ctx context.Context, account db_queries.HusonymApiAccount) (string, error) {
				return "testid", nil
			},
			testutil.GetTestLogger(t),
		)
		requireErrResp(t, account, err)
	})
}

var (
	dbViewerRole = pgtype.Int4{
		Int32: int32(mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER),
		Valid: true,
	}
)

func (s *IntegrationTestSuite) Test_CreateTeamAccountInvite() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	t.Run("new invite", func(t *testing.T) {
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo2@example.com",
			getFutureTs(t, 1*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite, err)
	})

	t.Run("expire old invites", func(t *testing.T) {
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo2@example.com",
			getFutureTs(t, 48*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite, err)

		invite2, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo2@example.com",
			getFutureTs(t, 48*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite2, err)
		// Add time here as the expired invites as updated to CURRENT_TIMESTAMP, so this reduces flakiness
		now := time.Now().Add(5 * time.Second)

		oldinvite1, err := s.db.Q.GetAccountInvite(s.ctx, s.db.Db, invite.ID)
		requireNoErrResp(t, oldinvite1, err)
		require.Greater(t, now.Unix(), oldinvite1.ExpiresAt.Time.Unix())
	})

	t.Run("personal not allowed", func(t *testing.T) {
		account, err := s.db.SetPersonalAccount(s.ctx, user.ID, nil)
		requireNoErrResp(t, account, err)

		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo@example.com",
			getFutureTs(t, 1*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireErrResp(t, invite, err)
		forbiddin := husonymerrors.NewForbidden("")
		require.ErrorAs(t, err, &forbiddin)
	})
}

func (s *IntegrationTestSuite) Test_ValidateInviteAddUserToAccount() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	t.Run("accept invite", func(t *testing.T) {
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo2@example.com",
			getFutureTs(t, 24*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite, err)

		user2 := s.setUser(t, s.ctx, "foo2")

		accountId, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx,
			user2.ID,
			invite.Token,
			"foo2@example.com",
			testIdentity(""),
		)
		requireNoErrResp(t, accountId, err)
	})

	t.Run("expired invite", func(t *testing.T) {
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo3@example.com",
			getFutureTs(t, -1*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite, err)

		user3 := s.setUser(t, s.ctx, "foo3")

		verifyResp, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx,
			user3.ID,
			invite.Token,
			"foo3@example.com",
			testIdentity(""),
		)
		require.Error(t, err)
		require.Nil(t, verifyResp)
		forbidden := husonymerrors.NewForbidden("")
		require.ErrorAs(t, err, &forbidden)
	})

	t.Run("incorrect email", func(t *testing.T) {
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx,
			account.ID,
			user.ID,
			"foo4@example.com",
			getFutureTs(t, -1*time.Hour),
			dbViewerRole,
			testIssuer,
		)
		requireNoErrResp(t, invite, err)

		user4 := s.setUser(t, s.ctx, "foo3")

		verifyResp, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx,
			user4.ID,
			invite.Token,
			"blah@example.com",
			testIdentity(""),
		)
		require.Error(t, err)
		require.Nil(t, verifyResp)
		badrequest := husonymerrors.NewBadRequest("")
		require.ErrorAs(t, err, &badrequest)
		t.Log(err.Error())
	})
}

// An invitation is checked before it is honored, whoever presents it: one that is no longer
// acceptable is refused to somebody already in the account exactly as it is to a newcomer, and
// leaves both as they were.
func (s *IntegrationTestSuite) Test_ValidateInvite_CheckedBeforeItIsHonored() {
	t := s.T()
	const email = "guest@example.com"
	adminRole := pgtype.Int4{Int32: int32(mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN), Valid: true}

	sender := s.setUser(t, s.ctx, "checked-sender")
	account, err := s.db.CreateTeamAccount(s.ctx, sender.ID, "checked-team", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)
	newcomer := s.setUser(t, s.ctx, "checked-newcomer")
	member := s.setUser(t, s.ctx, "checked-member")
	require.NoError(t, s.db.Q.CreateAccountUserAssociation(s.ctx, s.db.Db, db_queries.CreateAccountUserAssociationParams{
		AccountID: account.ID,
		UserID:    member.ID,
	}))

	// Each invitation is new, as creating one expires the earlier ones of the address.
	newInvite := func(t *testing.T, expiresIn time.Duration, issuer string) *db_queries.HusonymApiAccountInvite {
		t.Helper()
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx, account.ID, sender.ID, email, getFutureTs(t, expiresIn), adminRole, issuer,
		)
		requireNoErrResp(t, invite, err)
		return invite
	}
	stored := func(t *testing.T, invite *db_queries.HusonymApiAccountInvite) db_queries.HusonymApiAccountInvite {
		t.Helper()
		got, err := s.db.Q.GetAccountInvite(s.ctx, s.db.Db, invite.ID)
		require.NoError(t, err)
		return got
	}
	deployment := husonymdb.Identity{Issuer: testIssuer, Subject: "checked", MayAdoptLegacy: true}

	refusals := []struct {
		name     string
		invite   func(t *testing.T) *db_queries.HusonymApiAccountInvite
		email    string
		identity husonymdb.Identity
		code     connect.Code
		message  string
	}{
		{
			name:     "expired",
			invite:   func(t *testing.T) *db_queries.HusonymApiAccountInvite { return newInvite(t, -time.Hour, testIssuer) },
			email:    email,
			identity: deployment,
			code:     connect.CodePermissionDenied,
			message:  "account invitation expired",
		},
		{
			name: "superseded by a later one",
			invite: func(t *testing.T) *db_queries.HusonymApiAccountInvite {
				earlier := newInvite(t, 24*time.Hour, testIssuer)
				newInvite(t, 24*time.Hour, testIssuer)
				// The earlier one is expired at the time of the database, to the microsecond.
				time.Sleep(10 * time.Millisecond)
				return earlier
			},
			email:    email,
			identity: deployment,
			code:     connect.CodePermissionDenied,
			message:  "account invitation expired",
		},
		{
			name: "already accepted",
			invite: func(t *testing.T) *db_queries.HusonymApiAccountInvite {
				invite := newInvite(t, 24*time.Hour, testIssuer)
				_, err := s.db.Q.UpdateAccountInviteToAccepted(s.ctx, s.db.Db, invite.ID)
				require.NoError(t, err)
				return invite
			},
			email:    email,
			identity: deployment,
			code:     connect.CodeInvalidArgument,
			message:  "account invitation already accepted",
		},
		{
			name:     "for another address",
			invite:   func(t *testing.T) *db_queries.HusonymApiAccountInvite { return newInvite(t, 24*time.Hour, testIssuer) },
			email:    "somebody-else@example.com",
			identity: deployment,
			code:     connect.CodeInvalidArgument,
			message:  "invalid invite email",
		},
		{
			name:     "from another issuer",
			invite:   func(t *testing.T) *db_queries.HusonymApiAccountInvite { return newInvite(t, 24*time.Hour, testIssuer) },
			email:    email,
			identity: husonymdb.Identity{Issuer: "https://hostile.example.com/", Subject: "checked"},
			code:     connect.CodePermissionDenied,
			message:  "identity provider it was issued for",
		},
	}
	for _, refused := range refusals {
		t.Run(refused.name, func(t *testing.T) {
			invite := refused.invite(t)
			before := stored(t, invite)

			var messages []string
			for _, caller := range []*db_queries.HusonymApiUser{newcomer, member} {
				resp, err := s.db.ValidateInviteAddUserToAccount(s.ctx, caller.ID, invite.Token, refused.email, refused.identity)
				requireErrResp(t, resp, err)
				require.Equal(t, refused.code, connect.CodeOf(err), "refused for another reason: %v", err)
				require.Contains(t, err.Error(), refused.message)
				messages = append(messages, err.Error())
			}
			require.Equal(t, messages[0], messages[1], "a member is refused as a newcomer is")

			require.Equal(t, before.Accepted, stored(t, invite).Accepted, "a refusal leaves the invitation as it was")
			require.False(t, s.isMember(t, newcomer.ID, account.ID))
			require.True(t, s.isMember(t, member.ID, account.ID))
		})
	}

	// One that is acceptable is honored for both, with the role it names.
	for name, caller := range map[string]*db_queries.HusonymApiUser{"a newcomer": newcomer, "a member": member} {
		t.Run("acceptable, presented by "+name, func(t *testing.T) {
			invite := newInvite(t, 24*time.Hour, testIssuer)

			resp, err := s.db.ValidateInviteAddUserToAccount(s.ctx, caller.ID, invite.Token, email, deployment)
			requireNoErrResp(t, resp, err)
			require.Equal(t, husonymdb.UUIDString(account.ID), husonymdb.UUIDString(resp.AccountId))
			require.Equal(t, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN, resp.Role)
			require.True(t, stored(t, invite).Accepted.Bool)
			require.True(t, s.isMember(t, caller.ID, account.ID))
		})
	}
}

func (s *IntegrationTestSuite) Test_CreateAccountApiKey() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	key, err := s.db.CreateAccountApikey(s.ctx, &husonymdb.CreateAccountApiKeyRequest{
		KeyName:           "foo",
		KeyValue:          "bar",
		AccountUuid:       account.ID,
		CreatedByUserUuid: user.ID,
		ExpiresAt:         getFutureTs(t, 24*time.Hour),
		Permissions:       []string{"job:view", "connection:view"},
	})
	requireNoErrResp(t, key, err)
	require.Equal(t, []string{"job:view", "connection:view"}, key.Permissions)

	// Given none, a key holds none: an empty list, not a NULL the column would refuse.
	bare, err := s.db.CreateAccountApikey(s.ctx, &husonymdb.CreateAccountApiKeyRequest{
		KeyName:           "bare",
		KeyValue:          "baz",
		AccountUuid:       account.ID,
		CreatedByUserUuid: user.ID,
		ExpiresAt:         getFutureTs(t, 24*time.Hour),
	})
	requireNoErrResp(t, bare, err)
	require.Empty(t, bare.Permissions)
}

func (s *IntegrationTestSuite) Test_CreateJob() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	connection, err := s.db.Q.CreateConnection(s.ctx, s.db.Db, db_queries.CreateConnectionParams{
		Name:             "foo",
		AccountID:        account.ID,
		ConnectionConfig: &pg_models.ConnectionConfig{},
		CreatedByID:      user.ID,
		UpdatedByID:      user.ID,
	})
	requireNoErrResp(t, connection, err)

	job, err := s.db.CreateJob(s.ctx, &db_queries.CreateJobParams{
		Name:      "foo",
		AccountID: account.ID,
		Status:    1,
		ConnectionOptions: &pg_models.JobSourceOptions{
			PostgresOptions: &pg_models.PostgresSourceOptions{
				NewColumnAdditionStrategy: &pg_models.PostgresNewColumnAdditionStrategy{HaltJob: &pg_models.PostgresHaltJobStrategy{}},
			},
		},
		Mappings:           []*pg_models.JobMapping{{Schema: "foo", Table: "bar", Column: "baz"}},
		CronSchedule:       pgtype.Text{String: "blah", Valid: true},
		CreatedByID:        user.ID,
		UpdatedByID:        user.ID,
		WorkflowOptions:    &pg_models.WorkflowOptions{},
		SyncOptions:        &pg_models.ActivityOptions{},
		VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
		JobtypeConfig:      []byte(`{"job_type": {"sync": {}}}`),
	}, []*husonymdb.CreateJobConnectionDestination{
		{
			ConnectionId: connection.ID,
			Options:      &pg_models.JobDestinationOptions{},
		},
	}, nil)
	requireNoErrResp(t, job, err)
}

// The guard of a creation runs in the transaction that writes the job, before the job is
// written, and an error from it leaves nothing written.
func (s *IntegrationTestSuite) Test_CreateJob_Guard() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	params := func(name string) *db_queries.CreateJobParams {
		return &db_queries.CreateJobParams{
			Name:               name,
			AccountID:          account.ID,
			Status:             1,
			ConnectionOptions:  &pg_models.JobSourceOptions{},
			Mappings:           []*pg_models.JobMapping{},
			CreatedByID:        user.ID,
			UpdatedByID:        user.ID,
			WorkflowOptions:    &pg_models.WorkflowOptions{},
			SyncOptions:        &pg_models.ActivityOptions{},
			VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
			JobtypeConfig:      []byte(`{}`),
		}
	}
	storedJobs := func(dbtx husonymdb.BaseDBTX) []string {
		jobs, err := s.db.Q.GetJobsByAccount(s.ctx, dbtx, account.ID)
		require.NoError(t, err)
		names := make([]string, 0, len(jobs))
		for _, job := range jobs {
			names = append(names, job.Name)
		}
		return names
	}

	// What the guard writes through its handle goes with the transaction it refuses.
	refused := husonymerrors.NewForbidden("refused by the guard")
	job, err := s.db.CreateJob(s.ctx, params("refused"), nil, func(ctx context.Context, dbtx husonymdb.BaseDBTX) error {
		_, err := s.db.Q.CreateJob(ctx, dbtx, *params("written-by-the-guard"))
		require.NoError(t, err)
		return refused
	})
	require.ErrorIs(t, err, refused)
	require.Nil(t, job)
	require.Empty(t, storedJobs(s.db.Db))

	var seenByTheGuard []string
	job, err = s.db.CreateJob(s.ctx, params("allowed"), nil, func(_ context.Context, dbtx husonymdb.BaseDBTX) error {
		seenByTheGuard = storedJobs(dbtx)
		return nil
	})
	requireNoErrResp(t, job, err)
	require.Empty(t, seenByTheGuard, "the guard runs before the job is written")
	require.Equal(t, []string{"allowed"}, storedJobs(s.db.Db))
}

// Setting the subsets writes the source options back whole. A change of the source that is
// under way when it starts is not undone: it waits for that change, and subsets the job as the
// change left it.
func (s *IntegrationTestSuite) Test_SetSourceSubsets_KeepsAChangeOfSourceUnderWay() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)
	job, err := s.db.CreateJob(s.ctx, &db_queries.CreateJobParams{
		Name:               "foo",
		AccountID:          account.ID,
		Status:             1,
		ConnectionOptions:  &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: "before"}},
		Mappings:           []*pg_models.JobMapping{},
		CreatedByID:        user.ID,
		UpdatedByID:        user.ID,
		WorkflowOptions:    &pg_models.WorkflowOptions{},
		SyncOptions:        &pg_models.ActivityOptions{},
		VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
		JobtypeConfig:      []byte(`{}`),
	}, nil, nil)
	requireNoErrResp(t, job, err)

	// The change of source, as far as its write: the row is its own until it commits.
	change, err := s.pgcontainer.DB.Begin(s.ctx)
	require.NoError(t, err)
	defer func() { _ = change.Rollback(s.ctx) }()
	_, err = s.db.Q.UpdateJobSource(s.ctx, change, db_queries.UpdateJobSourceParams{
		ID:                job.ID,
		ConnectionOptions: &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: "after"}},
		UpdatedByID:       user.ID,
	})
	require.NoError(t, err)

	subset := make(chan error, 1)
	go func() {
		subset <- s.db.SetSourceSubsets(s.ctx, job.ID, account.ID, &mgmtv1alpha1.JobSourceSqlSubetSchemas{
			Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_PostgresSubset{PostgresSubset: &mgmtv1alpha1.PostgresSourceSchemaSubset{}},
		}, true, user.ID)
	}()
	// Time for the subsets to read the job, if nothing makes them wait for the change.
	select {
	case err := <-subset:
		require.Failf(t, "the subsets were set while the source was being changed", "%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, change.Commit(s.ctx))
	require.NoError(t, <-subset)

	stored, err := s.db.Q.GetJobById(s.ctx, s.db.Db, job.ID)
	require.NoError(t, err)
	require.Equal(t, "after", stored.ConnectionOptions.PostgresOptions.ConnectionId)
	require.True(t, stored.ConnectionOptions.PostgresOptions.SubsetByForeignKeyConstraints)
}

func (s *IntegrationTestSuite) Test_SetSourceSubsets() {
	t := s.T()

	user := s.setUser(t, s.ctx, "foo")
	account, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam1", testutil.GetTestLogger(t))
	requireNoErrResp(t, account, err)

	job, err := s.db.CreateJob(s.ctx, &db_queries.CreateJobParams{
		Name:      "foo",
		AccountID: account.ID,
		Status:    1,
		ConnectionOptions: &pg_models.JobSourceOptions{
			PostgresOptions: &pg_models.PostgresSourceOptions{
				NewColumnAdditionStrategy: &pg_models.PostgresNewColumnAdditionStrategy{HaltJob: &pg_models.PostgresHaltJobStrategy{}},
			},
		},
		Mappings:           []*pg_models.JobMapping{{Schema: "foo", Table: "bar", Column: "baz"}},
		CronSchedule:       pgtype.Text{String: "blah", Valid: true},
		CreatedByID:        user.ID,
		UpdatedByID:        user.ID,
		WorkflowOptions:    &pg_models.WorkflowOptions{},
		SyncOptions:        &pg_models.ActivityOptions{},
		VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
		JobtypeConfig:      []byte(`{"job_type": {"sync": {}}}`),
	}, []*husonymdb.CreateJobConnectionDestination{}, nil)
	requireNoErrResp(t, job, err)

	where := "blah"

	t.Run("postgres", func(t *testing.T) {
		err := s.db.SetSourceSubsets(s.ctx, job.ID, account.ID, &mgmtv1alpha1.JobSourceSqlSubetSchemas{
			Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_PostgresSubset{
				PostgresSubset: &mgmtv1alpha1.PostgresSourceSchemaSubset{
					PostgresSchemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{
						{
							Schema: "foo",
							Tables: []*mgmtv1alpha1.PostgresSourceTableOption{
								{Table: "foo", WhereClause: &where},
							},
						},
					},
				},
			},
		}, false, user.ID)
		require.NoError(t, err)
	})

	t.Run("mysql", func(t *testing.T) {
		err := s.db.SetSourceSubsets(s.ctx, job.ID, account.ID, &mgmtv1alpha1.JobSourceSqlSubetSchemas{
			Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_MysqlSubset{
				MysqlSubset: &mgmtv1alpha1.MysqlSourceSchemaSubset{
					MysqlSchemas: []*mgmtv1alpha1.MysqlSourceSchemaOption{
						{
							Schema: "foo",
							Tables: []*mgmtv1alpha1.MysqlSourceTableOption{
								{Table: "foo", WhereClause: &where},
							},
						},
					},
				},
			},
		}, false, user.ID)
		require.NoError(t, err)
	})

	t.Run("mssql", func(t *testing.T) {
		err := s.db.SetSourceSubsets(s.ctx, job.ID, account.ID, &mgmtv1alpha1.JobSourceSqlSubetSchemas{
			Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_MssqlSubset{
				MssqlSubset: &mgmtv1alpha1.MssqlSourceSchemaSubset{
					MssqlSchemas: []*mgmtv1alpha1.MssqlSourceSchemaOption{
						{
							Schema: "foo",
							Tables: []*mgmtv1alpha1.MssqlSourceTableOption{
								{Table: "foo", WhereClause: &where},
							},
						},
					},
				},
			},
		}, false, user.ID)
		require.NoError(t, err)
	})

	t.Run("dynamodb", func(t *testing.T) {
		err := s.db.SetSourceSubsets(s.ctx, job.ID, account.ID, &mgmtv1alpha1.JobSourceSqlSubetSchemas{
			Schemas: &mgmtv1alpha1.JobSourceSqlSubetSchemas_DynamodbSubset{
				DynamodbSubset: &mgmtv1alpha1.DynamoDBSourceSchemaSubset{
					Tables: []*mgmtv1alpha1.DynamoDBSourceTableOption{
						{Table: "foo", WhereClause: &where},
					},
				},
			},
		}, false, user.ID)
		require.NoError(t, err)
	})
}

// The property this whole change exists for: whoever declares a provider controls the
// subjects it issues, so the same subject from two providers must be two people.
func (s *IntegrationTestSuite) Test_SetUserByIdentity_IssuerIsPartOfTheKey() {
	t := s.T()
	const sharedSub = "1234567890"

	t.Run("the same subject from two issuers is two users", func(t *testing.T) {
		first, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: "https://first.example.com/", Subject: sharedSub,
		}, nil)
		requireNoErrResp(t, first, err)

		second, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: "https://second.example.com/", Subject: sharedSub,
		}, nil)
		requireNoErrResp(t, second, err)

		require.NotEqual(
			t,
			husonymdb.UUIDString(first.ID),
			husonymdb.UUIDString(second.ID),
			"a hostile provider minting an existing subject must not be handed that user",
		)
	})

	t.Run("the same identity twice is one user", func(t *testing.T) {
		identity := husonymdb.Identity{Issuer: "https://first.example.com/", Subject: "stable"}

		first, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
		requireNoErrResp(t, first, err)
		second, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
		requireNoErrResp(t, second, err)

		require.Equal(t, husonymdb.UUIDString(first.ID), husonymdb.UUIDString(second.ID))
	})
}

// A row written before this migration names no issuer. It belongs to whoever the
// deployment was pointed at then, and to nobody else.
func (s *IntegrationTestSuite) Test_SetUserByIdentity_LegacyAdoption() {
	t := s.T()

	legacyUser := func(t *testing.T, sub string) pgtype.UUID {
		t.Helper()
		user, err := s.db.Q.CreateNonMachineUser(s.ctx, s.db.Db)
		require.NoError(t, err)
		_, err = s.db.Q.CreateIdentityProviderAssociation(s.ctx, s.db.Db, db_queries.CreateIdentityProviderAssociationParams{
			UserID:      user.ID,
			ProviderSub: sub,
			ProviderIss: "",
		})
		require.NoError(t, err)
		return user.ID
	}

	t.Run("the deployment's own issuer adopts it, keeping the user", func(t *testing.T) {
		sub := "legacy-adopted"
		existing := legacyUser(t, sub)

		got, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: testIssuer, Subject: sub, MayAdoptLegacy: true,
		}, nil)
		requireNoErrResp(t, got, err)
		require.Equal(
			t,
			husonymdb.UUIDString(existing),
			husonymdb.UUIDString(got.ID),
			"adoption must keep the user, not make a second one",
		)

		association, err := s.db.Q.GetUserAssociationByIdentity(s.ctx, s.db.Db, db_queries.GetUserAssociationByIdentityParams{
			ProviderSub: sub, ProviderIss: testIssuer,
		})
		require.NoError(t, err)
		require.Equal(t, testIssuer, association.ProviderIss, "adoption must be recorded")
	})

	t.Run("any other issuer is refused", func(t *testing.T) {
		sub := "legacy-refused"
		existing := legacyUser(t, sub)

		_, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: "https://hostile.example.com/", Subject: sub, MayAdoptLegacy: false,
		}, nil)
		require.ErrorIs(t, err, husonymdb.ErrIdentityNotAdoptable)

		// And it changed nothing.
		association, err := s.db.Q.GetUserAssociationByIdentity(s.ctx, s.db.Db, db_queries.GetUserAssociationByIdentityParams{
			ProviderSub: sub, ProviderIss: "",
		})
		require.NoError(t, err)
		require.Empty(t, association.ProviderIss)
		require.Equal(t, husonymdb.UUIDString(existing), husonymdb.UUIDString(association.UserID))
	})

	t.Run("adoption happens once, and the second issuer gets its own user", func(t *testing.T) {
		sub := "legacy-then-other"
		legacyUser(t, sub)

		adopted, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: testIssuer, Subject: sub, MayAdoptLegacy: true,
		}, nil)
		requireNoErrResp(t, adopted, err)

		other, err := s.db.SetUserByIdentity(s.ctx, husonymdb.Identity{
			Issuer: "https://other.example.com/", Subject: sub,
		}, nil)
		requireNoErrResp(t, other, err)

		require.NotEqual(t, husonymdb.UUIDString(adopted.ID), husonymdb.UUIDString(other.ID))
	})
}

// The review of PR #76 named this one: the association kept pointing at a user that no
// longer existed, so every sign-in made another orphan.
func (s *IntegrationTestSuite) Test_SetUserByIdentity_ReplacesAMissingUser() {
	t := s.T()
	identity := husonymdb.Identity{Issuer: testIssuer, Subject: "user-vanished"}

	first, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
	requireNoErrResp(t, first, err)

	_, err = s.db.Db.Exec(s.ctx, "DELETE FROM husonym_api.users WHERE id = $1", first.ID)
	require.NoError(t, err)

	second, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
	requireNoErrResp(t, second, err)
	require.NotEqual(t, husonymdb.UUIDString(first.ID), husonymdb.UUIDString(second.ID))

	third, err := s.db.SetUserByIdentity(s.ctx, identity, nil)
	requireNoErrResp(t, third, err)
	require.Equal(
		t,
		husonymdb.UUIDString(second.ID),
		husonymdb.UUIDString(third.ID),
		"the association must follow the replacement, or every sign-in makes another orphan",
	)
}

// An invitation says which person; the issuer says whose word we take for it.
func (s *IntegrationTestSuite) Test_ValidateInvite_BoundToItsIssuer() {
	t := s.T()

	newInvite := func(t *testing.T, email, issuer string) (string, pgtype.UUID) {
		t.Helper()
		sender := s.setUser(t, s.ctx, "invite-sender-"+email)
		account, err := s.db.CreateTeamAccount(s.ctx, sender.ID, "team-"+email, testutil.GetTestLogger(t))
		requireNoErrResp(t, account, err)
		invite, err := s.db.CreateTeamAccountInvite(
			s.ctx, account.ID, sender.ID, email, getFutureTs(t, time.Hour), dbViewerRole, issuer,
		)
		requireNoErrResp(t, invite, err)
		return invite.Token, account.ID
	}

	t.Run("the issuer it was created for accepts it", func(t *testing.T) {
		token, accountId := newInvite(t, "a@example.com", testIssuer)
		guest := s.setUser(t, s.ctx, "guest-a")

		resp, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx, guest.ID, token, "a@example.com",
			husonymdb.Identity{Issuer: testIssuer, Subject: "guest-a"},
		)
		requireNoErrResp(t, resp, err)
		require.Equal(t, husonymdb.UUIDString(accountId), husonymdb.UUIDString(resp.AccountId))
	})

	// The takeover the review found: another provider vouching for the same address.
	t.Run("another issuer is refused, same address", func(t *testing.T) {
		token, _ := newInvite(t, "b@example.com", testIssuer)
		guest := s.setUser(t, s.ctx, "guest-b")

		_, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx, guest.ID, token, "b@example.com",
			husonymdb.Identity{Issuer: "https://hostile.example.com/", Subject: "guest-b"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "identity provider it was issued for")
	})

	t.Run("an invitation naming no issuer is accepted only by the deployment's own", func(t *testing.T) {
		token, _ := newInvite(t, "c@example.com", "")
		guest := s.setUser(t, s.ctx, "guest-c")

		_, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx, guest.ID, token, "c@example.com",
			husonymdb.Identity{Issuer: "https://hostile.example.com/", Subject: "guest-c", MayAdoptLegacy: false},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "predates issuer recording")

		resp, err := s.db.ValidateInviteAddUserToAccount(
			s.ctx, guest.ID, token, "c@example.com",
			husonymdb.Identity{Issuer: testIssuer, Subject: "guest-c", MayAdoptLegacy: true},
		)
		requireNoErrResp(t, resp, err)
	})
}
