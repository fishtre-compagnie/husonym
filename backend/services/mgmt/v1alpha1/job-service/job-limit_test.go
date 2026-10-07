package v1alpha1_jobservice

import (
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_enforceJobLimit_RefusesWithTheTypedErrorOfTheCap(t *testing.T) {
	maxJobs := 1
	lic := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithLimits(&license.Limits{MaxJobs: &maxJobs}))
	user := userdatatest.NewUser(t, lic, userdata.NewMockEntityEnforcer(t))
	account := pgtype.UUID{Bytes: uuid.New(), Valid: true}

	querier := db_queries.NewMockQuerier(t)
	querier.On("GetJobsByAccount", mock.Anything, mock.Anything, account).
		Return([]db_queries.HusonymApiJob{{}}, nil)
	svc := &Service{db: husonymdb.New(husonymdb.NewMockDBTX(t), querier)}

	err := svc.enforceJobLimit(t.Context(), user, account)

	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	var refusal *license.Refusal
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, husonymdb.UUIDString(account), refusal.AccountId)
	require.Equal(t, []license.Gate{license.GateJobCap}, refusal.Gates)
	require.Equal(t, "this license allows 1 job(s) and 1 already exist; contact us to raise the limit", refusal.Message())
}
