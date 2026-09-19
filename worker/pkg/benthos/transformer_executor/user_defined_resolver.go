package transformer_executor

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

type apiUserDefinedTransformerResolver struct {
	transformerClient mgmtv1alpha1connect.TransformersServiceClient
}

// NewUserDefinedTransformerResolver resolves user-defined transformers through the
// transformers API.
func NewUserDefinedTransformerResolver(
	transformerClient mgmtv1alpha1connect.TransformersServiceClient,
) UserDefinedTransformerResolver {
	return &apiUserDefinedTransformerResolver{transformerClient: transformerClient}
}

func (u *apiUserDefinedTransformerResolver) GetUserDefinedTransformer(
	ctx context.Context,
	id string,
) (*mgmtv1alpha1.TransformerConfig, error) {
	resp, err := u.transformerClient.GetUserDefinedTransformerById(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetUserDefinedTransformerByIdRequest{
			TransformerId: id,
		}),
	)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTransformer().GetConfig(), nil
}
