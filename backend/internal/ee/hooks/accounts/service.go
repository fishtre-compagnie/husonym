package accounthooks

import (
	"context"
	"encoding/json"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
)

type Service struct {
	db             *husonymdb.HusonymDb
	userdataclient userdata.Interface
}

var _ Interface = (*Service)(nil)

type Interface interface {
	GetAccountHooks(
		ctx context.Context,
		req *mgmtv1alpha1.GetAccountHooksRequest,
	) (*mgmtv1alpha1.GetAccountHooksResponse, error)
	GetAccountHook(
		ctx context.Context,
		req *mgmtv1alpha1.GetAccountHookRequest,
	) (*mgmtv1alpha1.GetAccountHookResponse, error)
	CreateAccountHook(
		ctx context.Context,
		req *mgmtv1alpha1.CreateAccountHookRequest,
	) (*mgmtv1alpha1.CreateAccountHookResponse, error)
	UpdateAccountHook(
		ctx context.Context,
		req *mgmtv1alpha1.UpdateAccountHookRequest,
	) (*mgmtv1alpha1.UpdateAccountHookResponse, error)
	DeleteAccountHook(
		ctx context.Context,
		req *mgmtv1alpha1.DeleteAccountHookRequest,
	) (*mgmtv1alpha1.DeleteAccountHookResponse, error)
	IsAccountHookNameAvailable(
		ctx context.Context,
		req *mgmtv1alpha1.IsAccountHookNameAvailableRequest,
	) (*mgmtv1alpha1.IsAccountHookNameAvailableResponse, error)
	SetAccountHookEnabled(
		ctx context.Context,
		req *mgmtv1alpha1.SetAccountHookEnabledRequest,
	) (*mgmtv1alpha1.SetAccountHookEnabledResponse, error)
	GetActiveAccountHooksByEvent(
		ctx context.Context,
		req *mgmtv1alpha1.GetActiveAccountHooksByEventRequest,
	) (*mgmtv1alpha1.GetActiveAccountHooksByEventResponse, error)
}

func New(
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
) *Service {
	return &Service{db: db, userdataclient: userdataclient}
}

// refuseSlackKind refuses a hook configuration of the Slack kind, which is no longer supported.
// Slack hooks that already exist are still listed and deleted, but none is created or armed.
func refuseSlackKind(config *mgmtv1alpha1.AccountHookConfig) error {
	if config.GetSlack() != nil {
		return husonymerrors.NewBadRequest(
			"Slack account hooks are no longer supported: use a webhook instead",
		)
	}
	return nil
}

func (s *Service) GetAccountHooks(
	ctx context.Context,
	req *mgmtv1alpha1.GetAccountHooksRequest,
) (*mgmtv1alpha1.GetAccountHooksResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("accountId", req.GetAccountId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	accountId, err := husonymdb.ToUuid(req.GetAccountId())
	if err != nil {
		return nil, err
	}

	hooks, err := s.db.Q.GetAccountHooksByAccount(ctx, s.db.Db, accountId)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return &mgmtv1alpha1.GetAccountHooksResponse{
			Hooks: []*mgmtv1alpha1.AccountHook{},
		}, nil
	}
	logger.Debug(fmt.Sprintf("found %d hooks", len(hooks)))

	hooksDto, err := dtomaps.ToAccountHooksDto(hooks)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.GetAccountHooksResponse{
		Hooks: hooksDto,
	}, nil
}

func (s *Service) GetAccountHook(
	ctx context.Context,
	req *mgmtv1alpha1.GetAccountHookRequest,
) (*mgmtv1alpha1.GetAccountHookResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("hookId", req.GetId())

	hookuuid, err := husonymdb.ToUuid(req.GetId())
	if err != nil {
		return nil, err
	}

	hook, err := s.db.Q.GetAccountHookById(ctx, s.db.Db, hookuuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return nil, husonymerrors.NewNotFound("unable to find account hook by id")
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(husonymdb.UUIDString(hook.AccountID)), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	logger.Debug("hook successfully found")

	dto, err := dtomaps.ToAccountHookDto(&hook)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.GetAccountHookResponse{
		Hook: dto,
	}, nil
}

func (s *Service) DeleteAccountHook(
	ctx context.Context,
	req *mgmtv1alpha1.DeleteAccountHookRequest,
) (*mgmtv1alpha1.DeleteAccountHookResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("hookId", req.GetId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	hookuuid, err := husonymdb.ToUuid(req.GetId())
	if err != nil {
		return nil, err
	}

	hook, err := s.db.Q.GetAccountHookById(ctx, s.db.Db, hookuuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		logger.Debug("unable to find hook during deletion")
		return &mgmtv1alpha1.DeleteAccountHookResponse{}, nil
	}

	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(husonymdb.UUIDString(hook.AccountID)), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}
	logger.Debug("attempting to remove hook")
	err = s.db.Q.RemoveAccountHookById(ctx, s.db.Db, hookuuid)
	if err != nil {
		return nil, err
	}
	return &mgmtv1alpha1.DeleteAccountHookResponse{}, nil
}

func (s *Service) IsAccountHookNameAvailable(
	ctx context.Context,
	req *mgmtv1alpha1.IsAccountHookNameAvailableRequest,
) (*mgmtv1alpha1.IsAccountHookNameAvailableResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("hookName", req.GetName(), "accountId", req.GetAccountId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	accountId, err := husonymdb.ToUuid(req.GetAccountId())
	if err != nil {
		return nil, err
	}

	logger.Debug("checking if hook name is available")
	ok, err := s.db.Q.IsAccountHookNameAvailable(
		ctx,
		s.db.Db,
		db_queries.IsAccountHookNameAvailableParams{
			AccountID: accountId,
			Name:      req.GetName(),
		},
	)
	if err != nil {
		return nil, err
	}
	return &mgmtv1alpha1.IsAccountHookNameAvailableResponse{
		IsAvailable: ok,
	}, nil
}

func (s *Service) SetAccountHookEnabled(
	ctx context.Context,
	req *mgmtv1alpha1.SetAccountHookEnabledRequest,
) (*mgmtv1alpha1.SetAccountHookEnabledResponse, error) {
	getResp, err := s.GetAccountHook(ctx, &mgmtv1alpha1.GetAccountHookRequest{Id: req.GetId()})
	if err != nil {
		return nil, err
	}

	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("hookId", req.GetId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(getResp.Hook.AccountId), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	// A Slack hook can be turned off, never on.
	if req.GetEnabled() {
		if err := refuseSlackKind(getResp.GetHook().GetConfig()); err != nil {
			return nil, err
		}
	}

	if req.GetEnabled() == getResp.GetHook().GetEnabled() {
		logger.Debug("hook is already in the desired state")
		return &mgmtv1alpha1.SetAccountHookEnabledResponse{
			Hook: getResp.GetHook(),
		}, nil
	}

	// Turning a hook off is stopping, which never takes a license; arming it does.
	if req.GetEnabled() {
		if err := user.EnforceLicense(ctx, getResp.GetHook().GetAccountId()); err != nil {
			return nil, err
		}
	}

	hookuuid, err := husonymdb.ToUuid(getResp.GetHook().GetId())
	if err != nil {
		return nil, err
	}

	logger.Debug(
		fmt.Sprintf(
			"attempting to update account hook enabled status from %v to %v",
			getResp.GetHook().GetEnabled(),
			req.GetEnabled(),
		),
	)
	updatedHook, err := s.db.Q.SetAccountHookEnabled(
		ctx,
		s.db.Db,
		db_queries.SetAccountHookEnabledParams{
			ID:              hookuuid,
			Enabled:         req.GetEnabled(),
			UpdatedByUserID: user.PgId(),
		},
	)
	if err != nil {
		return nil, err
	}

	dto, err := dtomaps.ToAccountHookDto(&updatedHook)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.SetAccountHookEnabledResponse{
		Hook: dto,
	}, nil
}

func (s *Service) GetActiveAccountHooksByEvent(
	ctx context.Context,
	req *mgmtv1alpha1.GetActiveAccountHooksByEventRequest,
) (*mgmtv1alpha1.GetActiveAccountHooksByEventResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("event", req.GetEvent())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	accountId, err := husonymdb.ToUuid(req.GetAccountId())
	if err != nil {
		return nil, err
	}

	// We always want to include the unspecified (wildcard) event to return webhooks that are listening to all events
	validEvents := []int32{int32(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED)}
	if req.GetEvent() != mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED {
		validEvents = append(validEvents, int32(req.GetEvent()))
	}

	eventStrings := make([]string, len(validEvents))
	for i, event := range validEvents {
		eventStrings[i] = mgmtv1alpha1.AccountHookEvent(event).String()
	}
	logger.Debug(fmt.Sprintf("searching for active account hooks by events %v", eventStrings))

	hooks, err := s.db.Q.GetActiveAccountHooksByEvent(
		ctx,
		s.db.Db,
		db_queries.GetActiveAccountHooksByEventParams{
			AccountID: accountId,
			Events:    validEvents,
		},
	)
	if err != nil {
		return nil, err
	}

	hooksDto, err := dtomaps.ToAccountHooksDto(hooks)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.GetActiveAccountHooksByEventResponse{
		Hooks: hooksDto,
	}, nil
}

func (s *Service) CreateAccountHook(
	ctx context.Context,
	req *mgmtv1alpha1.CreateAccountHookRequest,
) (*mgmtv1alpha1.CreateAccountHookResponse, error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("accountId", req.GetAccountId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}
	hookReq := req.GetHook()
	if err := refuseSlackKind(hookReq.GetConfig()); err != nil {
		return nil, err
	}
	if err := user.EnforceLicense(ctx, req.GetAccountId()); err != nil {
		return nil, err
	}

	logger.Debug(fmt.Sprintf("attempting to create new account hook %q", hookReq.GetName()))

	config, err := json.Marshal(hookReq.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to map config to valid json for db storage: %w", err)
	}

	accountId, err := husonymdb.ToUuid(req.GetAccountId())
	if err != nil {
		return nil, err
	}

	validEvents := []int32{}
	for _, event := range hookReq.GetEvents() {
		if _, ok := mgmtv1alpha1.AccountHookEvent_name[int32(event)]; !ok {
			return nil, husonymerrors.NewBadRequest(fmt.Sprintf("invalid event: %v", event))
		}
		validEvents = append(validEvents, int32(event))
	}

	hook, err := s.db.Q.CreateAccountHook(ctx, s.db.Db, db_queries.CreateAccountHookParams{
		Name:            hookReq.GetName(),
		Description:     hookReq.GetDescription(),
		AccountID:       accountId,
		Events:          validEvents,
		Config:          config,
		CreatedByUserID: user.PgId(),
		UpdatedByUserID: user.PgId(),
		Enabled:         hookReq.GetEnabled(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create account hook: %w", err)
	}
	logger.Debug("created account hook")

	dto, err := dtomaps.ToAccountHookDto(&hook)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.CreateAccountHookResponse{
		Hook: dto,
	}, nil
}

func (s *Service) UpdateAccountHook(
	ctx context.Context,
	req *mgmtv1alpha1.UpdateAccountHookRequest,
) (*mgmtv1alpha1.UpdateAccountHookResponse, error) {
	getResp, err := s.GetAccountHook(ctx, &mgmtv1alpha1.GetAccountHookRequest{Id: req.GetId()})
	if err != nil {
		return nil, err
	}

	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("hookId", req.GetId())

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(getResp.GetHook().GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}
	if err := refuseSlackKind(req.GetConfig()); err != nil {
		return nil, err
	}
	if err := user.EnforceLicense(ctx, getResp.GetHook().GetAccountId()); err != nil {
		return nil, err
	}

	logger.Debug(fmt.Sprintf("attempting to update account hook %q", getResp.GetHook().GetName()))

	validEvents := []int32{}
	for _, event := range req.GetEvents() {
		if _, ok := mgmtv1alpha1.AccountHookEvent_name[int32(event)]; !ok {
			return nil, husonymerrors.NewBadRequest(fmt.Sprintf("invalid event: %v", event))
		}
		validEvents = append(validEvents, int32(event))
	}

	config, err := json.Marshal(req.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to map config to valid json for db storage: %w", err)
	}

	hookuuid, err := husonymdb.ToUuid(getResp.GetHook().GetId())
	if err != nil {
		return nil, err
	}

	updatedHook, err := s.db.Q.UpdateAccountHook(ctx, s.db.Db, db_queries.UpdateAccountHookParams{
		ID:              hookuuid,
		Enabled:         req.GetEnabled(),
		Name:            req.GetName(),
		Description:     req.GetDescription(),
		Events:          validEvents,
		Config:          config,
		UpdatedByUserID: user.PgId(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to update account hook: %w", err)
	}

	dto, err := dtomaps.ToAccountHookDto(&updatedHook)
	if err != nil {
		return nil, err
	}

	return &mgmtv1alpha1.UpdateAccountHookResponse{
		Hook: dto,
	}, nil
}
