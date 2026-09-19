package javascript_processor

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/dop251/goja"
	"github.com/fishtre-compagnie/husonym/internal/benthos_slogger"
	"github.com/fishtre-compagnie/husonym/internal/javascript"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"

	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	codeField = "code"
)

func javascriptProcessorConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Field(service.NewInterpolatedStringField(codeField))
}

func RegisterHusonymJavascriptProcessor(
	env *service.Environment,
	transformPiiTextApi transformers.TransformPiiTextApi,
) error {
	return env.RegisterBatchProcessor(
		"husonym_javascript", javascriptProcessorConfig(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchProcessor, error) {
			return newJavascriptProcessorFromConfig(conf, mgr, transformPiiTextApi)
		})
}

type javascriptProcessor struct {
	program    *goja.Program
	slogger    *slog.Logger
	piiTextApi transformers.TransformPiiTextApi
}

// vmPool holds the runners of every JavaScript processor of the process: a sealed runner
// costs milliseconds to build, and one built in the middle of a stream delays its rows
// past the flush of their page (see javascript_vm.Pool).
var vmPool = javascript_vm.NewPool(newPoolItem)

func newJavascriptProcessorFromConfig(
	conf *service.ParsedConfig,
	mgr *service.Resources,
	transformPiiTextApi transformers.TransformPiiTextApi,
) (*javascriptProcessor, error) {
	code, err := conf.FieldString(codeField)
	if err != nil {
		return nil, err
	}

	filename := "main.js"
	program, err := javascript_vm.Compile(filename, code)
	if err != nil {
		return nil, fmt.Errorf("failed to compile javascript code: %v", err)
	}

	// Benthos runs a pipeline on one thread per CPU (threads: -1): each needs a runner as
	// its first batch comes, and one built then delays its rows past the flush of their
	// page. The pool keeps them for the life of the process.
	if err := vmPool.Reserve(runtime.GOMAXPROCS(0)); err != nil {
		return nil, fmt.Errorf("failed to build the javascript runners: %w", err)
	}

	logger := mgr.Logger()
	slogger := benthos_slogger.NewSlogger(logger)

	return &javascriptProcessor{
		program:    program,
		slogger:    slogger,
		piiTextApi: transformPiiTextApi,
	}, nil
}

type vmPoolItem struct {
	runner   *javascript_vm.Runner
	valueApi *benthosValueApi
}

func (j *javascriptProcessor) ProcessBatch(
	ctx context.Context,
	batch service.MessageBatch,
) (result []service.MessageBatch, err error) {
	var runner *javascript_vm.Runner
	var valueApi *benthosValueApi

	poolItem, err := vmPool.Get()
	if err != nil {
		return nil, err
	}
	runner = poolItem.runner
	valueApi = poolItem.valueApi
	defer func() {
		poolItem.valueApi.SetMessage(nil) // reset the message to nil
		vmPool.Put(poolItem)
	}()

	// Add panic recovery for the entire batch processing
	// Goja has panic recovery built in, but if it encounters an uncatchable panic
	// An uncatchable panic is one that happens in a Go function called by JS.
	// Goja looks for a special `uncatchableException` error type and traps that in the panic.
	// For anything else though, it will re-panic with the original paniced error. /facepalm
	// This here acts as a final catch-all defense for anything we missed so prevent the process from crashing.
	defer func() {
		if r := recover(); r != nil {
			j.slogger.Error(
				"recovered from panic in husonym_javascript batch processor",
				"error",
				fmt.Sprintf("%v", r),
			)
			// Set the named return value 'err'
			err = fmt.Errorf("husonym_javascript batch processor panic recovered: %v", r)
			return
		}
	}()

	var newBatch service.MessageBatch
	runCtx := transformers.ContextWithPiiTextApi(ctx, j.piiTextApi)

	for i := range batch {
		valueApi.SetMessage(batch[i])
		_, err := runner.Run(runCtx, j.program, javascript_vm.WithRunLogger(j.slogger))
		if err != nil {
			return nil, err
		}
		if newMsg := valueApi.Message(); newMsg != nil {
			newBatch = append(newBatch, newMsg)
		}
	}

	return []service.MessageBatch{newBatch}, nil
}

func (j *javascriptProcessor) Close(ctx context.Context) error {
	return nil
}

func newPoolItem() (*vmPoolItem, error) {
	valueApi := newBatchBenthosValueApi()
	runner, err := javascript.NewDefaultValueRunner(valueApi)
	if err != nil {
		return nil, err
	}
	return &vmPoolItem{
		valueApi: valueApi,
		runner:   runner,
	}, nil
}
