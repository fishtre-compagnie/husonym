package auth_apikey

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pkg_utils "github.com/fishtre-compagnie/husonym/backend/pkg/utils"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// specOf is the spec of a procedure as Connect hands it to the interceptor.
func specOf(service, method string) connect.Spec {
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(
		protoreflect.FullName("mgmt.v1alpha1." + service + "." + method),
	)
	if err != nil {
		panic(err)
	}
	return connect.Spec{
		Schema:    descriptor,
		Procedure: "/mgmt.v1alpha1." + service + "/" + method,
	}
}

// Every procedure declares what a scoped key needs: a new one is a decision about who may call
// it, never an opening nobody chose.
func Test_EveryProcedureDeclaresItsPermissions(t *testing.T) {
	// The files register when the package is loaded; name one so that it is.
	_ = mgmtv1alpha1.File_mgmt_v1alpha1_job_proto

	procedures := 0
	protoregistry.GlobalFiles.RangeFilesByPackage("mgmt.v1alpha1", func(file protoreflect.FileDescriptor) bool {
		services := file.Services()
		for i := range services.Len() {
			methods := services.Get(i).Methods()
			for j := range methods.Len() {
				method := methods.Get(j)
				procedures++
				_, ok := Requires(method)
				require.True(t, ok,
					"%s declares no permission: give it (mgmt.v1alpha1.requires), with the permissions "+
						"a scoped API key needs to call it, or none: true if it needs none", method.FullName())
			}
		}
		return true
	})
	require.Greater(t, procedures, 100, "the procedures were not found: the walk checked nothing")
}

func Test_Client_InjectTokenCtx_Account_Permissions(t *testing.T) {
	expiresAt, err := husonymdb.ToTimestamp(time.Now().Add(5 * time.Minute))
	require.NoError(t, err)
	call := func(t *testing.T, granted []string, spec connect.Spec) error {
		t.Helper()
		querier := db_queries.NewMockQuerier(t)
		client := New(querier, db_queries.NewMockDBTX(t), []string{}, []string{})
		token := apikey.NewV1AccountKey()
		querier.On("GetAccountApiKeyByKeyValue", mock.Anything, mock.Anything, pkg_utils.ToSha256(token)).
			Return(db_queries.HusonymApiAccountApiKey{
				ID: pgtype.UUID{Valid: true}, ExpiresAt: expiresAt, Permissions: granted,
			}, nil)
		_, err := client.InjectTokenCtx(context.Background(), http.Header{
			"Authorization": []string{fmt.Sprintf("Bearer %s", token)},
		}, spec)
		return err
	}

	t.Run("holding every permission the procedure requires", func(t *testing.T) {
		require.NoError(t, call(t, []string{"job:view", "job:execute"}, specOf("JobService", "CreateJobRun")))
	})

	t.Run("lacking one, told which", func(t *testing.T) {
		err := call(t, []string{"job:view"}, specOf("JobService", "CreateJobRun"))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.ErrorContains(t, err, "job:execute")
	})

	t.Run("a scope that names nothing allows nothing", func(t *testing.T) {
		err := call(t, nil, specOf("JobService", "GetJob"))
		require.ErrorContains(t, err, "job:view")
	})

	t.Run("a procedure that needs no permission, with no scope", func(t *testing.T) {
		require.NoError(t, call(t, nil, specOf("UserAccountService", "GetUserAccounts")))
	})

	t.Run("a procedure that declares nothing is refused", func(t *testing.T) {
		err := call(t, []string{"job:view"}, connect.Spec{Procedure: "/mgmt.v1alpha1.JobService/Unknown"})
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.ErrorContains(t, err, "declares no permission")
	})
}
