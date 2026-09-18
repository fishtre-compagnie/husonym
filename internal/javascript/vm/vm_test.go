package javascript_vm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	"github.com/fishtre-compagnie/husonym/internal/testutil"

	"github.com/stretchr/testify/require"
)

func TestRunner(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		runner, err := NewRunner()
		require.NoError(t, err)

		program := goja.MustCompile("test.js", "1+1", true)
		result, err := runner.Run(context.Background(), program)
		require.NoError(t, err)
		require.Equal(t, int64(2), result.ToInteger())
	})

	t.Run("with_console", func(t *testing.T) {
		runner, err := NewRunner(WithConsole())
		require.NoError(t, err)

		program := goja.MustCompile("test.js", "console.log('hello world')", true)
		_, err = runner.Run(context.Background(), program)
		require.NoError(t, err)
	})

	t.Run("with_console_and_logger", func(t *testing.T) {
		runner, err := NewRunner(
			WithConsole(),
			WithLogger(testutil.GetTestLogger(t)),
		)
		require.NoError(t, err)

		program := goja.MustCompile("test.js", `console.log('hello world');`, true)
		_, err = runner.Run(context.Background(), program)
		require.NoError(t, err)
	})

	t.Run("parallel_runs", func(t *testing.T) {
		runner, err := NewRunner(
			WithConsole(),
			WithLogger(testutil.GetTestLogger(t)),
		)
		require.NoError(t, err)

		program := goja.MustCompile("test.js", `console.log('hello world');`, true)
		wg := sync.WaitGroup{}
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := runner.Run(context.Background(), program)
				require.NoError(t, err)
			}()
		}
		wg.Wait()
	})

	t.Run("with_global_alias_shares_the_object", func(t *testing.T) {
		customFn := javascript_functions.NewFunctionDefinition(
			"current",
			"hello",
			func(r javascript_functions.Runner) javascript_functions.Function {
				return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (any, error) {
					return "hello", nil
				}
			},
		)
		runner, err := NewRunner(WithFunctions(customFn), WithGlobalAlias("legacy", "current"))
		require.NoError(t, err)

		program := goja.MustCompile("test.js", `
			legacy.shared = "set through the alias";
			[legacy.hello(), current.shared].join(" / ");
		`, true)
		result, err := runner.Run(context.Background(), program)
		require.NoError(t, err)
		require.Equal(t, "hello / set through the alias", result.String())
	})

	t.Run("with_global_alias_on_undefined_target", func(t *testing.T) {
		_, err := NewRunner(WithGlobalAlias("legacy", "missing"))
		require.Error(t, err)
	})

	t.Run("with_functions", func(t *testing.T) {
		customFn := javascript_functions.NewFunctionDefinition(
			"test",
			"test",
			func(r javascript_functions.Runner) javascript_functions.Function {
				return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (any, error) {
					return "hello world", nil
				}
			},
		)

		runner, err := NewRunner(WithFunctions(customFn))
		require.NoError(t, err)

		program := goja.MustCompile("test.js", `test.test();`, true)
		result, err := runner.Run(context.Background(), program)
		require.NoError(t, err)
		require.Equal(t, "hello world", result.String())
	})
}

// A script never reads a file of the host: require() only knows the modules built into
// the runner.
func TestRunner_RequireLoadsNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"password":"do-not-read"}`), 0o600))
	runner, err := NewRunner(WithConsole())
	require.NoError(t, err)

	for _, module := range []string{path, strings.TrimSuffix(path, ".json")} {
		program := goja.MustCompile("test.js", fmt.Sprintf(`require(%q).password`, module), false)
		result, err := runner.Run(context.Background(), program)
		require.ErrorContains(t, err, ErrModuleFile.Error())
		require.Nil(t, result)
	}

	program := goja.MustCompile("test.js", `typeof require("console").log`, false)
	result, err := runner.Run(context.Background(), program)
	require.NoError(t, err)
	require.Equal(t, "function", result.String(), "the built-in modules stay available")
}

func TestRunner_TimeLimit(t *testing.T) {
	runner, err := NewRunner(WithTimeLimit(50 * time.Millisecond))
	require.NoError(t, err)

	start := time.Now()
	_, err = runner.Run(context.Background(), goja.MustCompile("loop.js", `while (true) {}`, false))
	require.ErrorIs(t, err, ErrTimeLimit)
	require.Less(t, time.Since(start), 2*time.Second)

	// The runner stays usable: the interrupt does not carry over to the next run.
	result, err := runner.Run(context.Background(), goja.MustCompile("next.js", `1+1`, false))
	require.NoError(t, err)
	require.Equal(t, int64(2), result.ToInteger())
}

func TestRunner_StopsWhenTheContextEnds(t *testing.T) {
	runner, err := NewRunner()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = runner.Run(ctx, goja.MustCompile("loop.js", `while (true) {}`, false))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
}

// The Go functions a script calls receive the context of the run, so that a call to a
// service ends with it.
func TestRunner_FunctionsReceiveTheRunContext(t *testing.T) {
	type key struct{}
	var seen any
	fn := javascript_functions.NewFunctionDefinition(
		"test",
		"probe",
		func(r javascript_functions.Runner) javascript_functions.Function {
			return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (any, error) {
				seen = ctx.Value(key{})
				return nil, nil
			}
		},
	)
	runner, err := NewRunner(WithFunctions(fn))
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), key{}, "run")
	_, err = runner.Run(ctx, goja.MustCompile("test.js", `test.probe()`, false))
	require.NoError(t, err)
	require.Equal(t, "run", seen)
}

func BenchmarkRunner_Single(b *testing.B) {
	runner, err := NewRunner(
		WithConsole(),
		WithLogger(testutil.GetTestLogger(b)),
	)
	require.NoError(b, err)

	program := goja.MustCompile("test.js", `console.log('hello world');`, true)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err = runner.Run(context.Background(), program)
		require.NoError(b, err)
	}
}
