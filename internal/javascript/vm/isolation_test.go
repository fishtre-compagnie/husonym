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
	// Compile, and not goja.Compile: it is how a run is compiled everywhere, and part of
	// what keeps one run from reaching the next.
	program, err := Compile("test.js", code)
	require.NoError(t, err)
	result, err := runner.Run(context.Background(), program)
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
		// Declarations of the global lexical environment: they never reach the global
		// object, so replacing it leaves them where they are.
		"let":   {`let kept = "row 1";`, `typeof kept === "undefined" ? undefined : kept`},
		"const": {`const kept = "row 1";`, `typeof kept === "undefined" ? undefined : kept`},
		"class": {`class kept { static of() { return "row 1"; } }`,
			`typeof kept === "undefined" ? undefined : kept.of()`},
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

// Freezing the built-ins must not break shadowing, which scripts rely on: a class naming
// its errors, an object with its own toString. The built-in itself stays read-only.
func TestRunner_ShadowingBuiltInProperties(t *testing.T) {
	runner := isolationRunner(t)
	result := run(t, runner, `
		class RuleError extends Error { constructor(m) { super(m); this.name = "RuleError"; } }
		const e = new RuleError("refusée");
		const plain = new Error("x"); plain.name = "Renamed";
		const o = {}; o.toString = () => "mine"; o.constructor = "c"; o.valueOf = () => 7;
		const out = Object.create(null); out["constructor"] = "col"; out["__proto__"] = "proto";
		[e.name, plain.name, String(o), o.constructor, o + 1, out["constructor"], out["__proto__"], String(e)].join("|");`)
	require.Equal(t, "RuleError|Renamed|mine|c|8|col|proto|RuleError: refusée", result.String())

	_, err := runner.Run(context.Background(), goja.MustCompile("test.js", `
		"use strict"; Object.prototype.toString = () => "leak";`, false))
	require.ErrorContains(t, err, "read only property 'toString' of a built-in object")
	require.Equal(t, "[object Object]", run(t, runner, `String({})`).String(), "the built-in is untouched")
	require.Equal(t, "hello", run(t, runner, `legacy.hello()`).String())
}

// A run declaring at its top level runs again, and again: the block Compile wraps it in
// ends with the run, so the second declaration is not a redeclaration.
func TestRunner_TopLevelDeclarationsRunAgain(t *testing.T) {
	runner := isolationRunner(t)
	for _, row := range []string{"row 1", "row 2", "row 3"} {
		result := run(t, runner, `
			const kept = "`+row+`";
			let seen = kept;
			class Named { value() { return seen; } }
			new Named().value();`)
		require.Equal(t, row, result.String())
	}
}

// Compile keeps what the source says: its value, its strict mode, and the lines a stack
// trace reports.
func TestCompile_KeepsProgramSemantics(t *testing.T) {
	runner := isolationRunner(t)

	require.Equal(t, "the value", run(t, runner, `"the value"`).String(),
		"the program still evaluates to its last statement")

	strict := run(t, runner, `"use strict";
		try { undeclared = 1; "assigned" } catch (e) { e.constructor.name }`)
	require.Equal(t, "ReferenceError", strict.String(), "the directive prologue still applies")

	sloppy := run(t, runner, `try { undeclared = 1; "assigned" } catch (e) { "threw" }`)
	require.Equal(t, "assigned", sloppy.String(), "code without a directive stays sloppy")

	program, err := Compile("test.js", "\n\nthrow new Error('boom');")
	require.NoError(t, err)
	_, err = runner.Run(context.Background(), program)
	require.ErrorContains(t, err, "test.js:3:", "the line of the source is the line reported")
}
