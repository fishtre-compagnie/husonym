package v1alpha1_authservice

import (
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The sign-in of an account that declared its provider starts from what it declared. The service
// is built from its configuration, a token client and the database: it is given no license, so
// what an account declared while the sso feature was included is answered after it is closed
// exactly as before.
func Test_GetAccountLoginMethod_AnswersADeclaredProvider(t *testing.T) {
	querier := db_queries.NewMockQuerier(t)
	querier.On("GetAccountLoginMethodBySlug", mock.Anything, mock.Anything, "acme").Return(
		db_queries.GetAccountLoginMethodBySlugRow{Issuer: "https://idp.acme.example/", ClientID: "husonym"}, nil,
	)
	svc := New(&Config{}, nil, husonymdb.New(husonymdb.NewMockDBTX(t), querier))

	resp, err := svc.GetAccountLoginMethod(t.Context(), connect.NewRequest(
		&mgmtv1alpha1.GetAccountLoginMethodRequest{AccountSlug: "acme"},
	))

	require.NoError(t, err)
	require.Equal(t, "https://idp.acme.example/", resp.Msg.GetIssuer())
	require.Equal(t, "husonym", resp.Msg.GetClientId())
}
