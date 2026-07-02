package v1alpha1_apikeyservice

import (
	"context"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pkg_utils "github.com/fishtre-compagnie/husonym/backend/pkg/utils"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	nucleuserrors "github.com/fishtre-compagnie/husonym/internal/errors"
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
		return nil, nucleuserrors.NewNotFound("unable to find api key")
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
		return nil, nucleuserrors.NewUnauthorized("api key user cannot create api keys")
	}

	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
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
		return nil, nucleuserrors.NewNotFound("account api key not found")
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	if user.IsApiKey() {
		return nil, nucleuserrors.NewUnauthorized("api key user cannot regenerate api keys")
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
		return nil, nucleuserrors.NewUnauthorized("api key user cannot delete api keys")
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
