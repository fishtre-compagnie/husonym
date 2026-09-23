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
