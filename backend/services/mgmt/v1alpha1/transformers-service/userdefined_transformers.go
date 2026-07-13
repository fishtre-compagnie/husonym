package v1alpha1_transformersservice

import (
	"context"
	"fmt"
	"regexp"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	nucleuserrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/dop251/goja"
)

func (s *Service) GetUserDefinedTransformers(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUserDefinedTransformersRequest],
) (*connect.Response[mgmtv1alpha1.GetUserDefinedTransformersResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()),
		rbac.JobAction_View,
	)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	transformers, err := s.db.Q.GetUserDefinedTransformersByAccount(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}

	dtoTransformers := []*mgmtv1alpha1.UserDefinedTransformer{}
	for idx := range transformers {
		transformer := transformers[idx]
		dto, err := dtomaps.ToUserDefinedTransformerDto(
			&transformer,
			s.getSystemTransformerSourceMap(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to map user defined transformer %s with source %d: %w",
				husonymdb.UUIDString(transformer.ID),
				transformer.Source,
				err,
			)
		}
		dtoTransformers = append(dtoTransformers, dto)
	}

	return connect.NewResponse(&mgmtv1alpha1.GetUserDefinedTransformersResponse{
		Transformers: dtoTransformers,
	}), nil
}

func (s *Service) GetUserDefinedTransformerById(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUserDefinedTransformerByIdRequest],
) (*connect.Response[mgmtv1alpha1.GetUserDefinedTransformerByIdResponse], error) {
	tId, err := husonymdb.ToUuid(req.Msg.GetTransformerId())
	if err != nil {
		return nil, err
	}

	transformer, err := s.db.Q.GetUserDefinedTransformerById(ctx, s.db.Db, tId)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return nil, nucleuserrors.NewNotFound("unable to find transformer by id")
	}

	dto, err := dtomaps.ToUserDefinedTransformerDto(&transformer, s.getSystemTransformerSourceMap())
	if err != nil {
		return nil, fmt.Errorf(
			"failed to map user defined transformer %s with source %d: %w",
			husonymdb.UUIDString(transformer.ID),
			transformer.Source,
			err,
		)
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(dto.GetAccountId()),
		rbac.JobAction_View,
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.GetUserDefinedTransformerByIdResponse{
		Transformer: dto,
	}), nil
}

func (s *Service) CreateUserDefinedTransformer(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.CreateUserDefinedTransformerRequest],
) (*connect.Response[mgmtv1alpha1.CreateUserDefinedTransformerResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()),
		rbac.JobAction_Edit,
	)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	UserDefinedTransformer := &db_queries.CreateUserDefinedTransformerParams{
		AccountID:         accountUuid,
		Name:              req.Msg.Name,
		Description:       req.Msg.Description,
		TransformerConfig: &pg_models.TransformerConfig{},
		Source:            int32(req.Msg.Source),
		CreatedByID:       user.PgId(),
		UpdatedByID:       user.PgId(),
	}

	err = UserDefinedTransformer.TransformerConfig.FromTransformerConfigDto(
		req.Msg.TransformerConfig,
	)
	if err != nil {
		return nil, err
	}

	ct, err := s.db.Q.CreateUserDefinedTransformer(ctx, s.db.Db, *UserDefinedTransformer)
	if err != nil {
		return nil, err
	}

	dto, err := dtomaps.ToUserDefinedTransformerDto(&ct, s.getSystemTransformerSourceMap())
	if err != nil {
		return nil, fmt.Errorf(
			"failed to map user defined transformer %s with source %d: %w",
			husonymdb.UUIDString(ct.ID),
			ct.Source,
			err,
		)
	}

	return connect.NewResponse(&mgmtv1alpha1.CreateUserDefinedTransformerResponse{
		Transformer: dto,
	}), nil
}

func (s *Service) DeleteUserDefinedTransformer(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.DeleteUserDefinedTransformerRequest],
) (*connect.Response[mgmtv1alpha1.DeleteUserDefinedTransformerResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("transformerId", req.Msg.GetTransformerId())

	tId, err := husonymdb.ToUuid(req.Msg.GetTransformerId())
	if err != nil {
		return nil, err
	}

	transformer, err := s.db.Q.GetUserDefinedTransformerById(ctx, s.db.Db, tId)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return connect.NewResponse(&mgmtv1alpha1.DeleteUserDefinedTransformerResponse{}), nil
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(husonymdb.UUIDString(transformer.AccountID)),
		rbac.JobAction_Delete,
	)
	if err != nil {
		return nil, err
	}

	err = s.db.Q.DeleteUserDefinedTransformerById(ctx, s.db.Db, transformer.ID)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		logger.Debug("transformer not found or has already been removed")
	}

	return connect.NewResponse(&mgmtv1alpha1.DeleteUserDefinedTransformerResponse{}), nil
}

func (s *Service) UpdateUserDefinedTransformer(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.UpdateUserDefinedTransformerRequest],
) (*connect.Response[mgmtv1alpha1.UpdateUserDefinedTransformerResponse], error) {
	tUuid, err := husonymdb.ToUuid(req.Msg.TransformerId)
	if err != nil {
		return nil, err
	}
	transformer, err := s.db.Q.GetUserDefinedTransformerById(ctx, s.db.Db, tUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return nil, nucleuserrors.NewNotFound("unable to find transformer by id")
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(husonymdb.UUIDString(transformer.AccountID)),
		rbac.JobAction_Edit,
	)
	if err != nil {
		return nil, err
	}

	updateParams := &db_queries.UpdateUserDefinedTransformerParams{
		Name:              req.Msg.Name,
		Description:       req.Msg.Description,
		TransformerConfig: &pg_models.TransformerConfig{},
		UpdatedByID:       user.PgId(),
		ID:                tUuid,
	}
	// todo: must verify that this updated config is valid for the configured source
	err = updateParams.TransformerConfig.FromTransformerConfigDto(req.Msg.GetTransformerConfig())
	if err != nil {
		return nil, err
	}

	updatedTransformer, err := s.db.Q.UpdateUserDefinedTransformer(ctx, s.db.Db, *updateParams)
	if err != nil {
		return nil, err
	}

	dto, err := dtomaps.ToUserDefinedTransformerDto(
		&updatedTransformer,
		s.getSystemTransformerSourceMap(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to map user defined transformer %s with source %d: %w",
			husonymdb.UUIDString(updatedTransformer.ID),
			updatedTransformer.Source,
			err,
		)
	}

	return connect.NewResponse(&mgmtv1alpha1.UpdateUserDefinedTransformerResponse{
		Transformer: dto,
	}), err
}

func (s *Service) IsTransformerNameAvailable(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.IsTransformerNameAvailableRequest],
) (*connect.Response[mgmtv1alpha1.IsTransformerNameAvailableResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()),
		rbac.JobAction_View,
	)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	count, err := s.db.Q.IsTransformerNameAvailable(
		ctx,
		s.db.Db,
		db_queries.IsTransformerNameAvailableParams{
			AccountId:       accountUuid,
			TransformerName: req.Msg.TransformerName,
		},
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.IsTransformerNameAvailableResponse{
		IsAvailable: count == 0,
	}), nil
}

// use the goja library to validate that the javascript can compile and theoretically run
func (s *Service) ValidateUserJavascriptCode(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ValidateUserJavascriptCodeRequest],
) (*connect.Response[mgmtv1alpha1.ValidateUserJavascriptCodeResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.ValidateUserJavascriptCodeResponse{
		Valid: IsUserJavascriptCodeValid(req.Msg.GetCode()),
	}), nil
}

// IsUserJavascriptCodeValid compiles a user-defined transformer's javascript
// body the same way ValidateUserJavascriptCode does, without going over the
// network. It is exported so that other server-side code paths (e.g. the AI
// transformer proposal generator in internal/ee/recommendations) can validate
// a draft before ever surfacing it to a caller.
func IsUserJavascriptCodeValid(jsCode string) bool {
	js := constructJavascriptCode(jsCode)
	_, err := goja.Compile("test", js, true)
	return err == nil
}

func constructJavascriptCode(jsCode string) string {
	if jsCode != "" {
		return fmt.Sprintf(`(()=>{
			function fn1(value){
				%s
				}})();`, jsCode)
	} else {
		return ""
	}
}

func (s *Service) ValidateUserRegexCode(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ValidateUserRegexCodeRequest],
) (*connect.Response[mgmtv1alpha1.ValidateUserRegexCodeResponse], error) {
	_, err := regexp.Compile(req.Msg.GetUserProvidedRegex())
	// todo: should return error message here and surface to user
	return connect.NewResponse(&mgmtv1alpha1.ValidateUserRegexCodeResponse{
		Valid: err == nil,
	}), nil
}
