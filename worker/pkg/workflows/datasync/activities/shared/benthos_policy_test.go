package shared

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

type mapResolver map[string]*mgmtv1alpha1.TransformerConfig

func (r mapResolver) GetUserDefinedTransformer(_ context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
	return r[id], nil
}

func javascriptMapping(column string, cfg *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMapping {
	return &mgmtv1alpha1.JobMapping{Schema: "public", Table: "client", Column: column,
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: cfg}}
}

func transformJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func TestBenthosRuns(t *testing.T) {
	resolver := mapResolver{"udt": transformJavascript(`return pseudo.lastName(value);`)}
	userDefined := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
		UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "udt"},
	}}

	plain := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{
		javascriptMapping("nom", transformJavascript(`return value.toUpperCase();`)),
		javascriptMapping("code", transformJavascript(`return (;`)), // fails its table, as before
	}}
	require.NoError(t, BenthosRuns(context.Background(), plain, resolver))

	inline := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{
		javascriptMapping("nom", transformJavascript(`return pseudo.lastName(value);`)),
	}}
	require.ErrorContains(t, BenthosRuns(context.Background(), inline, resolver), "public.client.nom")

	viaUserDefined := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{javascriptMapping("prenom", userDefined)}}
	require.ErrorContains(t, BenthosRuns(context.Background(), viaUserDefined, resolver),
		"benthos cannot run the pseudo functions of the rule of public.client.prenom")
}

// What an engine cannot run is told apart from a failure to find out: the first is a
// finding of the pre-flight check, the second fails it.
func TestEngineUnsupportedError(t *testing.T) {
	var unsupported *EngineUnsupportedError

	inline := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{
		javascriptMapping("nom", transformJavascript(`return pseudo.lastName(value);`)),
	}}
	require.ErrorAs(t, BenthosRuns(context.Background(), inline, mapResolver{}), &unsupported)
	require.ErrorAs(t, AthanorRuns(&mgmtv1alpha1.Job{}), &unsupported)

	unresolved := &mgmtv1alpha1.Job{Mappings: []*mgmtv1alpha1.JobMapping{javascriptMapping("prenom",
		&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
			UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "gone"},
		}})}}
	err := BenthosRuns(context.Background(), unresolved, failingResolver{})
	require.Error(t, err)
	require.NotErrorAs(t, err, &unsupported)
}

type failingResolver struct{}

func (failingResolver) GetUserDefinedTransformer(context.Context, string) (*mgmtv1alpha1.TransformerConfig, error) {
	return nil, errors.New("api unavailable")
}
