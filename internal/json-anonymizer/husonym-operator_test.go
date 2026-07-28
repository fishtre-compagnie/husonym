package jsonanonymizer

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"

	"github.com/stretchr/testify/require"
)

func Test_HusonymOperator(t *testing.T) {
	t.Run("Transform", func(t *testing.T) {
		t.Run("string", func(t *testing.T) {
			operator := newHusonymOperatorApi([]transformer_executor.TransformerExecutorOption{
				transformer_executor.WithLogger(testutil.GetTestLogger(t)),
			})
			actual, err := operator.Transform(context.Background(), &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
					GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{},
				},
			}, "blah")
			require.NoError(t, err)
			require.NotEmpty(t, actual)
			require.IsType(t, "", actual)
		})
		t.Run("default_empty_string", func(t *testing.T) {
			operator := newHusonymOperatorApi([]transformer_executor.TransformerExecutorOption{
				transformer_executor.WithLogger(testutil.GetTestLogger(t)),
			})
			actual, err := operator.Transform(context.Background(), &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformFirstNameConfig{
					TransformFirstNameConfig: &mgmtv1alpha1.TransformFirstName{},
				},
			}, "")
			require.NoError(t, err)
			require.Empty(t, actual)
			require.IsType(t, "", actual)
		})
		t.Run("default_number", func(t *testing.T) {
			operator := newHusonymOperatorApi([]transformer_executor.TransformerExecutorOption{
				transformer_executor.WithLogger(testutil.GetTestLogger(t)),
			})
			actual, err := operator.Transform(context.Background(), &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateCardNumberConfig{
					GenerateCardNumberConfig: &mgmtv1alpha1.GenerateCardNumber{},
				},
			}, "")
			require.NoError(t, err)
			require.NotEmpty(t, actual)
			require.IsType(t, "", actual)
		})
	})
}
