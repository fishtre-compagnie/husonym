package novalues

import (
	"reflect"
	"testing"

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
}

// The Reader holds its clients through the pinned interfaces and through nothing else: a second
// client, or a concrete one, would reach methods no test pins.
func Test_Reader_HoldsOnlyPinnedClients(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[Reader]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[dataClient]()}, testutil.InterfaceFields(typ))
	for _, pkg := range testutil.FieldPackages(typ) {
		require.NotContains(t, []string{
			"connectrpc.com/connect",
			"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect",
		}, pkg)
	}
}
