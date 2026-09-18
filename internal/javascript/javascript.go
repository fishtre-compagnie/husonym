package javascript

import (
	"log/slog"

	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	benthos_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/benthos"
	husonym_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/husonym"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
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
		javascript_vm.WithFunctions(functions...),
		// Code written for Neosync, before the rename, reaches these functions (and keeps
		// shared state) through the `neosync` global. It must keep working unchanged.
		javascript_vm.WithGlobalAlias(husonym_functions.LegacyNamespace, husonym_functions.Namespace),
	)
}

// Comes full featured but does not register any custom functions
func NewDefaultRunner(
	logger *slog.Logger,
) (*javascript_vm.Runner, error) {
	return javascript_vm.NewRunner(
		javascript_vm.WithLogger(logger),
		javascript_vm.WithConsole(),
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
