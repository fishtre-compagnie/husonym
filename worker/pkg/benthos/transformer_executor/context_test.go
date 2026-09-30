package transformer_executor

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

type callerKey struct{}

// piiTextApiSeeingTheCaller records who asks, as the transformers service would.
type piiTextApiSeeingTheCaller struct{ caller any }

func (a *piiTextApiSeeingTheCaller) Transform(ctx context.Context, _ *mgmtv1alpha1.TransformPiiText, value string) (string, error) {
	a.caller = ctx.Value(callerKey{})
	return value, nil
}

// A PII rule hands what it finds to its transformers as it runs, a user-defined one resolved
// then: with the context of the request, which carries who asks.
func Test_TransformPiiText_RunsWithTheContextItWasBuiltWith(t *testing.T) {
	api := &piiTextApiSeeingTheCaller{}
	ctx := context.WithValue(context.Background(), callerKey{}, "the caller")
	executor, err := InitializeTransformerByConfigType(ctx, &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{}},
	}, WithTransformPiiTextApi(api))
	require.NoError(t, err)

	_, err = executor.Mutate("Ada", executor.Opts)
	require.NoError(t, err)
	require.Equal(t, "the caller", api.caller)
}
