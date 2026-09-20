package jsonanonymizer

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
)

type husonymOperatorApi struct {
	opts []transformer_executor.TransformerExecutorOption
}

func newHusonymOperatorApi(
	executorOpts []transformer_executor.TransformerExecutorOption,
) *husonymOperatorApi {
	return &husonymOperatorApi{opts: executorOpts}
}

func (n *husonymOperatorApi) Transform(
	ctx context.Context,
	config *mgmtv1alpha1.TransformerConfig,
	value string,
) (string, error) {
	executor, err := transformer_executor.InitializeTransformerByConfigType(config, n.opts...)
	if err != nil {
		return "", err
	}
	result, err := executor.Mutate(value, executor.Opts)
	if err != nil {
		return "", err
	}

	switch result := result.(type) {
	case string:
		return result, nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", derefPointer(result)), nil
	}
}
