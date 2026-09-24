package rowvalues

import (
	"reflect"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Each read of values must go through the consent. Before adding a method, make sure its call
// site does.
func Test_Clients_MethodsArePinned(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"PreviewColumnTransformer"}, testutil.MethodNames(reflect.TypeFor[valuesClient]()))
	require.Equal(t, []string{"GetJobRun", "GetJobRunEvents"}, testutil.MethodNames(reflect.TypeFor[runClient]()))
}

// The Reader holds its clients through the pinned interfaces and through nothing else: a second
// client, or a concrete one, would reach methods no test pins.
func Test_Reader_HoldsOnlyPinnedClients(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[Reader]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[valuesClient](), reflect.TypeFor[runClient]()}, testutil.InterfaceFields(typ))
	for _, pkg := range testutil.FieldPackages(typ) {
		require.NotContains(t, []string{
			"connectrpc.com/connect",
			"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect",
		}, pkg)
	}
}
