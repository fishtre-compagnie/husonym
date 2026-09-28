package apikey

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

const (
	module     = "github.com/fishtre-compagnie/husonym"
	messages   = module + "/backend/gen/go/protos/mgmt/v1alpha1"
	workerMain = module + "/worker/cmd/worker"
)

// Every request the worker's code builds for the API is one a worker key may send: a procedure
// missing from WorkerProcedures fails the run of a worker that authenticates with a worker key,
// and only there. The requests are read from the source of every package the worker binary is
// built from; a request is named after its procedure (GetJobRequest for .../GetJob).
func Test_WorkerProcedures_CoverWhatTheWorkerCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the packages of the worker binary")
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
		Dir:  "../..",
	}, workerMain)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	called := map[string][]string{}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		if seen[pkg.PkgPath] || !strings.HasPrefix(pkg.PkgPath, module+"/") ||
			strings.HasPrefix(pkg.PkgPath, module+"/backend/gen/") {
			return
		}
		seen[pkg.PkgPath] = true
		for _, path := range pkg.GoFiles {
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			require.NoError(t, err)
			for _, method := range requestsBuilt(file) {
				called[method] = append(called[method], path)
			}
		}
	})
	require.NotEmpty(t, called, "no request found: the walk is broken, and the test would pass forever")

	for method, files := range called {
		allowed := slices.ContainsFunc(WorkerProcedures, func(p string) bool { return strings.HasSuffix(p, "/"+method) })
		require.Truef(t, allowed, "the worker calls %s (%s), which a worker key does not open: add it to WorkerProcedures",
			method, strings.Join(files, ", "))
	}
}

// requestsBuilt names the procedures whose request a file builds, as mgmtv1alpha1.XRequest{...}
// under whatever name the file imports the messages.
func requestsBuilt(file *ast.File) []string {
	name := ""
	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path == messages {
			name = "mgmtv1alpha1"
			if spec.Name != nil {
				name = spec.Name.Name
			}
		}
	}
	if name == "" {
		return nil
	}
	var methods []string
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == name && strings.HasSuffix(sel.Sel.Name, "Request") {
			methods = append(methods, strings.TrimSuffix(sel.Sel.Name, "Request"))
		}
		return true
	})
	return methods
}
