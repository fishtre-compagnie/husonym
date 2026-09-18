package javascript

import (
	"log/slog"

	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	benthos_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/benthos"
	husonym_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/husonym"
	pseudo_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions/pseudo"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
)

// NewDefaultValueRunner comes full featured, but expects a value api that the
// benthos/husonym functions can manipulate. The runner is sealed and may serve any job:
// each run is handed its logger (javascript_vm.WithRunLogger) and, through its context, the
// PII text API of its account (transformers.ContextWithPiiTextApi) and, under Athanor, the
// consistency scope the pseudo functions derive from (pseudo_functions.ContextWithSource).
func NewDefaultValueRunner(valueApi javascript_functions.ValueApi) (*javascript_vm.Runner, error) {
	functions, err := getDefaultFunctions()
	if err != nil {
		return nil, err
	}
	return javascript_vm.NewRunner(
		javascript_vm.WithValueApi(valueApi),
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

func getDefaultFunctions() ([]*javascript_functions.FunctionDefinition, error) {
	benthosFns := benthos_functions.Get()
	husonymFns, err := husonym_functions.Get()
	if err != nil {
		return nil, err
	}
	pseudoFns := pseudo_functions.Get()
	output := make([]*javascript_functions.FunctionDefinition, 0, len(benthosFns)+len(husonymFns)+len(pseudoFns))
	output = append(output, benthosFns...)
	output = append(output, husonymFns...)
	output = append(output, pseudoFns...)
	return output, nil
}
