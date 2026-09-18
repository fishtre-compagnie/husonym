package javascript_vm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
)

// DefaultTimeLimit is how long one run of a program may take. A program transforms one
// row or one value, which takes microseconds: the limit only stops a script that would
// never end, and leaves room for a function that calls a service (transformPiiText).
const DefaultTimeLimit = 10 * time.Second

// ErrTimeLimit is the error a run stopped by the time limit wraps.
var ErrTimeLimit = errors.New("javascript: the script ran past its time limit")

// ErrModuleFile is what require() returns for anything but a module built into the
// runner: a script never reads a file of the machine it runs on.
var ErrModuleFile = errors.New("javascript: scripts cannot load files")

type Runner struct {
	vm      *goja.Runtime
	options Options
	mu      sync.Mutex

	// runCtx is the context of the run in progress, handed to the Go functions the
	// script calls.
	runCtx context.Context
	// interruptMu guards run, the token of the run in progress: an interrupt fired for a
	// run that has ended must not reach the next one.
	interruptMu sync.Mutex
	run         *struct{}
}

func (r *Runner) ValueApi() javascript_functions.ValueApi {
	return r.options.valueApi
}

type Options struct {
	logger         *slog.Logger
	functions      []*javascript_functions.FunctionDefinition
	consoleEnabled bool
	valueApi       javascript_functions.ValueApi
	globalAliases  map[string]string // alias -> target global
	timeLimit      time.Duration
}

type Option func(*Options)

// Sets the value api for the runner
// This allows custom functions to access an underlying data structure that can be manipulated by the runner
func WithValueApi(valueApi javascript_functions.ValueApi) Option {
	return func(opts *Options) {
		opts.valueApi = valueApi
	}
}

// Sets the logger for the runner
func WithLogger(logger *slog.Logger) Option {
	return func(opts *Options) {
		opts.logger = logger
	}
}

// Sets the functions for the runner
// These functions will be registered with the runner
// Functions may interact with the value api
func WithFunctions(functions ...*javascript_functions.FunctionDefinition) Option {
	return func(opts *Options) {
		opts.functions = functions
	}
}

// WithGlobalAlias exposes the global object `target` under a second name. Both names
// refer to the same object, so properties set through one are visible through the other.
func WithGlobalAlias(alias, target string) Option {
	return func(opts *Options) {
		if opts.globalAliases == nil {
			opts.globalAliases = map[string]string{}
		}
		opts.globalAliases[alias] = target
	}
}

// WithConsole exposes console, printed through the logger, and require() for the
// modules built into the runner: require() never loads a file.
func WithConsole() Option {
	return func(opts *Options) {
		opts.consoleEnabled = true
	}
}

// WithTimeLimit replaces DefaultTimeLimit.
func WithTimeLimit(limit time.Duration) Option {
	return func(opts *Options) {
		opts.timeLimit = limit
	}
}

// Creates a new JS Runner
func NewRunner(opts ...Option) (*Runner, error) {
	options := Options{logger: slog.Default(), timeLimit: DefaultTimeLimit}
	for _, opt := range opts {
		opt(&options)
	}

	vm := goja.New()

	if options.consoleEnabled {
		// The registry's default loader reads the file system: a script could then
		// require() any JSON or JavaScript file of the host.
		registry := require.NewRegistry(require.WithLoader(refuseModuleFile))
		if options.logger != nil {
			registry.RegisterNativeModule(
				console.ModuleName,
				console.RequireWithPrinter(newConsoleLogger(stdPrefix, options.logger)),
			)
		}
		registry.Enable(vm)
		console.Enable(vm)
	}

	runner := &Runner{
		vm:      vm,
		options: options,
		runCtx:  context.Background(),
	}

	for _, function := range options.functions {
		if err := registerFunction(runner, function); err != nil {
			return nil, err
		}
	}

	for alias, target := range options.globalAliases {
		targetValue := vm.GlobalObject().Get(target)
		if targetValue == nil {
			return nil, fmt.Errorf("cannot alias global %s: global %s is not defined", alias, target)
		}
		if err := vm.GlobalObject().Set(alias, targetValue); err != nil {
			return nil, fmt.Errorf("failed to set global alias %s: %w", alias, err)
		}
	}

	return runner, nil
}

func refuseModuleFile(string) ([]byte, error) {
	return nil, ErrModuleFile
}

// Run runs a program. It stops, with an error wrapping ErrTimeLimit, when the program
// runs past the time limit, and with the context's error when the context ends first.
func (r *Runner) Run(ctx context.Context, program *goja.Program) (goja.Value, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run := &struct{}{}
	r.interruptMu.Lock()
	r.run = run
	r.interruptMu.Unlock()
	interrupt := func(reason error) {
		r.interruptMu.Lock()
		defer r.interruptMu.Unlock()
		if r.run == run {
			r.vm.Interrupt(reason)
		}
	}
	timer := time.AfterFunc(r.options.timeLimit, func() {
		interrupt(fmt.Errorf("%w (%s)", ErrTimeLimit, r.options.timeLimit))
	})
	stopOnDone := context.AfterFunc(ctx, func() { interrupt(context.Cause(ctx)) })
	r.runCtx = ctx

	defer func() {
		timer.Stop()
		stopOnDone()
		r.runCtx = context.Background()
		r.interruptMu.Lock()
		r.run = nil
		r.interruptMu.Unlock()
		// No interrupt can be fired for this run any more: one fired too late, after the
		// program ended, would otherwise stop the next run at its start.
		r.vm.ClearInterrupt()
	}()
	return r.vm.RunProgram(program)
}

// Registers a custom function with the vm
func registerFunction(runner *Runner, function *javascript_functions.FunctionDefinition) error {
	var targetObj *goja.Object
	if targetObjValue := runner.vm.GlobalObject().Get(function.Namespace()); targetObjValue != nil {
		targetObj = targetObjValue.ToObject(runner.vm)
	}
	if targetObj == nil {
		if err := runner.vm.GlobalObject().Set(function.Namespace(), map[string]any{}); err != nil {
			return fmt.Errorf("failed to set global %s object: %w", function.Namespace(), err)
		}
		targetObj = runner.vm.GlobalObject().Get(function.Namespace()).ToObject(runner.vm)
	}

	if err := targetObj.Set(function.Name(), func(call goja.FunctionCall, rt *goja.Runtime) goja.Value {
		l := runner.options.logger.With("function", function.Name())
		fn := function.Ctor()(runner)
		result, err := fn(runner.runCtx, call, rt, l)
		if err != nil {
			// This _has_ to be a panic so that the error is properly thrown in the JS runtime
			// Otherwise things like try/catch will not work properly
			panic(rt.ToValue(err.Error()))
		}
		return rt.ToValue(result)
	}); err != nil {
		return fmt.Errorf(
			"failed to set global %s function %v: %w",
			function.Namespace(),
			function.Name(),
			err,
		)
	}
	return nil
}
