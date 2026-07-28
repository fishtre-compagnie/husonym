package integrationtests_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func (s *IntegrationTestSuite) createPersonalAccount(
	ctx context.Context,
	userclient mgmtv1alpha1connect.UserAccountServiceClient,
) string {
	s.T().Helper()
	return tchusonymapi.CreatePersonalAccount(ctx, s.T(), userclient)
}

func requireNoErrResp[T any](t testing.TB, resp *connect.Response[T], err error) {
	t.Helper()
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func requireErrResp[T any](t testing.TB, resp *connect.Response[T], err error) {
	t.Helper()
	require.Error(t, err)
	require.Nil(t, resp)
}

func requireConnectError(t testing.TB, err error, expectedCode connect.Code) {
	t.Helper()
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok, fmt.Sprintf("error was not connect error %T", err))
	require.Equal(
		t,
		expectedCode,
		connectErr.Code(),
		fmt.Sprintf("%d: %s", connectErr.Code(), connectErr.Message()),
	)
}

func (s *IntegrationTestSuite) setAccountCreatedAt(
	ctx context.Context,
	accountId string,
	createdAt time.Time,
) error {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return err
	}
	_, err = s.HusonymQuerier.SetAccountCreatedAt(
		ctx,
		s.Pgcontainer.DB,
		db_queries.SetAccountCreatedAtParams{
			CreatedAt: pgtype.Timestamp{Time: createdAt, Valid: true},
			AccountId: accountUuid,
		},
	)
	return err
}

func getAccountIds(t testing.TB, accounts []*mgmtv1alpha1.UserAccount) []string {
	t.Helper()
	output := []string{}
	for _, acc := range accounts {
		output = append(output, acc.GetId())
	}
	return output
}
