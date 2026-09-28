package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/google/uuid"
)

// With authentication on, the context of a run is written by the worker, never from the
// session of a person, even one allowed to edit the jobs of the account.
func (s *IntegrationTestSuite) Test_SetRunContext_NotFromASession() {
	t := s.T()
	users := s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId("run-context-writer"))
	s.setUser(s.ctx, users)
	accountId := s.createPersonalAccount(s.ctx, users)
	jobs := s.OSSAuthenticatedLicensedClients.Jobs(integrationtests_test.WithUserId("run-context-writer"))

	key := &mgmtv1alpha1.RunContextKey{AccountId: accountId, JobRunId: uuid.NewString(), ExternalId: "benthosconfig"}
	resp, err := jobs.SetRunContext(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{Id: key, Value: []byte("{}")}))
	requireErrResp(t, resp, err)
	requireConnectError(t, err, connect.CodePermissionDenied)

	stream := jobs.SetRunContexts(s.ctx)
	_ = stream.Send(&mgmtv1alpha1.SetRunContextsRequest{Id: key, Value: []byte("{}")})
	streamResp, err := stream.CloseAndReceive()
	requireErrResp(t, streamResp, err)
	requireConnectError(t, err, connect.CodePermissionDenied)

	// The worker, with its key, writes it.
	worker := s.OSSAuthenticatedLicensedClients.Jobs(integrationtests_test.WithUserId(apikey.NewV1WorkerKey()))
	resp, err = worker.SetRunContext(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{Id: key, Value: []byte("{}")}))
	requireNoErrResp(t, resp, err)
}
