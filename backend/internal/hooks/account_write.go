package hooks

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// CreateAccountHook stores a webhook for an account.
func (s *AccountService) CreateAccountHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.CreateAccountHookRequest],
) (*connect.Response[mgmtv1alpha1.CreateAccountHookResponse], error) {
	t, accountID, err := account(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	hook := req.Msg.GetHook()
	admitted, err := s.gate.admit(
		ctx, mgmtv1alpha1connect.AccountHookServiceCreateAccountHookProcedure, t,
		intent{refuse: refuseRetired(hook.GetConfig())},
	)
	if err != nil {
		return nil, err
	}

	checked, err := checkAccountHook(hook.GetConfig(), hook.GetEvents())
	if err != nil {
		return nil, err
	}
	row, err := s.db.Q.CreateAccountHook(ctx, s.db.Db, db_queries.CreateAccountHookParams{
		Name:            hook.GetName(),
		Description:     hook.GetDescription(),
		AccountID:       accountID,
		Events:          checked.events,
		Config:          checked.config,
		CreatedByUserID: admitted.caller.PgId(),
		UpdatedByUserID: admitted.caller.PgId(),
		Enabled:         hook.GetEnabled(),
	})
	switch {
	case nameTaken(err, accountHookNameConstraint):
		return nil, accountHookNameTaken(hook.GetName())
	case err != nil:
		return nil, fmt.Errorf("unable to create account hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("account hook created", "hookId", husonymdb.UUIDString(row.ID), "hookName", row.Name)

	dto, err := s.readOne(ctx, admitted, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.CreateAccountHookResponse{Hook: dto}), nil
}

// UpdateAccountHook replaces the name, the description, the events, the configuration and
// the state of a hook. A hook of the retired kind may become a webhook; no hook becomes one
// of the retired kind.
func (s *AccountService) UpdateAccountHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.UpdateAccountHookRequest],
) (*connect.Response[mgmtv1alpha1.UpdateAccountHookResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), accountHookNotFound())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(
		ctx, mgmtv1alpha1connect.AccountHookServiceUpdateAccountHookProcedure, t,
		intent{refuse: refuseRetired(req.Msg.GetConfig())},
	)
	if err != nil {
		return nil, err
	}

	checked, err := checkAccountHook(req.Msg.GetConfig(), req.Msg.GetEvents())
	if err != nil {
		return nil, err
	}
	row, err := s.db.Q.UpdateAccountHook(ctx, s.db.Db, db_queries.UpdateAccountHookParams{
		Name:            req.Msg.GetName(),
		Description:     req.Msg.GetDescription(),
		Events:          checked.events,
		Config:          checked.config,
		Enabled:         req.Msg.GetEnabled(),
		UpdatedByUserID: admitted.caller.PgId(),
		ID:              current.ID,
	})
	switch {
	case husonymdb.IsNoRows(err):
		return nil, accountHookNotFound()
	case nameTaken(err, accountHookNameConstraint):
		return nil, accountHookNameTaken(req.Msg.GetName())
	case err != nil:
		return nil, fmt.Errorf("unable to update account hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("account hook updated", "hookId", husonymdb.UUIDString(row.ID), "hookName", row.Name)

	dto, err := s.readOne(ctx, admitted, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.UpdateAccountHookResponse{Hook: dto}), nil
}

// SetAccountHookEnabled sets the enabled flag of a hook. Enabling is refused unless the stored
// configuration is a webhook; disabling is accepted for any stored configuration. The checks
// are chosen from the requested value alone, and the row is written only when the flag
// differs from it.
func (s *AccountService) SetAccountHookEnabled(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetAccountHookEnabledRequest],
) (*connect.Response[mgmtv1alpha1.SetAccountHookEnabledResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), accountHookNotFound())
	if err != nil {
		return nil, err
	}
	asked := intent{arming: req.Msg.GetEnabled()}
	if asked.arming {
		asked.refuse = refuseToArm(ctx, current)
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceSetAccountHookEnabledProcedure, t, asked)
	if err != nil {
		return nil, err
	}

	row := *current
	if current.Enabled != req.Msg.GetEnabled() {
		row, err = s.db.Q.SetAccountHookEnabled(ctx, s.db.Db, db_queries.SetAccountHookEnabledParams{
			Enabled:         req.Msg.GetEnabled(),
			UpdatedByUserID: admitted.caller.PgId(),
			ID:              current.ID,
		})
		switch {
		case husonymdb.IsNoRows(err):
			return nil, accountHookNotFound()
		case err != nil:
			return nil, fmt.Errorf("unable to turn the account hook on or off: %w", err)
		}
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).
			Debug("account hook turned on or off", "hookId", husonymdb.UUIDString(row.ID), "enabled", row.Enabled)
	}

	dto, err := s.readOne(ctx, admitted, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetAccountHookEnabledResponse{Hook: dto}), nil
}

// DeleteAccountHook removes a hook, whatever its kind. With no hook to remove, it is done
// already.
func (s *AccountService) DeleteAccountHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.DeleteAccountHookRequest],
) (*connect.Response[mgmtv1alpha1.DeleteAccountHookResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), errNothingToRemove)
	if err != nil {
		return nil, err
	}
	_, err = s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceDeleteAccountHookProcedure, t, intent{})
	switch {
	case errors.Is(err, errNothingToRemove):
		return connect.NewResponse(&mgmtv1alpha1.DeleteAccountHookResponse{}), nil
	case err != nil:
		return nil, err
	}

	if err := s.db.Q.RemoveAccountHookById(ctx, s.db.Db, current.ID); err != nil {
		return nil, fmt.Errorf("unable to remove account hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("account hook removed", "hookId", husonymdb.UUIDString(current.ID), "hookName", current.Name)
	return connect.NewResponse(&mgmtv1alpha1.DeleteAccountHookResponse{}), nil
}
