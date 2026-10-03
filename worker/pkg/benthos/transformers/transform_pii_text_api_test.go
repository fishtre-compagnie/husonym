package transformers

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/stretchr/testify/require"
)

// anonymizationService records what the worker sends with a text.
type anonymizationService struct {
	mgmtv1alpha1connect.AnonymizationServiceClient
	header http.Header
	msg    *mgmtv1alpha1.AnonymizeSingleRequest
}

func (s *anonymizationService) AnonymizeSingle(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.AnonymizeSingleRequest],
) (*connect.Response[mgmtv1alpha1.AnonymizeSingleResponse], error) {
	s.header, s.msg = req.Header().Clone(), req.Msg
	return connect.NewResponse(&mgmtv1alpha1.AnonymizeSingleResponse{OutputData: `{"input":"rewritten"}`}), nil
}

func Test_AccountAwareAnonymizationPiiTextApi(t *testing.T) {
	config := &mgmtv1alpha1.TransformPiiText{ScoreThreshold: 0.5}

	t.Run("a text goes to the API with its configuration and its account", func(t *testing.T) {
		service := &anonymizationService{}
		out, err := NewAccountAwareAnonymizationPiiTextApi(service, "the-account").
			Transform(context.Background(), config, "a text")
		require.NoError(t, err)
		require.Equal(t, "rewritten", out)
		require.Equal(t, "the-account", service.msg.GetAccountId())
		require.JSONEq(t, `{"input":"a text"}`, service.msg.GetInputData())
		require.Same(t, config, service.msg.GetTransformerMappings()[0].GetTransformer().GetTransformPiiTextConfig())
	})

	t.Run("without a consistency scope no key goes with the text", func(t *testing.T) {
		service := &anonymizationService{}
		_, err := NewAccountAwareAnonymizationPiiTextApi(service, "the-account").
			Transform(context.Background(), config, "a text")
		require.NoError(t, err)
		require.Empty(t, service.header.Values(piitext.HashKeyHeader))
	})

	t.Run("the hash key of the run goes with every text, and nothing else of the scope does", func(t *testing.T) {
		service := &anonymizationService{}
		key := piitext.HashKey{1, 2, 3}
		api := NewAccountAwareAnonymizationPiiTextApi(service, "the-account").WithHashKey(key)
		for range 2 {
			_, err := api.Transform(context.Background(), config, "a text")
			require.NoError(t, err)

			sent, err := piitext.ParseHashKey(service.header.Get(piitext.HashKeyHeader))
			require.NoError(t, err)
			require.Equal(t, key, sent)
			// The key is the only thing the request carries beyond the text, its configuration
			// and its account.
			require.Len(t, service.header, 1)
			require.JSONEq(t, `{"input":"a text"}`, service.msg.GetInputData())
		}
	})
}
