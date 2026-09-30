package transformer_executor

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
)

type apiUserDefinedTransformerResolver struct {
	transformerClient mgmtv1alpha1connect.TransformersServiceClient
	accountId         string
}

// NewUserDefinedTransformerResolver resolves, through the transformers API, the user-defined
// transformers of one account: that of the job or the request whose rules they are. The API
// gives the worker any transformer, so the account is checked here.
func NewUserDefinedTransformerResolver(
	transformerClient mgmtv1alpha1connect.TransformersServiceClient,
	accountId string,
) UserDefinedTransformerResolver {
	return &apiUserDefinedTransformerResolver{transformerClient: transformerClient, accountId: accountId}
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
	transformer := resp.Msg.GetTransformer()
	if !strings.EqualFold(transformer.GetAccountId(), u.accountId) {
		return nil, connect.NewError(
			connect.CodeNotFound,
			fmt.Errorf("unable to find user defined transformer %s in account %s", id, u.accountId),
		)
	}
	// A user-defined transformer runs a transformer of the catalog. One stored before that was
	// refused names another one, maybe itself: resolving it would never end.
	if nested := job_util.UserDefinedTransformerIds(transformer.GetConfig()); len(nested) > 0 {
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("user defined transformer %s runs another user defined transformer: %s", id, nested[0]),
		)
	}
	return transformer.GetConfig(), nil
}
