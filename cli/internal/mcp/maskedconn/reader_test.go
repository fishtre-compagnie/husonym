package maskedconn

import (
	"reflect"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Every method of connectionClient must be sent with exclude_sensitive. Adding one is fine,
// once its call site sets the flag: this list is here so that it is added on purpose.
func Test_ConnectionClient_MethodsArePinned(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"GetConnection", "GetConnections"}, testutil.MethodNames(reflect.TypeFor[connectionClient]()))
}

// The Reader holds its clients through the pinned interfaces and through nothing else: a second
// client, or a concrete one, would reach methods no test pins.
func Test_Reader_HoldsOnlyPinnedClients(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[Reader]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[connectionClient]()}, testutil.InterfaceFields(typ))
	for _, pkg := range testutil.FieldPackages(typ) {
		require.NotContains(t, []string{
			"connectrpc.com/connect",
			"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect",
		}, pkg)
	}
}
