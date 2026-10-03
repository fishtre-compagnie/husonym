package integrationtests_test

import (
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TransformPiiText through AnonymizeSingle, as a run and as a user call it: the text is
// rewritten where the analyzer finds personal data, and a hash is computed under the key a run
// hands with its call — a key the API reads from the worker alone.
func (s *IntegrationTestSuite) Test_AnonymizeService_AnonymizeSingle_PiiText() {
	t := s.T()
	ctx := s.ctx

	userclient := s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId(testAuthUserId))
	s.setUser(ctx, userclient)
	accountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	user := s.OSSAuthenticatedLicensedClients.Anonymize(integrationtests_test.WithUserId(testAuthUserId))
	worker := s.OSSAuthenticatedLicensedClients.Anonymize(integrationtests_test.WithUserId(apikey.NewV1WorkerKey()))

	s.Mocks.Presidio.OnAnalyze(presidiotest.Finding(t, "PERSON", "Zoé").Analyze)
	t.Cleanup(func() { s.Mocks.Presidio.OnAnalyze(nil) })

	// rewritten is what the caller gets for a note, under an anonymizer, with or without the
	// header of a hash key.
	rewritten := func(
		t *testing.T,
		client mgmtv1alpha1connect.AnonymizationServiceClient,
		anonymizer *mgmtv1alpha1.PiiAnonymizer,
		key *piitext.HashKey,
	) string {
		t.Helper()
		req := connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
			AccountId: accountId,
			InputData: `{"note":"appeler Zoé demain"}`,
			TransformerMappings: []*mgmtv1alpha1.TransformerMapping{{
				Expression: ".note",
				Transformer: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
						TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: anonymizer},
					},
				},
			}},
		})
		if key != nil {
			req.Header().Set(piitext.HashKeyHeader, key.Encode())
		}
		resp, err := client.AnonymizeSingle(ctx, req)
		require.NoError(t, err)
		var out struct {
			Note string `json:"note"`
		}
		require.NoError(t, json.Unmarshal([]byte(resp.Msg.GetOutputData()), &out))
		return out.Note
	}

	algo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
	hash := &mgmtv1alpha1.PiiAnonymizer{
		Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo}},
	}
	one, other := &piitext.HashKey{1}, &piitext.HashKey{2}

	t.Run("a finding after accented letters is rewritten where it is", func(t *testing.T) {
		require.Equal(t, "appeler <PERSON> demain", rewritten(t, user, nil, nil))
	})

	t.Run("a run gets the same hash for the same text under its key, and another under another key", func(t *testing.T) {
		first := rewritten(t, worker, hash, one)
		require.Regexp(t, `^appeler [0-9a-f]{64} demain$`, first)
		require.Equal(t, first, rewritten(t, worker, hash, one))
		require.NotEqual(t, first, rewritten(t, worker, hash, other))
	})

	t.Run("the key in the header of a caller that is not the worker is not used", func(t *testing.T) {
		withoutHeader := rewritten(t, user, hash, nil)
		require.Equal(t, withoutHeader, rewritten(t, user, hash, one), "the header changes nothing for a user")
		require.Equal(t, withoutHeader, rewritten(t, user, hash, other))
		require.NotEqual(t, rewritten(t, worker, hash, one), rewritten(t, user, hash, one),
			"a user does not get the hashes of a run by sending its key")
	})

	t.Run("without a key the worker gets the hashes of the process, as a user does", func(t *testing.T) {
		require.Equal(t, rewritten(t, user, hash, nil), rewritten(t, worker, hash, nil))
	})
}
