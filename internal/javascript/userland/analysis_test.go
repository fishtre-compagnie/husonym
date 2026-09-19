package javascript_userland

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyze(t *testing.T) {
	tests := map[string]struct {
		code         string
		globalWrites []string
		usesPseudo   bool
	}{
		"local variables": {code: `
			var a = value; let b = 1; const c = 2;
			function helper(x) { var y = x; y++; return y; }
			for (let i = 0; i < 3; i++) { b += i; }
			for (const k of [1, 2]) { b += k; }
			for (var key in input) { a = key; }
			try { a = b; } catch (e) { a = e; }
			const {d, e: f, ...rest} = input; const [g, h = 3] = [1];
			(z => { z = 1; })(0);
			return a + b + c + d + f + g + h + rest + helper(1);`},
		"undeclared variable": {code: `picked = value; return picked;`, globalWrites: []string{"picked"}},
		"increment":           {code: `count++; return value;`, globalWrites: []string{"count"}},
		"negation is a read":  {code: `var n = 1; return -n;`},
		"loop variable":       {code: `for (k in input) {} return value;`, globalWrites: []string{"k"}},
		"namespaces": {code: `neosync.nom = value; husonym["x"] = 1; globalThis.y = 2; this.z = 3; return value;`,
			globalWrites: []string{"neosync.nom", "husonym[…]", "globalThis.y", "this.z"}},
		"shadowed namespace":         {code: `var neosync = {}; neosync.nom = value; return value;`},
		"pseudo":                     {code: `return pseudo.lastName(value);`, usesPseudo: true},
		"pseudo aliased":             {code: `const p = pseudo; return p.lastName(value);`, usesPseudo: true},
		"pseudo as property":         {code: `return input.pseudo + {pseudo: 1}.pseudo;`},
		"pseudo declared":            {code: `var pseudo = {lastName: v => v}; return pseudo.lastName(value);`},
		"pseudo shorthand":           {code: `const o = {pseudo}; return o.pseudo.lastName(value);`, usesPseudo: true},
		"pseudo on global":           {code: `return globalThis.pseudo.lastName(value);`, usesPseudo: true},
		"pseudo by name":             {code: `return globalThis["pseudo"].lastName(value);`, usesPseudo: true},
		"destructured then assigned": {code: `let {nom, ville: v} = input; nom = nom.trim(); v = v + "!"; return nom + v;`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			analysis, err := Analyze(tt.code)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.globalWrites, analysis.GlobalWrites)
			require.Equal(t, tt.usesPseudo, analysis.UsesPseudo)
		})
	}

	_, err := Analyze(`return (;`)
	require.ErrorContains(t, err, "does not compile")
}
