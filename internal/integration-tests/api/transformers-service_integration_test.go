package integrationtests_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

func (s *IntegrationTestSuite) Test_TransformersService_GetSystemTransformers() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Transformers().
		GetSystemTransformers(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemTransformersRequest{}))
	requireNoErrResp(s.T(), resp, err)
	require.NotEmpty(s.T(), resp.Msg.GetTransformers())
}

func (s *IntegrationTestSuite) Test_TransformersService_GetSystemTransformersBySource() {
	t := s.T()
	t.Run("ok", func(t *testing.T) {
		resp, err := s.OSSUnauthenticatedLicensedClients.Transformers().
			GetSystemTransformerBySource(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemTransformerBySourceRequest{
				Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL,
			}))
		requireNoErrResp(t, resp, err)
		transformer := resp.Msg.GetTransformer()
		require.NotNil(t, transformer)
		require.Equal(
			t,
			transformer.GetSource(),
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL,
		)
	})
	t.Run("not_found", func(t *testing.T) {
		resp, err := s.OSSUnauthenticatedLicensedClients.Transformers().
			GetSystemTransformerBySource(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemTransformerBySourceRequest{
				Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
			}))
		requireErrResp(t, resp, err)
		requireConnectError(s.T(), err, connect.CodeNotFound)
	})
}

func (s *IntegrationTestSuite) Test_TransformersService_GetTransformPiiRecognizers() {
	t := s.T()

	t.Cleanup(func() { s.Mocks.Presidio.OnSupportedEntities(nil) })
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	getEntities := func() (*connect.Response[mgmtv1alpha1.GetTransformPiiEntitiesResponse], error) {
		return s.OSSUnauthenticatedLicensedClients.Transformers().
			GetTransformPiiEntities(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetTransformPiiEntitiesRequest{
				AccountId: accountId,
			}))
	}

	t.Run("ok", func(t *testing.T) {
		allowed := []string{"foo", "bar"}
		var asked []string
		s.Mocks.Presidio.OnSupportedEntities(func(_ context.Context, language string) ([]string, error) {
			asked = append(asked, language)
			return allowed, nil
		})

		resp, err := getEntities()
		requireNoErrResp(t, resp, err)
		recognizers := resp.Msg.GetEntities()
		require.Equal(t, allowed, recognizers)
		// A deployment that sets no default language lists the entities of English.
		require.Equal(t, []string{"en"}, asked)
	})

	t.Run("a Presidio that does not answer is unavailable, and where it is reached is not told", func(t *testing.T) {
		s.Mocks.Presidio.OnSupportedEntities(func(context.Context, string) ([]string, error) {
			return nil, errors.Join(presidio.ErrNoAnswer, errors.New("dial tcp presidio.internal:3000: connection refused"))
		})

		resp, err := getEntities()
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodeUnavailable)
		require.NotContains(t, err.Error(), "presidio.internal")
	})

	t.Run("a Presidio that refuses the request tells why", func(t *testing.T) {
		s.Mocks.Presidio.OnSupportedEntities(func(context.Context, string) ([]string, error) {
			return nil, &presidio.RefusedError{
				Operation: "supported entities", StatusCode: http.StatusBadRequest, Message: "unknown language",
			}
		})

		resp, err := getEntities()
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodeInvalidArgument)
		require.ErrorContains(t, err, "unknown language")
	})
}
