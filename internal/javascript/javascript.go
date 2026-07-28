package javascript

import (
	"log/slog"

	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	benthos_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/benthos"
	husonym_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/husonym"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	goja_require "github.com/dop251/goja_nodejs/require"
)

// Comes full featured, but expects a value api that the benthos/husonym functions can manipulate
func NewDefaultValueRunner(
	valueApi javascript_functions.ValueApi,
	transformPiiTextApi transformers.TransformPiiTextApi,
	logger *slog.Logger,
) (*javascript_vm.Runner, error) {
	functions, err := getDefaultFunctions(transformPiiTextApi)
	if err != nil {
		return nil, err
	}
	return javascript_vm.NewRunner(
		javascript_vm.WithValueApi(valueApi),
		javascript_vm.WithLogger(logger),
		javascript_vm.WithConsole(),
		javascript_vm.WithJsRegistry(goja_require.NewRegistry()),
		javascript_vm.WithFunctions(functions...),
	)
}

// Comes full featured but does not register any custom functions
func NewDefaultRunner(
	logger *slog.Logger,
) (*javascript_vm.Runner, error) {
	return javascript_vm.NewRunner(
		javascript_vm.WithLogger(logger),
		javascript_vm.WithConsole(),
		javascript_vm.WithJsRegistry(goja_require.NewRegistry()),
	)
}

func getDefaultFunctions(
	transformPiiTextApi transformers.TransformPiiTextApi,
) ([]*javascript_functions.FunctionDefinition, error) {
	benthosFns := benthos_functions.Get()
	husonymFns, err := husonym_functions.Get(transformPiiTextApi)
	if err != nil {
		return nil, err
	}
	output := make([]*javascript_functions.FunctionDefinition, 0, len(benthosFns)+len(husonymFns))
	output = append(output, benthosFns...)
	output = append(output, husonymFns...)
	return output, nil
}
