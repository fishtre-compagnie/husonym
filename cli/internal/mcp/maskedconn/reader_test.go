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
