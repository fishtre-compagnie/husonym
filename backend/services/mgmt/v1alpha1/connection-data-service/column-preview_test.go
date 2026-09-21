package v1alpha1_connectiondataservice

import (
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	jsonanonymizer "github.com/fishtre-compagnie/husonym/internal/json-anonymizer"
	"github.com/stretchr/testify/require"
)

func Test_previewValues(t *testing.T) {
	t.Run("a constant shows as fewer distinct values after than before", func(t *testing.T) {
		// The signal the preview exists to give: three logins become one, which a unique index
		// would refuse on the second row. The run would only have said so after minutes.
		resp := previewValues(
			[]any{"alice", "bruno", "chloe"},
			func(any) (any, error) { return "demo", nil },
		)
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "demo", resp.GetValues()[0].GetOutput().GetValue())
	})

	t.Run("a failure is reported against its value and does not stop the others", func(t *testing.T) {
		resp := previewValues(
			[]any{"ok", "boom", "ok too"},
			func(v any) (any, error) {
				if v == "boom" {
					return nil, errors.New("cannot transform")
				}
				return v, nil
			},
		)
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "cannot transform", resp.GetValues()[1].GetError())
		require.Nil(t, resp.GetValues()[1].GetOutput())
		require.Equal(t, "ok too", resp.GetValues()[2].GetOutput().GetValue())
		// The failed value produced nothing, so it counts on one side only.
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(2), resp.GetDistinctOutputs())
	})

	t.Run("nulls are shown and left out of the counts", func(t *testing.T) {
		resp := previewValues(
			[]any{nil, "x"},
			func(v any) (any, error) { return v, nil },
		)
		require.True(t, resp.GetValues()[0].GetInput().GetIsNull())
		require.True(t, resp.GetValues()[0].GetOutput().GetIsNull())
		require.Equal(t, uint32(1), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
	})
}

func Test_transformValue(t *testing.T) {
	// Through the real anonymizer, so the wrapping — {"value": ...} and the .value expression —
	// is what is under test, not a stand-in for it.
	anonymizer, err := jsonanonymizer.NewAnonymizer(
		jsonanonymizer.WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression: ".value",
			Transformer: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
					TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{
						Code: `return value.toUpperCase();`,
					},
				},
			},
		}}),
	)
	require.NoError(t, err)

	out, err := transformValue(anonymizer, "alice")
	require.NoError(t, err)
	require.Equal(t, "ALICE", out)
}
