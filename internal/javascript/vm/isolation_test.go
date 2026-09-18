package javascript_vm

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dop251/goja"
	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	"github.com/stretchr/testify/require"
)

func isolationRunner(t *testing.T) *Runner {
	t.Helper()
	hello := javascript_functions.NewFunctionDefinition("current", "hello",
		func(r javascript_functions.Runner) javascript_functions.Function {
			return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (any, error) {
				return "hello", nil
			}
		})
	runner, err := NewRunner(WithConsole(), WithFunctions(hello), WithGlobalAlias("legacy", "current"))
	require.NoError(t, err)
	return runner
}

func run(t *testing.T, runner *Runner, code string) goja.Value {
	t.Helper()
	result, err := runner.Run(context.Background(), goja.MustCompile("test.js", code, false))
	require.NoError(t, err)
	return result
}

// Every way a script can hold state is empty at the next run.
func TestRunner_NoStateAcrossRuns(t *testing.T) {
	ways := map[string]struct{ set, get string }{
		"namespace":           {`current.kept = "row 1";`, `current.kept`},
		"namespace alias":     {`legacy.kept = "row 1";`, `current.kept`},
		"undeclared variable": {`kept = "row 1";`, `typeof kept === "undefined" ? undefined : kept`},
		"globalThis":          {`globalThis.kept = "row 1";`, `globalThis.kept`},
		"this":                {`this.kept = "row 1";`, `globalThis.kept`},
		"indirect eval var":   {`(0, eval)("var kept = 'row 1'");`, `globalThis.kept`},
		"defined global":      {`Object.defineProperty(globalThis, "kept", {value: "row 1"});`, `globalThis.kept`},
		"sealed global":       {`kept = "row 1"; Object.seal(globalThis);`, `globalThis.kept`},
		"sealed global object": {`Object.getPrototypeOf(globalThis).kept = "row 1";`,
			`Object.getPrototypeOf(globalThis).kept`},
		"global prototype":  {`Object.setPrototypeOf(globalThis, {kept: "row 1"});`, `globalThis.kept`},
		"global __proto__":  {`globalThis.__proto__ = {kept: "row 1"};`, `globalThis.kept`},
		"symbol global":     {`globalThis[Symbol.for("kept")] = "row 1";`, `globalThis[Symbol.for("kept")]`},
		"Object.prototype":  {`Object.prototype.kept = "row 1";`, `({}).kept`},
		"Array.prototype":   {`Array.prototype.kept = "row 1";`, `[].kept`},
		"String.prototype":  {`String.prototype.kept = "row 1";`, `"".kept`},
		"built-in object":   {`Math.kept = "row 1";`, `Math.kept`},
		"built-in replaced": {`JSON = {kept: "row 1"};`, `JSON.kept`},
		"console":           {`console.kept = "row 1";`, `console.kept`},
		"function":          {`current.hello.kept = "row 1";`, `current.hello.kept`},
		"namespace deleted": {`delete current; current = {kept: "row 1"};`, `current.kept`},
	}
	for name, way := range ways {
		t.Run(name, func(t *testing.T) {
			runner := isolationRunner(t)
			run(t, runner, way.set)
			require.True(t, goja.IsUndefined(run(t, runner, way.get)), "state of the previous run is still there")
			require.Equal(t, "hello", run(t, runner, `legacy.hello()`).String(), "the functions stay reachable")
		})
	}
}

// Within a run the columns of a row share their state, through the namespaces as through
// plain globals.
func TestRunner_StateWithinRun(t *testing.T) {
	runner := isolationRunner(t)
	result := run(t, runner, `
		legacy.picked = "Durand";
		chosen = "Marie";
		globalThis.city = "Lyon";
		this.zip = "69001";
		[current.picked, chosen, city, globalThis.zip].join(" ");`)
	require.Equal(t, "Durand Marie Lyon 69001", result.String())
}
