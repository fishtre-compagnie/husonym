package v1alpha1_connectiondataservice

import (
	"context"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func pgConfig(host string) *pg_models.ConnectionConfig {
	return &pg_models.ConnectionConfig{PgConfig: &pg_models.PostgresConnectionConfig{
		Connection: &pg_models.PostgresConnection{Host: host, Name: "shop", User: "u", Pass: "p"},
	}}
}

// A kept scan is read back as it was given, by table or for the whole connection; a second
// scan of a table takes the place of the first; and a connection whose settings change
// forgets the scans of its tables, which may no longer be the tables it reaches.
func Test_ScanResults_KeepReadAndForget(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	queries := db_queries.New()
	store := NewScanResults(husonymdb.New(pool, queries))

	account, err := queries.CreateTeamAccount(ctx, pool, "scan-account")
	require.NoError(t, err)
	user, err := queries.CreateNonMachineUser(ctx, pool)
	require.NoError(t, err)
	conn, err := queries.CreateConnection(ctx, pool, db_queries.CreateConnectionParams{
		Name: "source", AccountID: account.ID, ConnectionConfig: pgConfig("db-a"), CreatedByID: user.ID, UpdatedByID: user.ID,
	})
	require.NoError(t, err)
	connectionID := husonymdb.UUIDString(conn.ID)

	columns := []*mgmtv1alpha1.PiiScannedColumn{{Column: "email", DataType: "text"}, {Column: "id", DataType: "integer"}}
	scan := func(category string) *mgmtv1alpha1.DetectPiiInConnectionDataResponse {
		return &mgmtv1alpha1.DetectPiiInConnectionDataResponse{
			Detections: []*mgmtv1alpha1.ColumnPiiDetection{{Schema: "public", Table: "users", Column: "email", DataCategory: category}},
			Verdicts: []*mgmtv1alpha1.ColumnPiiVerdict{
				{Schema: "public", Table: "users", Column: "email", IsSensitive: true, DataCategory: category},
				{Schema: "public", Table: "users", Column: "id"},
			},
		}
	}
	require.NoError(t, store.Keep(ctx, connectionID, "public", "users", columns, scan("email")))
	require.NoError(t, store.Keep(ctx, connectionID, "public", "orders", columns[1:], &mgmtv1alpha1.DetectPiiInConnectionDataResponse{}))

	all, err := store.Read(ctx, connectionID, nil)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, "orders", all[0].GetTable())
	users := all[1]
	require.Equal(t, "users", users.GetTable())
	require.Equal(t, []string{"email", "id"}, []string{users.GetColumns()[0].GetColumn(), users.GetColumns()[1].GetColumn()})
	require.Equal(t, "integer", users.GetColumns()[1].GetDataType())
	require.Equal(t, "email", users.GetVerdicts()[0].GetDataCategory())
	require.True(t, users.GetVerdicts()[0].GetIsSensitive())
	require.NotNil(t, users.GetScannedAt())

	// A table is named by its schema and its name together: public.orders is not asked for.
	some, err := store.Read(ctx, connectionID, []*mgmtv1alpha1.PiiScanTable{
		{Schema: "public", Table: "users"}, {Schema: "archive", Table: "orders"},
	})
	require.NoError(t, err)
	require.Len(t, some, 1)
	require.Equal(t, "users", some[0].GetTable())

	require.NoError(t, store.Keep(ctx, connectionID, "public", "users", columns, scan("nir")))
	again, err := store.Read(ctx, connectionID, []*mgmtv1alpha1.PiiScanTable{{Schema: "public", Table: "users"}})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, "nir", again[0].GetVerdicts()[0].GetDataCategory())

	// Renaming the connection leaves its scans; pointing it at another server does not.
	_, err = queries.UpdateConnection(ctx, pool, db_queries.UpdateConnectionParams{
		ID: conn.ID, Name: "renamed", ConnectionConfig: pgConfig("db-a"), UpdatedByID: user.ID,
	})
	require.NoError(t, err)
	kept, err := store.Read(ctx, connectionID, nil)
	require.NoError(t, err)
	require.Len(t, kept, 2)

	_, err = queries.UpdateConnection(ctx, pool, db_queries.UpdateConnectionParams{
		ID: conn.ID, Name: "renamed", ConnectionConfig: pgConfig("db-b"), UpdatedByID: user.ID,
	})
	require.NoError(t, err)
	forgotten, err := store.Read(ctx, connectionID, nil)
	require.NoError(t, err)
	require.Empty(t, forgotten)
}
