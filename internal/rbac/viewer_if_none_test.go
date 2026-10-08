package rbac

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/casbin/casbin/v3/model"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
	"github.com/stretchr/testify/require"
)

// firstRows is a table held in memory that also gives a role to who holds none, as the table of
// the API database does. It counts how often it is read whole.
type firstRows struct {
	*memoryRows
	loads int
}

var _ enforcer.FirstAssignments = (*firstRows)(nil)

func (s *firstRows) held(user, account string) bool {
	for _, row := range s.rows {
		if row[0] == "g" && len(row) == 4 && row[1] == user && row[3] == account {
			return true
		}
	}
	return false
}

func (s *firstRows) HasAssignmentCtx(_ context.Context, user, account string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held(user, account), nil
}

func (s *firstRows) AddAssignmentIfNoneCtx(_ context.Context, user, role, account string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failingWrites != nil {
		return false, s.failingWrites
	}
	if s.held(user, account) {
		return false, nil
	}
	s.rows = append(s.rows, []string{"g", user, role, account})
	return true, nil
}

func (s *firstRows) LoadFilteredPolicyCtx(ctx context.Context, m model.Model, filter any) error {
	s.mu.Lock()
	s.loads++
	s.mu.Unlock()
	return s.memoryRows.LoadFilteredPolicyCtx(ctx, m, filter)
}

func serviceOnFirstRows(t *testing.T) (*Service, *firstRows) {
	t.Helper()
	rows := &firstRows{memoryRows: &memoryRows{}}
	service, err := newService(t.Context(), rows, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return service, rows
}

func Test_GrantViewerIfNone_GivesViewerToWhoHoldsNoRole(t *testing.T) {
	ctx := context.Background()
	service, rows := serviceOnFirstRows(t)
	member, account := someone(), someAccount()

	require.NoError(t, service.GrantViewerIfNone(ctx, member, account))

	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())
	require.NoError(t, service.Enforce(ctx, member, account, JobAction_View))
	requireRefused(t, service.Enforce(ctx, member, account, JobAction_Create), JobAction_Create)
}

// A role held costs one look at the table: no write, and the roles are not read again.
func Test_GrantViewerIfNone_ARoleHeldCostsNoWriteAndNoReload(t *testing.T) {
	ctx := context.Background()
	service, rows := serviceOnFirstRows(t)
	member, account := someone(), someAccount()
	// Another instance made the member an admin: this one has not read it.
	rows.write(assignment(member, roleAdmin, account)...)
	writes, loads := rows.writes, rows.loads

	require.NoError(t, service.GrantViewerIfNone(ctx, member, account))

	require.Equal(t, [][]string{assignment(member, roleAdmin, account)}, rows.stored())
	require.Equal(t, writes, rows.writes)
	require.Equal(t, loads, rows.loads)
}

// A role the table took and that could not be read back is told as SetRole tells it.
func Test_GrantViewerIfNone_TellsARoleStoredAndNotReadBack(t *testing.T) {
	ctx := context.Background()
	service, rows := serviceOnFirstRows(t)
	member, account := someone(), someAccount()

	down := errors.New("the database is down")
	rows.fail(nil, down)
	err := service.GrantViewerIfNone(ctx, member, account)
	require.ErrorIs(t, err, ErrRoleNotReadBack)
	require.ErrorIs(t, err, down)
	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())

	rows.fail(nil, nil)
	require.NoError(t, service.enforcer.LoadPolicy())
	require.NoError(t, service.Enforce(ctx, member, account, JobAction_View))
}

func Test_GrantViewerIfNone_ATableThatRefusesGivesNothing(t *testing.T) {
	ctx := context.Background()
	service, rows := serviceOnFirstRows(t)
	member, account := someone(), someAccount()

	down := errors.New("the database is down")
	rows.fail(down, nil)
	err := service.GrantViewerIfNone(ctx, member, account)
	require.ErrorIs(t, err, down)
	require.NotErrorIs(t, err, ErrRoleNotReadBack)
	require.Empty(t, rows.stored())
}

// A table that cannot give a first role says so, instead of giving one from what memory holds.
func Test_GrantViewerIfNone_OnATableThatCannot(t *testing.T) {
	service := serviceOn(t, &memoryRows{})
	err := service.GrantViewerIfNone(context.Background(), someone(), someAccount())
	require.ErrorIs(t, err, enforcer.ErrNoFirstAssignments)
}
