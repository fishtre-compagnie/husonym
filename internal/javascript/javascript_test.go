package javascript

import (
	"context"
	"testing"

	"github.com/dop251/goja"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/stretchr/testify/require"
)

type fakePiiTextApi struct{ output string }

func (f fakePiiTextApi) Transform(context.Context, *mgmtv1alpha1.TransformPiiText, string) (string, error) {
	return f.output, nil
}

// A runner serves any account: transformPiiText calls the API the run comes with.
func TestDefaultValueRunner_PiiTextApiComesWithTheRun(t *testing.T) {
	runner, err := NewDefaultValueRunner(nil)
	require.NoError(t, err)
	program := goja.MustCompile("test.js", `husonym.transformPiiText("Jean Dupont", {})`, false)

	for _, account := range []string{"<PERSONNE A>", "<PERSONNE B>"} {
		ctx := transformers.ContextWithPiiTextApi(context.Background(), fakePiiTextApi{output: account})
		result, err := runner.Run(ctx, program)
		require.NoError(t, err)
		require.Equal(t, account, result.String())
	}

	_, err = runner.Run(context.Background(), program)
	require.ErrorContains(t, err, "transformPiiText is not available here")
}
