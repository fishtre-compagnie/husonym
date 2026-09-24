package rowvalues

import (
	"reflect"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Each read of values must go through the consent in Preview. Before adding a method, make sure
// its call site does.
func Test_Clients_MethodsArePinned(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"PreviewColumnTransformer"}, testutil.MethodNames(reflect.TypeFor[valuesClient]()))
	require.Equal(t, []string{"GetSystemTransformerBySource"}, testutil.MethodNames(reflect.TypeFor[catalogClient]()))
}
