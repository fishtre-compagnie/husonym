package v1alpha1_useraccountservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	authjwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/tokenctx"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/version"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) GetUser(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	if !s.cfg.IsAuthEnabled {
		// intentionally ignoring error here because we are in unauth mode anyways
		// but if it's available, let's return the api key's user id
		apiTokenCtxData, _ := auth_apikey.GetTokenDataFromCtx(ctx)
		if apiTokenCtxData != nil {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{
				UserId: husonymdb.UUIDString(apiTokenCtxData.ApiKey.UserID),
			}), nil
		}
		user, err := s.db.Q.GetAnonymousUser(ctx, s.db.Db)
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, husonymerrors.New(err)
		} else if err != nil && husonymdb.IsNoRows(err) {
			user, err = s.db.Q.SetAnonymousUser(ctx, s.db.Db)
			if err != nil {
				return nil, err
			}
		}
		return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{
			UserId: husonymdb.UUIDString(user.ID),
		}), nil
	}

	tokenctxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil {
		return nil, err
	}

	if tokenctxResp.ApiKeyContextData != nil {
		if tokenctxResp.ApiKeyContextData.ApiKeyType == apikey.AccountApiKey &&
			tokenctxResp.ApiKeyContextData.ApiKey != nil {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{
				UserId: husonymdb.UUIDString(tokenctxResp.ApiKeyContextData.ApiKey.UserID),
			}), nil
		} else if tokenctxResp.ApiKeyContextData.ApiKeyType == apikey.WorkerApiKey {
			return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{
				UserId: "00000000-0000-0000-0000-000000000000",
			}), nil
		}
		return nil, husonymerrors.NewUnauthenticated(
			fmt.Sprintf(
				"invalid api key type when calling GetUser: %s",
				tokenctxResp.ApiKeyContextData.ApiKeyType,
			),
		)
	} else if tokenctxResp.JwtContextData != nil {
		identity := s.identityOf(tokenctxResp.JwtContextData)
		user, err := s.db.Q.GetUserAssociationByIdentity(ctx, s.db.Db, db_queries.GetUserAssociationByIdentityParams{
			ProviderSub: identity.Subject,
			ProviderIss: identity.Issuer,
		})
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, husonymerrors.New(err)
		} else if err != nil && husonymdb.IsNoRows(err) {
			return nil, husonymerrors.NewNotFound("unable to find user")
		}
		// A row recorded before issuers were is only this user's if the deployment's own
		// issuer is the one presenting it. Adopting it is SetUser's job, not this one's.
		if user.ProviderIss == "" && !identity.MayAdoptLegacy {
			return nil, husonymerrors.NewNotFound("unable to find user")
		}

		return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{
			UserId: husonymdb.UUIDString(user.UserID),
		}), nil
	}
	return nil, husonymerrors.NewUnauthenticated(
		"unable to find a valid user based on the provided auth credentials",
	)
}

func (s *Service) SetUser(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetUserRequest],
) (*connect.Response[mgmtv1alpha1.SetUserResponse], error) {
	if !s.cfg.IsAuthEnabled {
		// intentionally ignoring error here because we are in unauth mode anyways
		// but if it's available, let's return the api key's user id
		apiTokenCtxData, _ := auth_apikey.GetTokenDataFromCtx(ctx)
		if apiTokenCtxData != nil {
			return connect.NewResponse(&mgmtv1alpha1.SetUserResponse{
				UserId: husonymdb.UUIDString(apiTokenCtxData.ApiKey.UserID),
			}), nil
		}
		user, err := s.db.Q.SetAnonymousUser(ctx, s.db.Db)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&mgmtv1alpha1.SetUserResponse{
			UserId: husonymdb.UUIDString(user.ID),
		}), nil
	}

	tokenctxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil {
		return nil, err
	}
	if tokenctxResp.ApiKeyContextData != nil {
		return connect.NewResponse(&mgmtv1alpha1.SetUserResponse{
			UserId: husonymdb.UUIDString(tokenctxResp.ApiKeyContextData.ApiKey.UserID),
		}), nil
	} else if tokenctxResp.JwtContextData != nil {
		tokenCtxData, err := authjwt.GetTokenDataFromCtx(ctx)
		if err != nil {
			return nil, husonymerrors.New(err)
		}

		if err := s.refuseApplicationToken(tokenCtxData); err != nil {
			return nil, err
		}

		user, err := s.db.SetUserByIdentity(
			ctx,
			s.identityOf(tokenCtxData),
			s.resolveIdentityProfile(ctx, tokenCtxData),
		)
		if err != nil {
			return nil, husonymerrors.New(err)
		}

		return connect.NewResponse(&mgmtv1alpha1.SetUserResponse{
			UserId: husonymdb.UUIDString(user.ID),
		}), nil
	}
	return nil, husonymerrors.NewUnauthenticated(
		"unable to find a valid user based on the provided auth credentials",
	)
}

func (s *Service) GetUserAccounts(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUserAccountsRequest],
) (*connect.Response[mgmtv1alpha1.GetUserAccountsResponse], error) {
	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}
	userId, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	accounts, err := s.db.Q.GetAccountsByUser(ctx, s.db.Db, userId)
	if err != nil {
		return nil, err
	}

	dtoAccounts := []*mgmtv1alpha1.UserAccount{}
	for index := range accounts {
		dtoAccounts = append(dtoAccounts, dtomaps.ToUserAccount(&accounts[index]))
	}

	return connect.NewResponse(&mgmtv1alpha1.GetUserAccountsResponse{
		Accounts: dtoAccounts,
	}), nil
}

func (s *Service) ConvertPersonalToTeamAccount(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ConvertPersonalToTeamAccountRequest],
) (*connect.Response[mgmtv1alpha1.ConvertPersonalToTeamAccountResponse], error) {
	if !s.cfg.IsAuthEnabled {
		return nil, husonymerrors.NewForbidden(
			"unable to convert personal account to team account as authentication is not enabled",
		)
	}

	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}
	userId, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	personalAccountId := req.Msg.GetAccountId()
	if personalAccountId == "" {
		logger.Debug(
			"account id was not provided during personal->team conversion. Attempting to find personal account",
		)
		accounts, err := s.db.Q.GetAccountsByUser(ctx, s.db.Db, userId)
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, err
		} else if err != nil && husonymdb.IsNoRows(err) {
			return nil, husonymerrors.NewNotFound("user has no accounts")
		}

		for idx := range accounts {
			if accounts[idx].AccountType == int16(husonymdb.AccountType_Personal) {
				personalAccountId = husonymdb.UUIDString(accounts[idx].ID)
				logger.Debug(
					"found personal account to convert to team account",
					"personalAccountId",
					personalAccountId,
				)
				break
			}
		}
	} else {
		personalAccountUuid, err := husonymdb.ToUuid(personalAccountId)
		if err != nil {
			return nil, err
		}
		count, err := s.db.Q.IsUserInAccount(ctx, s.db.Db, db_queries.IsUserInAccountParams{
			AccountId: personalAccountUuid,
			UserId:    userId,
		})
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, husonymerrors.NewNotFound("user is not in the provided account")
		}
		account, err := s.db.Q.GetAccount(ctx, s.db.Db, personalAccountUuid)
		if err != nil {
			return nil, err
		}
		if account.AccountType != int16(husonymdb.AccountType_Personal) {
			return nil, husonymerrors.NewNotFound("account is not a personal account")
		}
	}

	personalAccountUuid, err := husonymdb.ToUuid(personalAccountId)
	if err != nil {
		return nil, err
	}
	resp, err := s.db.ConvertPersonalToTeamAccount(
		ctx,
		&husonymdb.ConvertPersonalToTeamAccountRequest{
			UserId:            userId,
			PersonalAccountId: personalAccountUuid,
			TeamName:          req.Msg.GetName(),
		},
		logger,
	)
	if err != nil {
		return nil, err
	}

	newPersonalAccountId := husonymdb.UUIDString(resp.PersonalAccount.ID)
	if err := s.setRole(
		ctx,
		rbac.NewUser(user.Msg.GetUserId()),
		rbac.NewAccount(newPersonalAccountId),
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	); err != nil {
		// note: if this fails the account is kind of in a broken state...
		return nil, fmt.Errorf(
			"unable to set account role for user in new personal account, please reach out to support for further assistance: %w",
			err,
		)
	}

	return connect.NewResponse(&mgmtv1alpha1.ConvertPersonalToTeamAccountResponse{
		AccountId:            husonymdb.UUIDString(resp.TeamAccount.ID),
		NewPersonalAccountId: husonymdb.UUIDString(resp.PersonalAccount.ID),
	}), nil
}

func (s *Service) SetPersonalAccount(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetPersonalAccountRequest],
) (*connect.Response[mgmtv1alpha1.SetPersonalAccountResponse], error) {
	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}

	userId, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	account, err := s.db.SetPersonalAccount(ctx, userId, s.cfg.DefaultMaxAllowedRecords)
	if err != nil {
		return nil, err
	}

	if err := s.setRole(
		ctx,
		rbac.NewUser(user.Msg.GetUserId()),
		rbac.NewAccount(husonymdb.UUIDString(account.ID)),
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	); err != nil {
		// note: if this fails the account is kind of in a broken state...
		return nil, fmt.Errorf(
			"unable to set account role for user, please reach out to support for further assistance: %w",
			err,
		)
	}

	return connect.NewResponse(&mgmtv1alpha1.SetPersonalAccountResponse{
		AccountId: husonymdb.UUIDString(account.ID),
	}), nil
}

func (s *Service) IsUserInAccount(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}

	userId, err := husonymdb.ToUuid(user.Msg.UserId)
	if err != nil {
		return nil, err
	}
	accountId, err := husonymdb.ToUuid(req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	apiKeyCount, err := s.db.Q.IsUserInAccountApiKey(
		ctx,
		s.db.Db,
		db_queries.IsUserInAccountApiKeyParams{
			AccountId: accountId,
			UserId:    userId,
		},
	)
	if err != nil {
		return nil, err
	}
	if apiKeyCount > 0 {
		return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{
			Ok: apiKeyCount > 0,
		}), nil
	}
	count, err := s.db.Q.IsUserInAccount(ctx, s.db.Db, db_queries.IsUserInAccountParams{
		AccountId: accountId,
		UserId:    userId,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{
		Ok: count > 0,
	}), nil
}

func (s *Service) CreateTeamAccount(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.CreateTeamAccountRequest],
) (*connect.Response[mgmtv1alpha1.CreateTeamAccountResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	if !s.cfg.IsAuthEnabled {
		return nil, husonymerrors.NewForbidden(
			"unable to create team account as authentication is not enabled",
		)
	}

	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}
	userId, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	account, err := s.db.CreateTeamAccount(ctx, userId, req.Msg.GetName(), logger)
	if err != nil {
		return nil, err
	}

	if err := s.setRole(
		ctx,
		rbac.NewUser(user.Msg.GetUserId()),
		rbac.NewAccount(husonymdb.UUIDString(account.ID)),
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	); err != nil {
		// note: if this fails the account is kind of in a broken state...
		return nil, fmt.Errorf(
			"unable to set account role for user, please reach out to support for further assistance: %w",
			err,
		)
	}

	return connect.NewResponse(&mgmtv1alpha1.CreateTeamAccountResponse{
		AccountId: husonymdb.UUIDString(account.ID),
	}), nil
}

func (s *Service) GetTeamAccountMembers(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetTeamAccountMembersRequest],
) (*connect.Response[mgmtv1alpha1.GetTeamAccountMembersResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.AccountId)
	if err != nil {
		return nil, err
	}

	if err := s.verifyTeamAccount(ctx, accountUuid); err != nil {
		return nil, err
	}

	userIdentities, err := s.db.Q.GetUserIdentitiesByTeamAccount(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}

	rbacUsers := make([]rbac.User, 0, len(userIdentities))
	for i := range userIdentities {
		rbacUsers = append(rbacUsers, rbac.NewPgUser(userIdentities[i].UserID))
	}

	userRoles := s.rbacClient.Roles(rbacUsers, rbac.NewAccount(husonymdb.UUIDString(accountUuid)))
	logger.Debug(fmt.Sprintf("found %d users with roles", len(userRoles)))

	dtoUsers := make([]*mgmtv1alpha1.AccountUser, len(userIdentities))
	group := new(errgroup.Group)
	for i := range userIdentities {
		i := i
		user := userIdentities[i]
		group.Go(func() error {
			dtoUsers[i] = &mgmtv1alpha1.AccountUser{
				Id: husonymdb.UUIDString(user.UserID),
			}
			// A member without a role has the unspecified one.
			dtoUsers[i].Role = userRoles[rbac.NewPgUser(user.UserID)]
			// What the provider said at sign-in, stored on the association. This is the
			// nominal path, and it is the same for every OIDC provider.
			identity := &authmgmt.User{
				Name:    user.Name.String,
				Email:   user.Email.String,
				Picture: user.Picture.String,
			}

			// A blank field is completed from the deployment's administration API, which
			// only Auth0 and Keycloak have. It may only ever add -- see
			// completeDisplayIdentity.
			//
			// Only for an identity the deployment's own provider issued. That API speaks
			// for one provider and knows subjects in its namespace alone, so asking it
			// about a subject another provider minted would answer about whoever happens
			// to carry that subject there -- and show one person's name and address in
			// another's place. The same reason identities carry their issuer at all.
			if !isDisplayIdentityComplete(identity) && s.isDeploymentIssuerOrLegacy(user.ProviderIss) {
				if user.ProviderSub == "" {
					logger.Warn(
						fmt.Sprintf(
							"unable to find provider sub associated with user id: %q",
							husonymdb.UUIDString(user.UserID),
						),
					)
				} else if authuser, err := s.authadminclient.GetUserBySub(ctx, user.ProviderSub); err != nil {
					// Not fatal: what was stored is still shown.
					logger.Warn(fmt.Sprintf("unable to retrieve user by sub: %s", err.Error()))
				} else {
					identity = completeDisplayIdentity(identity, authuser)
				}
			}

			dtoUsers[i].Email = identity.Email
			dtoUsers[i].Name = identity.Name
			dtoUsers[i].Image = identity.Picture
			return nil
		})
	}
	err = group.Wait()
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.GetTeamAccountMembersResponse{
		Users: dtoUsers,
	}), nil
}

func (s *Service) RemoveTeamAccountMember(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RemoveTeamAccountMemberRequest],
) (*connect.Response[mgmtv1alpha1.RemoveTeamAccountMemberResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	if err := s.verifyTeamAccount(ctx, accountUuid); err != nil {
		return nil, err
	}
	memberUserId, err := husonymdb.ToUuid(req.Msg.UserId)
	if err != nil {
		return nil, err
	}
	err = s.db.Q.RemoveAccountUser(ctx, s.db.Db, db_queries.RemoveAccountUserParams{
		AccountId: accountUuid,
		UserId:    memberUserId,
	})
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, fmt.Errorf("unable to remove account user from db: %w", err)
	}

	if err := s.rbacClient.RemoveMember(
		ctx,
		rbac.NewPgUser(memberUserId),
		rbac.NewAccount(husonymdb.UUIDString(accountUuid)),
	); err != nil {
		return nil, fmt.Errorf("unable to remove account user from rbac engine: %w", err)
	}

	return connect.NewResponse(&mgmtv1alpha1.RemoveTeamAccountMemberResponse{}), nil
}

func (s *Service) InviteUserToTeamAccount(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.InviteUserToTeamAccountRequest],
) (*connect.Response[mgmtv1alpha1.InviteUserToTeamAccountResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	if err := s.verifyTeamAccount(ctx, accountUuid); err != nil {
		return nil, err
	}
	// An invitation that names no role is a plain invitation, and the member is given the
	// viewer role when accepting it: only a role chosen at invitation time is the feature.
	if err := enforceRbacForRole(ctx, user, req.Msg.GetAccountId(), req.Msg.GetRole()); err != nil {
		return nil, err
	}

	tomorrow := time.Now().Add(24 * time.Hour)
	expiresAt, err := husonymdb.ToTimestamp(tomorrow)
	if err != nil {
		return nil, err
	}

	var role pgtype.Int4
	if req.Msg.GetRole() != mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED {
		role = pgtype.Int4{Int32: int32(req.Msg.GetRole()), Valid: true}
	}

	invite, err := s.db.CreateTeamAccountInvite(
		ctx,
		accountUuid,
		user.PgId(),
		req.Msg.GetEmail(),
		expiresAt,
		role,
		// The issuer the invitation may be accepted from. It is a property of the
		// account, not of whoever sends the invitation -- which is why it is the
		// deployment's issuer and not the sender's, and why it will become the account's
		// own the day an account declares one.
		s.cfg.DeploymentIssuer,
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.InviteUserToTeamAccountResponse{
		Invite: dtomaps.ToAccountInviteDto(invite),
	}), nil
}

func (s *Service) GetTeamAccountInvites(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetTeamAccountInvitesRequest],
) (*connect.Response[mgmtv1alpha1.GetTeamAccountInvitesResponse], error) {
	userdataclient := userdata.NewClient(s, s.rbacClient, s.licenseclient)
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	if err := s.verifyTeamAccount(ctx, accountUuid); err != nil {
		return nil, err
	}

	invites, err := s.db.Q.GetActiveAccountInvites(ctx, s.db.Db, accountUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, husonymerrors.New(err)
	} else if err != nil && husonymdb.IsNoRows(err) {
		return connect.NewResponse(&mgmtv1alpha1.GetTeamAccountInvitesResponse{
			Invites: []*mgmtv1alpha1.AccountInvite{},
		}), nil
	}

	dtos := []*mgmtv1alpha1.AccountInvite{}
	for index := range invites {
		dtos = append(dtos, dtomaps.ToAccountInviteDto(&invites[index]))
	}

	return connect.NewResponse(&mgmtv1alpha1.GetTeamAccountInvitesResponse{
		Invites: dtos,
	}), nil
}

func (s *Service) RemoveTeamAccountInvite(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RemoveTeamAccountInviteRequest],
) (*connect.Response[mgmtv1alpha1.RemoveTeamAccountInviteResponse], error) {
	inviteId, err := husonymdb.ToUuid(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	invite, err := s.db.Q.GetAccountInvite(ctx, s.db.Db, inviteId)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, husonymerrors.New(err)
	} else if err != nil && husonymdb.IsNoRows(err) {
		return connect.NewResponse(&mgmtv1alpha1.RemoveTeamAccountInviteResponse{}), nil
	}

	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(husonymdb.UUIDString(invite.AccountID)),
		rbac.AccountAction_Edit,
	); err != nil {
		return nil, err
	}

	if err := s.verifyTeamAccount(ctx, invite.AccountID); err != nil {
		return nil, err
	}

	err = s.db.Q.RemoveAccountInvite(ctx, s.db.Db, inviteId)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, husonymerrors.New(err)
	}

	return connect.NewResponse(&mgmtv1alpha1.RemoveTeamAccountInviteResponse{}), nil
}

func (s *Service) AcceptTeamAccountInvite(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.AcceptTeamAccountInviteRequest],
) (*connect.Response[mgmtv1alpha1.AcceptTeamAccountInviteResponse], error) {
	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}
	userUuid, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	tokenctxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil {
		return nil, err
	}
	if tokenctxResp.JwtContextData == nil {
		return nil, husonymerrors.NewUnauthenticated(
			"must be a valid jwt user to accept team account invites",
		)
	}

	// An invitation is matched on an email address, and an address is only worth matching
	// on if the provider vouched for it. Without that, an account that declares its own
	// provider mints a token carrying somebody else's address and walks into an account it
	// was never invited to. The provider that vouches has to be the right one too, which
	// is what the issuer on the invitation is for.
	profile := s.resolveIdentityProfile(ctx, tokenctxResp.JwtContextData)
	if profile == nil || profile.Email == "" {
		return nil, husonymerrors.NewUnauthenticated(
			"unable to find the email address this invitation would be accepted for",
		)
	}
	if !profile.EmailVerified {
		return nil, husonymerrors.NewForbidden(
			"the identity provider does not assert that this email address is verified, so it cannot be used to accept an invitation",
		)
	}

	validateResp, err := s.db.ValidateInviteAddUserToAccount(
		ctx,
		userUuid,
		req.Msg.Token,
		profile.Email,
		s.identityOf(tokenctxResp.JwtContextData),
	)
	if err != nil {
		return nil, err
	}

	if err := s.setRole(
		ctx,
		rbac.NewUser(user.Msg.GetUserId()),
		rbac.NewAccount(husonymdb.UUIDString(validateResp.AccountId)),
		validateResp.Role,
	); err != nil {
		return nil, fmt.Errorf(
			"unable to set account role for user, please reach out to support for further assistance: %w",
			err,
		)
	}

	if err := s.verifyTeamAccount(ctx, validateResp.AccountId); err != nil {
		return nil, err
	}

	account, err := s.db.Q.GetAccount(ctx, s.db.Db, validateResp.AccountId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.AcceptTeamAccountInviteResponse{
		Account: dtomaps.ToUserAccount(&account),
	}), nil
}

func (s *Service) SetUserRole(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetUserRoleRequest],
) (*connect.Response[mgmtv1alpha1.SetUserRoleResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	requestingUserUuid, err := husonymdb.ToUuid(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	if err := enforceRbacForRole(ctx, user, req.Msg.GetAccountId(), req.Msg.GetRole()); err != nil {
		return nil, err
	}

	count, err := s.db.Q.IsUserInAccount(ctx, s.db.Db, db_queries.IsUserInAccountParams{
		AccountId: accountUuid,
		UserId:    requestingUserUuid,
	})
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, husonymerrors.NewBadRequest("provided user id is not in account")
	}

	err = s.setRole(
		ctx,
		rbac.NewPgUser(requestingUserUuid),
		rbac.NewAccount(husonymdb.UUIDString(accountUuid)),
		req.Msg.GetRole(),
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.SetUserRoleResponse{}), nil
}

func (s *Service) verifyTeamAccount(ctx context.Context, accountId pgtype.UUID) error {
	account, err := s.db.Q.GetAccount(ctx, s.db.Db, accountId)
	if err != nil {
		return err
	}
	if account.AccountType != int16(husonymdb.AccountType_Team) &&
		account.AccountType != int16(husonymdb.AccountType_Enterprise) {
		return husonymerrors.NewForbidden("account is not a team account")
	}
	return nil
}

func (s *Service) GetSystemInformation(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetSystemInformationRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemInformationResponse], error) {
	versionInfo := version.Get()
	builtDate, err := time.Parse(time.RFC3339, versionInfo.BuildDate)
	if err != nil {
		return nil, fmt.Errorf("unable to parse build date: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemInformationResponse{
		Version:   versionInfo.GitVersion,
		Commit:    versionInfo.GitCommit,
		Compiler:  versionInfo.Compiler,
		Platform:  versionInfo.Platform,
		BuildDate: timestamppb.New(builtDate),
		License:   s.systemLicense(ctx),
	}), nil
}

func (s *Service) HasPermission(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.HasPermissionRequest],
) (*connect.Response[mgmtv1alpha1.HasPermissionResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	if err := user.EnforceAccountAccess(ctx, req.Msg.GetAccountId()); err != nil {
		return nil, err
	}

	hasPermission := false
	switch req.Msg.GetResource().GetType() {
	case mgmtv1alpha1.ResourcePermission_TYPE_ACCOUNT:
		if req.Msg.GetResource().GetId() != req.Msg.GetAccountId() {
			return connect.NewResponse(
				&mgmtv1alpha1.HasPermissionResponse{HasPermission: false},
			), nil
		}
		switch req.Msg.GetResource().GetAction() {
		case mgmtv1alpha1.ResourcePermission_ACTION_CREATE:
			ok, err := user.Account(
				ctx,
				userdata.NewIdentifier(req.Msg.GetAccountId()),
				rbac.AccountAction_Create,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_READ:
			ok, err := user.Account(
				ctx,
				userdata.NewIdentifier(req.Msg.GetAccountId()),
				rbac.AccountAction_View,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_UPDATE:
			ok, err := user.Account(
				ctx,
				userdata.NewIdentifier(req.Msg.GetAccountId()),
				rbac.AccountAction_Edit,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		}
	case mgmtv1alpha1.ResourcePermission_TYPE_CONNECTION:
		switch req.Msg.GetResource().GetAction() {
		case mgmtv1alpha1.ResourcePermission_ACTION_CREATE:
			ok, err := user.Connection(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.ConnectionAction_Create,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_READ:
			ok, err := user.Connection(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.ConnectionAction_View,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_UPDATE:
			ok, err := user.Connection(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.ConnectionAction_Edit,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_DELETE:
			ok, err := user.Connection(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.ConnectionAction_Delete,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		}
	case mgmtv1alpha1.ResourcePermission_TYPE_JOB:
		switch req.Msg.GetResource().GetAction() {
		case mgmtv1alpha1.ResourcePermission_ACTION_CREATE:
			ok, err := user.Job(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.JobAction_Create,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_READ:
			ok, err := user.Job(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.JobAction_View,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_UPDATE:
			ok, err := user.Job(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.JobAction_Edit,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		case mgmtv1alpha1.ResourcePermission_ACTION_DELETE:
			ok, err := user.Job(
				ctx,
				userdata.NewDomainEntity(req.Msg.GetAccountId(), req.Msg.GetResource().GetId()),
				rbac.JobAction_Delete,
			)
			if err != nil {
				return nil, err
			}
			hasPermission = ok
		}
	}
	return connect.NewResponse(
		&mgmtv1alpha1.HasPermissionResponse{HasPermission: hasPermission},
	), nil
}

func (s *Service) HasPermissions(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.HasPermissionsRequest],
) (*connect.Response[mgmtv1alpha1.HasPermissionsResponse], error) {
	permissions := make([]bool, len(req.Msg.GetResources()))
	mu := &sync.Mutex{}

	g, errctx := errgroup.WithContext(ctx)
	g.SetLimit(10)

	for i, resource := range req.Msg.GetResources() {
		i, resource := i, resource // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			resp, err := s.HasPermission(
				errctx,
				connect.NewRequest(&mgmtv1alpha1.HasPermissionRequest{
					AccountId: req.Msg.GetAccountId(),
					Resource:  resource,
				}),
			)
			if err != nil {
				return err
			}

			mu.Lock()
			permissions[i] = resp.Msg.GetHasPermission()
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.HasPermissionsResponse{
		Assertions: permissions,
	}), nil
}
