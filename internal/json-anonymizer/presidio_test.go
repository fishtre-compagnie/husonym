package jsonanonymizer

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/stretchr/testify/require"
)

// The caller of the anonymizer tells a Presidio that did not answer from a value that could not
// be transformed: the error of Presidio stays the cause of the one returned.
func Test_AnonymizeJSONObject_KeepsWhyPresidioFailed(t *testing.T) {
	presidioFake := presidiotest.New(t)
	presidioFake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return nil, errors.Join(presidio.ErrNoAnswer, errors.New("connection refused"))
	})
	anonymizer, err := NewAnonymizer(
		context.Background(),
		WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression: ".note",
			Transformer: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
					TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{},
				},
			},
		}}),
		WithConditionalAnonymizeConfig(true, presidioFake, presidioFake, nil),
	)
	require.NoError(t, err)

	_, err = anonymizer.AnonymizeJSONObject(`{"note":"call Jane"}`)

	require.ErrorIs(t, err, presidio.ErrNoAnswer)
	require.ErrorContains(t, err, "failed to anonymize JSON")
}
