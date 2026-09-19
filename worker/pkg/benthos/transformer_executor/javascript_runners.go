package transformer_executor

import (
	"context"
	"log/slog"

	"github.com/dop251/goja"
	"github.com/fishtre-compagnie/husonym/internal/javascript"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/redpanda-data/benthos/v4/public/service"
)

// anonRunner is a sealed JavaScript runner and the value API its functions read and write.
type anonRunner struct {
	runner   *javascript_vm.Runner
	valueApi *AnonValueApi
}

// anonRunners serve every JavaScript transformer run on one message at a time: the
// transformers of this executor, and those of Athanor. A sealed runner costs milliseconds
// to build, and keeps nothing from one run to the next (see javascript_vm.Pool).
var anonRunners = javascript_vm.NewPool(func() (*anonRunner, error) {
	valueApi := NewAnonValueApi()
	runner, err := javascript.NewDefaultValueRunner(valueApi)
	if err != nil {
		return nil, err
	}
	return &anonRunner{runner: runner, valueApi: valueApi}, nil
})

// RunJavascript runs program on message, with transformPiiText calling piiTextApi (nil
// disables it) and the script's console printing through logger, and returns the message
// the program leaves.
func RunJavascript(
	ctx context.Context,
	program *goja.Program,
	message *service.Message,
	piiTextApi transformers.TransformPiiTextApi,
	logger *slog.Logger,
) (*service.Message, error) {
	r, err := anonRunners.Get()
	if err != nil {
		return nil, err
	}
	defer func() {
		r.valueApi.SetMessage(nil)
		anonRunners.Put(r)
	}()
	r.valueApi.SetMessage(message)
	if _, err := r.runner.Run(transformers.ContextWithPiiTextApi(ctx, piiTextApi), program,
		javascript_vm.WithRunLogger(logger)); err != nil {
		return nil, err
	}
	return r.valueApi.Message(), nil
}
