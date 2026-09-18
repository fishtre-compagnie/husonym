package javascript_userland

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
)

// Analysis is what reading a rule, without running it, tells about it.
type Analysis struct {
	// GlobalWrites are the places the rule writes state outside its own variables: a
	// variable assigned without a declaration, a property of neosync, husonym, globalThis
	// or this. That state lives for one row: it is shared with the other columns of the
	// row, and gone at the next one.
	GlobalWrites []string
	// UsesPseudo tells whether the rule reaches the deterministic functions (pseudo.*),
	// which only Athanor offers.
	UsesPseudo bool
}

// globalObjects are the globals a rule writes state on through their properties.
var globalObjects = []string{"neosync", "husonym", "globalThis"}

// Analyze reads the code of a rule, the body of a function receiving value and input as
// GetTransformJavascriptFunction assembles it (a generate rule simply ignores them).
func Analyze(code string) (*Analysis, error) {
	program, err := parser.ParseFile(nil, "rule.js", GetTransformJavascriptFunction(code, "rule", true), 0)
	if err != nil {
		return nil, fmt.Errorf("the code does not compile: %w", err)
	}
	w := &walker{declared: map[string]bool{"value": true, "input": true}}
	w.walk(reflect.ValueOf(program))

	analysis := &Analysis{UsesPseudo: w.referenced["pseudo"] && !w.declared["pseudo"]}
	for _, name := range w.assigned {
		if !w.declared[name] && !slices.Contains(analysis.GlobalWrites, name) {
			analysis.GlobalWrites = append(analysis.GlobalWrites, name)
		}
	}
	for _, write := range w.memberWrites {
		if object := write[:strings.IndexAny(write, ".[")]; (object == "this" || !w.declared[object]) &&
			!slices.Contains(analysis.GlobalWrites, write) {
			analysis.GlobalWrites = append(analysis.GlobalWrites, write)
		}
	}
	return analysis, nil
}

// walker collects, over the whole rule, the names it declares, reads and assigns. A name
// declared anywhere counts as declared everywhere: the analysis may miss a global write
// hidden behind a name declared elsewhere, never report a local one.
type walker struct {
	declared     map[string]bool
	referenced   map[string]bool
	assigned     []string
	memberWrites []string // "object.property"
}

var (
	identifierType = reflect.TypeFor[*ast.Identifier]()
	nodeType       = reflect.TypeFor[ast.Node]()
)

func (w *walker) walk(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
	}
	if v.Type().Implements(nodeType) || v.Type() == identifierType {
		if w.visit(v.Interface()) {
			return
		}
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		w.walk(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				w.walk(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			w.walk(v.Index(i))
		}
	}
}

// visit handles the nodes that declare, read or assign a name. It returns true when it
// walked the children itself.
func (w *walker) visit(node any) bool {
	switch n := node.(type) {
	case *ast.Identifier:
		w.reference(n.Name.String())
	case *ast.DotExpression:
		// The property is a name, not a variable: only the object is read.
		w.walk(reflect.ValueOf(n.Left))
		return true
	case *ast.PropertyKeyed:
		if n.Computed {
			w.walk(reflect.ValueOf(n.Key))
		}
		w.walk(reflect.ValueOf(n.Value))
		return true
	case *ast.Binding:
		w.declareTarget(n.Target)
		w.walk(reflect.ValueOf(n.Initializer))
		return true
	case *ast.FunctionLiteral:
		if n.Name != nil {
			w.declared[n.Name.Name.String()] = true
		}
	case *ast.ClassLiteral:
		if n.Name != nil {
			w.declared[n.Name.Name.String()] = true
		}
	case *ast.CatchStatement:
		w.declareTarget(n.Parameter)
	case *ast.AssignExpression:
		w.assign(n.Left)
	case *ast.UnaryExpression:
		if n.Operator == token.INCREMENT || n.Operator == token.DECREMENT {
			w.assign(n.Operand)
		}
	case *ast.ForDeclaration:
		w.declareTarget(n.Target)
	case *ast.ForIntoExpression:
		w.assign(n.Expression)
	}
	return false
}

func (w *walker) reference(name string) {
	if w.referenced == nil {
		w.referenced = map[string]bool{}
	}
	w.referenced[name] = true
}

// assign records what an expression written to designates.
func (w *walker) assign(target ast.Expression) {
	switch t := target.(type) {
	case *ast.Identifier:
		w.assigned = append(w.assigned, t.Name.String())
	case *ast.DotExpression:
		if object := globalObject(t.Left); object != "" {
			w.memberWrites = append(w.memberWrites, object+"."+t.Identifier.Name.String())
		}
	case *ast.BracketExpression:
		if object := globalObject(t.Left); object != "" {
			w.memberWrites = append(w.memberWrites, object+"[…]")
		}
	}
}

// globalObject names the global an expression designates, or "".
func globalObject(e ast.Expression) string {
	switch o := e.(type) {
	case *ast.ThisExpression:
		return "this"
	case *ast.Identifier:
		if name := o.Name.String(); slices.Contains(globalObjects, name) {
			return name
		}
	}
	return ""
}

// declareTarget declares the names a binding target introduces, patterns included.
func (w *walker) declareTarget(target ast.Node) {
	switch t := target.(type) {
	case nil:
	case *ast.Identifier:
		w.declared[t.Name.String()] = true
	default:
		w.collectNames(reflect.ValueOf(t))
	}
}

// collectNames declares every identifier of a destructuring pattern. A name read in a
// default value is declared with them: it can only hide a warning, never raise one.
func (w *walker) collectNames(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
		if id, ok := v.Interface().(*ast.Identifier); ok {
			w.declared[id.Name.String()] = true
			return
		}
		w.collectNames(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				w.collectNames(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			w.collectNames(v.Index(i))
		}
	}
}
