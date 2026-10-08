package v1alpha1_useraccountservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/stretchr/testify/require"
)

// rolesOfTest is an access control whose changes of role answer what the test tells them to.
type rolesOfTest struct {
	rbac.Interface
	answer error
}

func (r rolesOfTest) SetRole(context.Context, rbac.User, rbac.Account, mgmtv1alpha1.AccountRole) error {
	return r.answer
}

func (r rolesOfTest) SetRoleKeepingAnAdmin(context.Context, rbac.User, rbac.Account, mgmtv1alpha1.AccountRole) error {
	return r.answer
}

func (r rolesOfTest) RemoveMemberKeepingAnAdmin(context.Context, rbac.User, rbac.Account) error {
	return r.answer
}

// For the organization of the instance, the change that would leave it with no administrator is
// a failed precondition told in a plain sentence, which names neither the member nor the account;
// a change stored and not read back is a change made; any other failure fails the request.
func Test_KeepingAnAdmin_TheLastAdministratorIsAFailedPrecondition(t *testing.T) {
	var logged bytes.Buffer
	ctx := logger_interceptor.SetLoggerContext(context.Background(), slog.New(slog.NewTextHandler(&logged, nil)))
	member, account := rbac.NewUser("6a1c0c0e-9d1f-4f0b-8a55-0c5a4a1b2c3d"), rbac.NewAccount("3f2b7a10-1111-4222-8333-444455556666")
	viewer := mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER
	changes := func(s *Service) []error {
		return []error{s.setRoleKeepingAnAdmin(ctx, member, account, viewer), s.removeRolesKeepingAnAdmin(ctx, member, account)}
	}

	last := &Service{rbacClient: rolesOfTest{answer: fmt.Errorf("unable to change the role: %w", rbac.ErrLastAdmin)}}
	for _, err := range changes(last) {
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		require.Contains(t, err.Error(), "the organization of this instance must keep an administrator")
		require.NotContains(t, err.Error(), "6a1c0c0e")
		require.NotContains(t, err.Error(), "3f2b7a10")
	}
	require.Empty(t, logged.String())

	for _, err := range changes(&Service{rbacClient: rolesOfTest{}}) {
		require.NoError(t, err)
	}

	notReadBack := &Service{rbacClient: rolesOfTest{answer: fmt.Errorf("unable to change the role: %w", rbac.ErrRoleNotReadBack)}}
	for _, err := range changes(notReadBack) {
		require.NoError(t, err)
	}
	require.Equal(t, 2, strings.Count(logged.String(), "level=WARN"))

	down := errors.New("the database is down")
	for _, err := range changes(&Service{rbacClient: rolesOfTest{answer: down}}) {
		require.ErrorIs(t, err, down)
	}
}

// A role that is stored, and that this instance could not read back, is a role given: the
// request goes on as when the role is held at once, and a warning names the member and the
// account. Any other failure of a change of role fails the request.
func Test_setRole_ARoleStoredAndNotReadBackIsGiven(t *testing.T) {
	var logged bytes.Buffer
	ctx := logger_interceptor.SetLoggerContext(context.Background(), slog.New(slog.NewTextHandler(&logged, nil)))
	member, account := rbac.NewUser("6a1c0c0e-9d1f-4f0b-8a55-0c5a4a1b2c3d"), rbac.NewAccount("3f2b7a10-1111-4222-8333-444455556666")
	admin := mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN

	given := &Service{rbacClient: rolesOfTest{}}
	require.NoError(t, given.setRole(ctx, member, account, admin))
	require.Empty(t, logged.String())

	notReadBack := &Service{rbacClient: rolesOfTest{answer: fmt.Errorf("unable to give the role: %w", rbac.ErrRoleNotReadBack)}}
	require.NoError(t, notReadBack.setRole(ctx, member, account, admin))
	require.Contains(t, logged.String(), "level=WARN")
	require.Contains(t, logged.String(), "6a1c0c0e-9d1f-4f0b-8a55-0c5a4a1b2c3d")
	require.Contains(t, logged.String(), "3f2b7a10-1111-4222-8333-444455556666")

	down := errors.New("the database is down")
	refused := &Service{rbacClient: rolesOfTest{answer: down}}
	require.ErrorIs(t, refused.setRole(ctx, member, account, admin), down)
}
