package hooks

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// GetAccountHooks gives the hooks of an account, oldest first.
func (s *AccountService) GetAccountHooks(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountHooksRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountHooksResponse], error) {
	t, accountID, err := account(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceGetAccountHooksProcedure, t, intent{})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Q.GetAccountHooksByAccount(ctx, s.db.Db, accountID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the hooks of the account: %w", err)
	}
	hooks, err := s.read(ctx, admitted, rows)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetAccountHooksResponse{Hooks: hooks}), nil
}

// GetAccountHook gives one hook. It is what the worker reads a hook with before calling it.
func (s *AccountService) GetAccountHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountHookRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountHookResponse], error) {
	t, row, err := s.hook(ctx, req.Msg.GetId(), accountHookNotFound())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceGetAccountHookProcedure, t, intent{})
	if err != nil {
		return nil, err
	}
	hook, err := s.readOne(ctx, admitted, row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetAccountHookResponse{Hook: hook}), nil
}

// IsAccountHookNameAvailable says whether no hook of the account has this name. It is a hint
// for whoever picks a name: only creating the hook tells for sure.
func (s *AccountService) IsAccountHookNameAvailable(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.IsAccountHookNameAvailableRequest],
) (*connect.Response[mgmtv1alpha1.IsAccountHookNameAvailableResponse], error) {
	t, accountID, err := account(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	if _, err := s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceIsAccountHookNameAvailableProcedure, t, intent{}); err != nil {
		return nil, err
	}
	available, err := s.db.Q.IsAccountHookNameAvailable(ctx, s.db.Db, db_queries.IsAccountHookNameAvailableParams{
		AccountID: accountID,
		Name:      req.Msg.GetName(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to tell whether the account hook name is free: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.IsAccountHookNameAvailableResponse{IsAvailable: available}), nil
}

// GetActiveAccountHooksByEvent gives the hooks of an account that are on and listen to the
// event, or to every event, oldest first. Asked about no event in particular, it gives the
// hooks that listen to every event. It is what the worker asks when an event occurs.
func (s *AccountService) GetActiveAccountHooksByEvent(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetActiveAccountHooksByEventRequest],
) (*connect.Response[mgmtv1alpha1.GetActiveAccountHooksByEventResponse], error) {
	t, accountID, err := account(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.AccountHookServiceGetActiveAccountHooksByEventProcedure, t, intent{})
	if err != nil {
		return nil, err
	}

	everyEvent := int32(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED)
	listenedTo := []int32{everyEvent}
	if event := int32(req.Msg.GetEvent()); event != everyEvent {
		listenedTo = append(listenedTo, event)
	}
	rows, err := s.db.Q.GetActiveAccountHooksByEvent(ctx, s.db.Db, db_queries.GetActiveAccountHooksByEventParams{
		AccountID: accountID,
		Events:    listenedTo,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the active hooks of the account: %w", err)
	}
	hooks, err := s.read(ctx, admitted, rows)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetActiveAccountHooksByEventResponse{Hooks: hooks}), nil
}

// read gives hooks as the admitted caller reads them.
func (s *AccountService) read(
	ctx context.Context,
	caller *admission,
	rows []db_queries.HusonymApiAccountHook,
) ([]*mgmtv1alpha1.AccountHook, error) {
	readsSecret, err := s.readsSecret(ctx, caller)
	if err != nil {
		return nil, err
	}
	return toAccountHooks(ctx, rows, readsSecret)
}

func (s *AccountService) readOne(
	ctx context.Context,
	caller *admission,
	row *db_queries.HusonymApiAccountHook,
) (*mgmtv1alpha1.AccountHook, error) {
	readsSecret, err := s.readsSecret(ctx, caller)
	if err != nil {
		return nil, err
	}
	return toAccountHook(ctx, row, readsSecret)
}
