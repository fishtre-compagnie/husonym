package v1alpha1_connectionservice

import (
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
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

func Test_enforceConnectionLimits_RefuseWithTheTypedErrorOfTheirGate(t *testing.T) {
	account := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	postgres := &mgmtv1alpha1.ConnectionConfig{
		Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
	}
	maxConnections := 1

	tests := map[string]struct {
		limits  *license.Limits
		gate    license.Gate
		message string
	}{
		"a type the license does not allow": {
			limits:  &license.Limits{AllowedConnectionTypes: []string{"mysql"}},
			gate:    license.GateConnectionType,
			message: "this license does not include postgres connections; contact us to add it",
		},
		"the cap on connections": {
			limits:  &license.Limits{MaxConnections: &maxConnections},
			gate:    license.GateConnectionCap,
			message: "this license allows 1 connection(s) and 1 already exist; contact us to raise the limit",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			lic := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithLimits(tc.limits))
			user := userdatatest.NewUser(t, lic, userdata.NewMockEntityEnforcer(t))
			querier := db_queries.NewMockQuerier(t)
			if tc.gate == license.GateConnectionCap {
				querier.On("GetConnectionsByAccount", mock.Anything, mock.Anything, account).
					Return([]db_queries.HusonymApiConnection{{}}, nil)
			}
			svc := &Service{db: husonymdb.New(husonymdb.NewMockDBTX(t), querier)}

			err := svc.enforceConnectionLimits(t.Context(), user, account, postgres)

			require.Error(t, err)
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			var refusal *license.Refusal
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, husonymdb.UUIDString(account), refusal.AccountId)
			require.Equal(t, []license.Gate{tc.gate}, refusal.Gates)
			require.Equal(t, tc.message, refusal.Message())
		})
	}
}
