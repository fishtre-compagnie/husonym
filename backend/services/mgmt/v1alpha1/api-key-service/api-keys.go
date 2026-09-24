package v1alpha1_apikeyservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pkg_utils "github.com/fishtre-compagnie/husonym/backend/pkg/utils"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

func (s *Service) GetAccountApiKeys(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountApiKeysRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountApiKeysResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
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

	apiKeys, err := s.db.Q.GetAccountApiKeys(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}

	dtos := make([]*mgmtv1alpha1.AccountApiKey, len(apiKeys))
	for idx := range apiKeys {
		apiKey := apiKeys[idx]
		dtos[idx] = dtomaps.ToAccountApiKeyDto(&apiKey, nil)
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountApiKeysResponse{
		ApiKeys: dtos,
	}), nil
}

func (s *Service) GetAccountApiKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountApiKeyRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountApiKeyResponse], error) {
	apiKeyUuid, err := husonymdb.ToUuid(req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	apiKey, err := s.db.Q.GetAccountApiKeyById(ctx, s.db.Db, apiKeyUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return nil, husonymerrors.NewNotFound("unable to find api key")
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(husonymdb.UUIDString(apiKey.AccountID)),
		rbac.AccountAction_View,
	); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountApiKeyResponse{
		ApiKey: dtomaps.ToAccountApiKeyDto(&apiKey, nil),
	}), nil
}

func (s *Service) CreateAccountApiKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.CreateAccountApiKeyRequest],
) (*connect.Response[mgmtv1alpha1.CreateAccountApiKeyResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	if user.IsApiKey() {
		return nil, husonymerrors.NewUnauthorized("api key user cannot create api keys")
	}

	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}
	if err := enforceHeldByCreator(ctx, user, req.Msg.GetAccountId(), req.Msg.GetPermissions()); err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	expiresAt, err := husonymdb.ToTimestamp(req.Msg.GetExpiresAt().AsTime())
	if err != nil {
		return nil, err
	}

	clearKeyValue := apikey.NewV1AccountKey()
	hashedKeyValue := pkg_utils.ToSha256(
		clearKeyValue,
	)

	newApiKey, err := s.db.CreateAccountApikey(ctx, &husonymdb.CreateAccountApiKeyRequest{
		KeyName:           req.Msg.Name,
		KeyValue:          hashedKeyValue,
		AccountUuid:       accountUuid,
		CreatedByUserUuid: user.PgId(),
		ExpiresAt:         expiresAt,
		Permissions:       permission.Names(req.Msg.GetPermissions()),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.CreateAccountApiKeyResponse{
		ApiKey: dtomaps.ToAccountApiKeyDto(newApiKey, &clearKeyValue),
	}), nil
}

func (s *Service) RegenerateAccountApiKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RegenerateAccountApiKeyRequest],
) (*connect.Response[mgmtv1alpha1.RegenerateAccountApiKeyResponse], error) {
	apiKeyUuid, err := husonymdb.ToUuid(req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	apiKey, err := s.db.Q.GetAccountApiKeyById(ctx, s.db.Db, apiKeyUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return nil, husonymerrors.NewNotFound("account api key not found")
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	if user.IsApiKey() {
		return nil, husonymerrors.NewUnauthorized("api key user cannot regenerate api keys")
	}

	if err := user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(husonymdb.UUIDString(apiKey.AccountID)),
		rbac.AccountAction_Edit,
	); err != nil {
		return nil, err
	}

	clearKeyValue := apikey.NewV1AccountKey()
	hashedKeyValue := pkg_utils.ToSha256(
		clearKeyValue,
	)
	expiresAt, err := husonymdb.ToTimestamp(req.Msg.GetExpiresAt().AsTime())
	if err != nil {
		return nil, err
	}
	updatedApiKey, err := s.db.Q.UpdateAccountApiKeyValue(
		ctx,
		s.db.Db,
		db_queries.UpdateAccountApiKeyValueParams{
			KeyValue:    hashedKeyValue,
			ExpiresAt:   expiresAt,
			UpdatedByID: user.PgId(),
			ID:          apiKeyUuid,
		},
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.RegenerateAccountApiKeyResponse{
		ApiKey: dtomaps.ToAccountApiKeyDto(&updatedApiKey, &clearKeyValue),
	}), nil
}

func (s *Service) DeleteAccountApiKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.DeleteAccountApiKeyRequest],
) (*connect.Response[mgmtv1alpha1.DeleteAccountApiKeyResponse], error) {
	apiKeyUuid, err := husonymdb.ToUuid(req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	apiKey, err := s.db.Q.GetAccountApiKeyById(ctx, s.db.Db, apiKeyUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return connect.NewResponse(&mgmtv1alpha1.DeleteAccountApiKeyResponse{}), nil
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if user.IsApiKey() {
		return nil, husonymerrors.NewUnauthorized("api key user cannot delete api keys")
	}
	if err := user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(husonymdb.UUIDString(apiKey.AccountID)),
		rbac.AccountAction_Edit,
	); err != nil {
		return nil, err
	}

	err = s.db.Q.RemoveAccountApiKey(ctx, s.db.Db, apiKeyUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.DeleteAccountApiKeyResponse{}), nil
}

// enforceHeldByCreator refuses a permission the creator does not hold account-wide: a key cannot
// do more than the person who made it. Without a license the RBAC allows everything, so this
// only bites where roles exist.
func enforceHeldByCreator(
	ctx context.Context,
	user *userdata.User,
	accountId string,
	permissions []mgmtv1alpha1.Permission,
) error {
	wildcard := userdata.NewWildcardDomainEntity(accountId)
	for _, p := range permissions {
		var held bool
		var err error
		switch entity, action := permission.Split(p); entity {
		case "account":
			held, err = user.Account(ctx, userdata.NewIdentifier(accountId), rbac.AccountAction(action))
		case "connection":
			held, err = user.Connection(ctx, wildcard, rbac.ConnectionAction(action))
		case "job":
			held, err = user.Job(ctx, wildcard, rbac.JobAction(action))
		default:
			return husonymerrors.NewBadRequest(fmt.Sprintf("no such permission: %s", permission.Name(p)))
		}
		if err != nil {
			return err
		}
		if !held {
			return husonymerrors.NewUnauthorized(fmt.Sprintf(
				"a key cannot hold %s: you do not hold it yourself", permission.Name(p),
			))
		}
	}
	return nil
}
