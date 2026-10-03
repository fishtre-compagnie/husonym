package serve_connect

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/stretchr/testify/require"
)

type granterOfTest struct {
	granted int
	failing error
}

func (g granterOfTest) GrantAdminWhereNoRole(context.Context, rbac.Accounts) (int, error) {
	return g.granted, g.failing
}

// The start of the API goes on when the accounts where nobody holds a role could not be given
// an admin: the failure is logged as an error, and nothing is said to have been given.
func Test_Start_SurvivesAFailingGrant(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	down := errors.New("the database is down")

	grantAdminWhereNoRole(context.Background(), granterOfTest{granted: 3, failing: down}, nil, logger)

	require.Contains(t, logged.String(), "level=ERROR")
	require.Contains(t, logged.String(), down.Error())
	require.NotContains(t, logged.String(), "made admin")
}

// What the start gave is logged, and a start that gave nothing says nothing.
func Test_Start_LogsTheAdminsItGave(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	grantAdminWhereNoRole(context.Background(), granterOfTest{}, nil, logger)
	require.Empty(t, logged.String())

	grantAdminWhereNoRole(context.Background(), granterOfTest{granted: 2}, nil, logger)
	require.Contains(t, logged.String(), "members=2")
}
