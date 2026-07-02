package connections_cmd

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

const husonymDbMigrationsPath = "../../../../../backend/sql/postgresql/schema"

func Test_Connections(t *testing.T) {
	t.Parallel()
	ok := testutil.ShouldRunCLIIntegrationTest()
	if !ok {
		return
	}
	ctx := context.Background()

	husonymApi, err := tchusonymapi.NewHusonymApiTestClient(
		ctx,
		t,
		tchusonymapi.WithMigrationsDirectory(husonymDbMigrationsPath),
	)
	if err != nil {
		panic(err)
	}
	postgresUrl := "postgresql://postgres:foofar@localhost:5434/husonym"

	t.Run("list_unauthed", func(t *testing.T) {
		accountId := tchusonymapi.CreatePersonalAccount(
			ctx,
			t,
			husonymApi.OSSUnauthenticatedLicensedClients.Users(),
		)
		conn1 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			husonymApi.OSSUnauthenticatedLicensedClients.Connections(),
			accountId,
			"conn1",
			postgresUrl,
		)
		conn2 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			husonymApi.OSSUnauthenticatedLicensedClients.Connections(),
			accountId,
			"conn2",
			postgresUrl,
		)
		conns := []*mgmtv1alpha1.Connection{conn1, conn2}
		connections, err := getConnections(
			ctx,
			husonymApi.OSSUnauthenticatedLicensedClients.Connections(),
			accountId,
		)
		require.NoError(t, err)
		require.Len(t, connections, len(conns))
	})

	t.Run("list_auth", func(t *testing.T) {
		testAuthUserId := "c3b32842-9b70-4f4e-ad45-9cab26c6f2f1"
		userclient := husonymApi.OSSAuthenticatedLicensedClients.Users(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		connclient := husonymApi.OSSAuthenticatedLicensedClients.Connections(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		tchusonymapi.SetUser(ctx, t, userclient)
		accountId := tchusonymapi.CreatePersonalAccount(ctx, t, userclient)
		conn1 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			connclient,
			accountId,
			"conn1",
			postgresUrl,
		)
		conn2 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			connclient,
			accountId,
			"conn2",
			postgresUrl,
		)
		conns := []*mgmtv1alpha1.Connection{conn1, conn2}
		connections, err := getConnections(ctx, connclient, accountId)
		require.NoError(t, err)
		require.Len(t, connections, len(conns))
	})

	t.Run("list_cloud", func(t *testing.T) {
		testAuthUserId := "34f3e404-c995-452b-89e4-9c486b491dab"
		userclient := husonymApi.HusonymCloudAuthenticatedLicensedClients.Users(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		connclient := husonymApi.HusonymCloudAuthenticatedLicensedClients.Connections(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		tchusonymapi.SetUser(ctx, t, userclient)
		accountId := tchusonymapi.CreatePersonalAccount(ctx, t, userclient)
		conn1 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			connclient,
			accountId,
			"conn1",
			postgresUrl,
		)
		conn2 := tchusonymapi.CreatePostgresConnection(
			ctx,
			t,
			connclient,
			accountId,
			"conn2",
			postgresUrl,
		)
		conns := []*mgmtv1alpha1.Connection{conn1, conn2}
		connections, err := getConnections(ctx, connclient, accountId)
		require.NoError(t, err)
		require.Len(t, connections, len(conns))
	})

	err = husonymApi.TearDown(ctx)
	if err != nil {
		panic(err)
	}
}
