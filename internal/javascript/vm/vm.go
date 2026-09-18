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

	// runCtx and runLogger are those of the run in progress, handed to the Go functions
	// the script calls and to its console.
	runCtx    context.Context
	runLogger *slog.Logger
	// interruptMu guards run, the token of the run in progress: an interrupt fired for a
	// run that has ended must not reach the next one.
	interruptMu sync.Mutex
	run         *struct{}

	// The sealed state every run starts from (see isolation.go).
	namespaces   []*namespace
	sealedGlobal *goja.Object
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
	preludes       []string
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

// WithPrelude runs code once, before the runner is sealed: what it defines on the built-in
// objects is there for every run, and frozen with them.
func WithPrelude(code string) Option {
	return func(opts *Options) {
		opts.preludes = append(opts.preludes, code)
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
	if options.logger == nil {
		options.logger = slog.Default()
	}

	vm := goja.New()
	runner := &Runner{
		vm:        vm,
		options:   options,
		runCtx:    context.Background(),
		runLogger: options.logger,
	}

	if options.consoleEnabled {
		// The registry's default loader reads the file system: a script could then
		// require() any JSON or JavaScript file of the host.
		registry := require.NewRegistry(require.WithLoader(refuseModuleFile))
		registry.RegisterNativeModule(
			console.ModuleName,
			console.RequireWithPrinter(newConsoleLogger(stdPrefix, func() *slog.Logger { return runner.runLogger })),
		)
		registry.Enable(vm)
		console.Enable(vm)
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

	if err := runner.seal(); err != nil {
		return nil, err
	}
	return runner, nil
}

func refuseModuleFile(string) ([]byte, error) {
	return nil, ErrModuleFile
}

// RunOption configures one run.
type RunOption func(*Runner)

// WithRunLogger prints the run's console and the logs of the functions it calls through
// logger, instead of the runner's.
func WithRunLogger(logger *slog.Logger) RunOption {
	return func(r *Runner) {
		if logger != nil {
			r.runLogger = logger
		}
	}
}

// Run runs a program, from the state the runner was sealed in: nothing a previous run
// left reaches it (see isolation.go). It stops, with an error wrapping ErrTimeLimit, when
// the program runs past the time limit, and with the context's error when the context
// ends first. The Go functions the program calls receive ctx.
func (r *Runner) Run(ctx context.Context, program *goja.Program, opts ...RunOption) (goja.Value, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.resetState(); err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(r)
	}

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
		r.runLogger = r.options.logger
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
		// A JavaScript object, not a wrapped Go map: sealing the runner freezes it.
		targetObj = runner.vm.NewObject()
		if err := runner.vm.GlobalObject().Set(function.Namespace(), targetObj); err != nil {
			return fmt.Errorf("failed to set global %s object: %w", function.Namespace(), err)
		}
	}

	if err := targetObj.Set(function.Name(), func(call goja.FunctionCall, rt *goja.Runtime) goja.Value {
		l := runner.runLogger.With("function", function.Name())
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
