package novalues

import (
	"reflect"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// None of these answers with a value read from a row. Before adding one, check what its
// response carries: a sample, a preview or a stream does not belong here.
func Test_DataClient_MethodsArePinned(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{
		"DetectPiiInConnectionData",
		"GetAllSchemasAndTables",
		"GetConnectionSchema",
		"GetConnectionTableConstraints",
	}, testutil.MethodNames(reflect.TypeFor[dataClient]()))
	require.Equal(t, []string{"GetSystemTransformerBySource"}, testutil.MethodNames(reflect.TypeFor[catalogClient]()))
	require.Equal(t, []string{"ValidateJobMappings"}, testutil.MethodNames(reflect.TypeFor[validationClient]()))
}

// The Reader holds its clients through the pinned interfaces and through nothing else: a second
// client, or a concrete one, would reach methods no test pins.
func Test_Reader_HoldsOnlyPinnedClients(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[Reader]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[dataClient](), reflect.TypeFor[catalogClient](), reflect.TypeFor[validationClient]()}, testutil.InterfaceFields(typ))
	for _, pkg := range testutil.FieldPackages(typ) {
		require.NotContains(t, []string{
			"connectrpc.com/connect",
			"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect",
		}, pkg)
	}
}

// Code is recognised by the field that carries it, not by a list of transformers.
func Test_RunsCode(t *testing.T) {
	t.Parallel()
	require.True(t, RunsCode(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{},
	}}), "empty code is still a place for code")
	require.True(t, RunsCode(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateJavascriptConfig{
		GenerateJavascriptConfig: &mgmtv1alpha1.GenerateJavascript{Code: "return 1"},
	}}))
	require.False(t, RunsCode(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
		GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
	}}))
	require.False(t, RunsCode(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
		PassthroughConfig: &mgmtv1alpha1.Passthrough{},
	}}))
}
