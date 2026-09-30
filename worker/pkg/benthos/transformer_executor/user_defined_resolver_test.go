package transformer_executor

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The API gives the worker the transformer of any account: the resolver gives only those of
// the account it resolves for, and one of another account reads as absent.
func Test_UserDefinedTransformerResolver_AccountOfTheTransformer(t *testing.T) {
	const accountId = "8a7a4b2e-5f0e-4c8e-9d61-3f3c2c7b9a10"
	config := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: "return value;"},
	}}
	client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
	answer := func(id, owner string) {
		client.EXPECT().GetUserDefinedTransformerById(mock.Anything, mock.MatchedBy(
			func(req *connect.Request[mgmtv1alpha1.GetUserDefinedTransformerByIdRequest]) bool {
				return req.Msg.GetTransformerId() == id
			},
		)).Return(connect.NewResponse(&mgmtv1alpha1.GetUserDefinedTransformerByIdResponse{
			Transformer: &mgmtv1alpha1.UserDefinedTransformer{Id: id, AccountId: owner, Config: config},
		}), nil)
	}
	answer("own", accountId)
	answer("foreign", "0d4f6a55-2b8e-4a47-8f2e-6c1d9b3e7a21")
	resolver := NewUserDefinedTransformerResolver(client, strings.ToUpper(accountId))

	resolved, err := resolver.GetUserDefinedTransformer(context.Background(), "own")
	require.NoError(t, err)
	require.Equal(t, config, resolved)

	_, err = resolver.GetUserDefinedTransformer(context.Background(), "foreign")
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "%v", err)
}

// A user-defined transformer stored before nesting was refused may name another one, or itself:
// it is refused rather than resolved without end.
func Test_UserDefinedTransformerResolver_RunsNoUserDefinedTransformer(t *testing.T) {
	const accountId = "8a7a4b2e-5f0e-4c8e-9d61-3f3c2c7b9a10"
	client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
	client.EXPECT().GetUserDefinedTransformerById(mock.Anything, mock.Anything).Return(
		connect.NewResponse(&mgmtv1alpha1.GetUserDefinedTransformerByIdResponse{
			Transformer: &mgmtv1alpha1.UserDefinedTransformer{Id: "self", AccountId: accountId, Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
					UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "self"},
				},
			}},
		}), nil).Once()
	resolver := NewUserDefinedTransformerResolver(client, accountId)

	_, err := InitializeTransformerByConfigType(&mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
			UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "self"},
		},
	}, WithUserDefinedTransformerResolver(resolver))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "%v", err)
}
