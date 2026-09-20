package pseudo_functions

import (
	"context"
	"fmt"
	"testing"

	"github.com/dop251/goja"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/stretchr/testify/require"
)

// fakeSource derives from a fixed key; Fake tags the value with its kind.
type fakeSource struct{ deriver *consistency.Deriver }

func (f fakeSource) Fake(kind string, value any) (any, error) {
	return fmt.Sprintf("%s(%v)", kind, value), nil
}

func (f fakeSource) Seed(domain string, value any) consistency.Seed {
	return f.deriver.Domain("user:" + domain).WithCanonicalizer(consistency.Exact).Seed(fmt.Sprint(value))
}

func runPseudo(t *testing.T, ctx context.Context, code string) (goja.Value, error) {
	t.Helper()
	runner, err := javascript_vm.NewRunner(javascript_vm.WithFunctions(Get()...))
	require.NoError(t, err)
	return runner.Run(ctx, goja.MustCompile("test.js", code, false))
}

func TestFunctions(t *testing.T) {
	ctx := ContextWithSource(context.Background(), fakeSource{deriver: consistency.New([]byte("k"), "run:1")})

	for _, kind := range Kinds {
		result, err := runPseudo(t, ctx, fmt.Sprintf(`pseudo.%s("Durand")`, kind))
		require.NoError(t, err, kind)
		require.Equal(t, kind+"(Durand)", result.String())
	}

	same, err := runPseudo(t, ctx, `pseudo.hash("Durand", "client") === pseudo.hash("Durand", "client")`)
	require.NoError(t, err)
	require.True(t, same.ToBoolean(), "the same value hashes the same")
	other, err := runPseudo(t, ctx, `pseudo.hash("Durand", "client") !== pseudo.hash("Durand", "fournisseur")`)
	require.NoError(t, err)
	require.True(t, other.ToBoolean(), "two domains hash apart")
	exact, err := runPseudo(t, ctx, `pseudo.hash("Durand", "client") !== pseudo.hash("durand", "client")`)
	require.NoError(t, err)
	require.True(t, exact.ToBoolean(), "a rule's domain compares values exactly")

	n, err := runPseudo(t, ctx, `pseudo.int(42, "age", 18, 20)`)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n.ToInteger(), int64(18))
	require.LessOrEqual(t, n.ToInteger(), int64(20))

	picked, err := runPseudo(t, ctx, `pseudo.pick(["A", "B", "C"], "Durand", "segment")`)
	require.NoError(t, err)
	require.Contains(t, []string{"A", "B", "C"}, picked.String())

	for _, code := range []string{`pseudo.lastName(null)`, `pseudo.hash(null, "d")`, `pseudo.int(null, "d", 1, 2)`, `pseudo.pick([1], null, "d")`} {
		result, err := runPseudo(t, ctx, code)
		require.NoError(t, err, code)
		require.True(t, goja.IsNull(result), "%s keeps NULL", code)
	}

	for code, message := range map[string]string{
		`pseudo.hash("x", "")`:       "the domain must not be empty",
		`pseudo.int("x", "d", 5, 1)`: "min 5 is above max 1",
		`pseudo.pick([], "x", "d")`:  "the list must not be empty",
		`pseudo.pick(["a"], "x")`:    "expects (list, value, domain)",
		`pseudo.hash("x")`:           "expects (value, domain)",
		`pseudo.int("x", "d", 1)`:    "expects (value, domain, min, max)",
	} {
		_, err := runPseudo(t, ctx, code)
		require.ErrorContains(t, err, message, code)
	}
}

// Without a consistency scope — under Benthos — a call fails and says why.
func TestFunctions_WithoutSource(t *testing.T) {
	_, err := runPseudo(t, context.Background(), `pseudo.lastName("Durand")`)
	require.ErrorContains(t, err, "pseudo.lastName is only available when Athanor runs the job")
}
