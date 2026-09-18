package javascript_vm

import (
	"fmt"

	"github.com/dop251/goja"
)

// A run transforms one row, or one value, and keeps nothing for the next one. A runner is
// reused from run to run — per page, from a pool, per request — so state left by a run
// would reach whichever row comes next, at random: the personal data of one row written
// into another. Within a run the state is free: the columns of a row share it.
//
// A new VM per run would cost far more than the run itself, and so would looking, after
// each run, for what it added. Instead the runner is sealed once built, and each run gets
// a global object of its own:
//
//   - sealing freezes the built-in objects (Object.prototype, Array.prototype, Math,
//     JSON…), the functions the runner offers, and the global object itself: nothing a run
//     writes there can stay;
//   - each run gets a new, empty global object inheriting the sealed one, holding its own
//     globalThis and, for each namespace of functions (husonym and its alias neosync,
//     benthos), a new empty object inheriting the frozen functions. Whatever the run
//     writes — `x = …` without a declaration, `globalThis.x = …`, `neosync.x = …`, a
//     defined property, a prototype — lands there, and is dropped with it.

// namespace is a global object holding the functions of the runner.
type namespace struct {
	names     []string // the global and its aliases
	functions *goja.Object
}

// hardenScript freezes every object reachable from the global object, the global object
// included, prototypes and the functions of the namespaces too.
const hardenScript = `(function () {
	const seen = new Set();
	const freeze = (value) => {
		if ((typeof value !== "object" && typeof value !== "function") || value === null || seen.has(value)) {
			return;
		}
		seen.add(value);
		Object.freeze(value);
		freeze(Object.getPrototypeOf(value));
		for (const key of Reflect.ownKeys(value)) {
			const d = Object.getOwnPropertyDescriptor(value, key);
			freeze(d.value); freeze(d.get); freeze(d.set);
		}
	};
	freeze(globalThis);
})`

// seal freezes the runner in the state every run starts from.
func (r *Runner) seal() error {
	global := r.vm.GlobalObject()
	byTarget := map[string]*namespace{}
	for _, fn := range r.options.functions {
		name := fn.Namespace()
		if byTarget[name] == nil {
			ns := &namespace{names: []string{name}, functions: global.Get(name).ToObject(r.vm)}
			byTarget[name] = ns
			r.namespaces = append(r.namespaces, ns)
		}
	}
	for alias, target := range r.options.globalAliases {
		if ns := byTarget[target]; ns != nil {
			ns.names = append(ns.names, alias)
		}
	}

	harden, err := r.vm.RunString(hardenScript)
	if err != nil {
		return fmt.Errorf("javascript: sealing the runner: %w", err)
	}
	fn, ok := goja.AssertFunction(harden)
	if !ok {
		return fmt.Errorf("javascript: sealing the runner: the harden script is not a function")
	}
	if _, err := fn(goja.Undefined()); err != nil {
		return fmt.Errorf("javascript: sealing the runner: %w", err)
	}
	r.sealedGlobal = global
	return nil
}

// resetState gives the next run a global object of its own.
func (r *Runner) resetState() error {
	global := r.vm.CreateObject(r.sealedGlobal)
	if err := global.DefineDataProperty("globalThis", global, goja.FLAG_TRUE, goja.FLAG_TRUE, goja.FLAG_FALSE); err != nil {
		return fmt.Errorf("javascript: setting globalThis: %w", err)
	}
	for _, ns := range r.namespaces {
		fresh := r.vm.CreateObject(ns.functions)
		for _, name := range ns.names {
			if err := global.DefineDataProperty(name, fresh, goja.FLAG_TRUE, goja.FLAG_TRUE, goja.FLAG_TRUE); err != nil {
				return fmt.Errorf("javascript: setting %s: %w", name, err)
			}
		}
	}
	r.vm.SetGlobalObject(global)
	return nil
}
