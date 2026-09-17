package runner

// javascript.go — runs the JavaScript transformers of a table the way Benthos does.
//
// Each JavaScript transformer could be run alone, one VM per column, but existing
// transformers rely on what a single VM gives them: `input`, the whole row, and state
// kept on a global object from one column to the next (a column picks a full identity,
// the following ones read it back). In separate VMs that state is always empty and such
// a transformer silently returns the original value — real personal data. So every
// JavaScript transformer of a table runs in one program, in mapping order, once per
// row, after the value transformers, from code assembled exactly like the Benthos
// processor's.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dop251/goja"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/javascript"
	javascript_userland "github.com/fishtre-compagnie/husonym/internal/javascript/userland"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/redpanda-data/benthos/v4/public/service"
)

// TransformEnv is what the transformers of a table can reach at run time.
type TransformEnv struct {
	// Resolver resolves user-defined transformers to their configuration; nil fails
	// on any user-defined transformer.
	Resolver te.UserDefinedTransformerResolver
	// PiiTextApi backs TransformPiiText and the PII functions exposed to JavaScript;
	// nil disables them.
	PiiTextApi transformers.TransformPiiTextApi
	Logger     *slog.Logger
	// ExecOptions configure the transformers run through the Benthos executor.
	ExecOptions []te.TransformerExecutorOption
}

// javascriptColumn is one column computed by a JavaScript transformer.
type javascriptColumn struct {
	column   string
	code     string
	generate bool // generate: no input value; transform: value and whole row
}

// javascriptColumnOf returns the JavaScript column described by a transformer config,
// and false for any other transformer.
func javascriptColumnOf(column string, cfg *mgmtv1alpha1.TransformerConfig) (javascriptColumn, bool) {
	switch {
	case cfg.GetTransformJavascriptConfig() != nil:
		return javascriptColumn{column: column, code: cfg.GetTransformJavascriptConfig().GetCode()}, true
	case cfg.GetGenerateJavascriptConfig() != nil:
		return javascriptColumn{column: column, code: cfg.GetGenerateJavascriptConfig().GetCode(), generate: true}, true
	default:
		return javascriptColumn{}, false
	}
}

// javascriptRows is the row transformer running every JavaScript column of a table.
type javascriptRows struct {
	reads    []string
	writes   []string
	runner   *javascript_vm.Runner
	valueApi *te.AnonValueApi
	program  *goja.Program
}

func newJavascriptRows(tableColumns []string, columns []javascriptColumn, env *TransformEnv) (*javascriptRows, error) {
	var functions, setters []string
	writes := make([]string, 0, len(columns))
	for _, c := range columns {
		if c.generate {
			functions = append(functions, javascript_userland.GetGenerateJavascriptFunction(c.code, c.column))
			setters = append(setters, javascript_userland.BuildOutputSetter(c.column, false, false))
		} else {
			functions = append(functions, javascript_userland.GetTransformJavascriptFunction(c.code, c.column, true))
			setters = append(setters, javascript_userland.BuildOutputSetter(c.column, true, true))
		}
		writes = append(writes, c.column)
	}

	program, err := goja.Compile("main.js", javascript_userland.GetFunction(functions, setters), false)
	if err != nil {
		return nil, fmt.Errorf("runner: compilation du JavaScript de la table: %w", err)
	}
	logger := env.Logger
	if logger == nil {
		logger = slog.Default()
	}
	valueApi := te.NewAnonValueApi()
	vm, err := javascript.NewDefaultValueRunner(valueApi, env.PiiTextApi, logger)
	if err != nil {
		return nil, fmt.Errorf("runner: création de la VM JavaScript: %w", err)
	}
	return &javascriptRows{reads: tableColumns, writes: writes, runner: vm, valueApi: valueApi, program: program}, nil
}

func (j *javascriptRows) Reads() []string  { return j.reads }
func (j *javascriptRows) Writes() []string { return j.writes }

// TransformRow runs the table program on one row and writes back the JavaScript
// columns only: the other columns keep their Go values untouched.
func (j *javascriptRows) TransformRow(ctx transform.Ctx, row transform.Row) error {
	input := make(map[string]any, len(j.reads))
	for _, col := range j.reads {
		if v, ok := row.Get(col); ok {
			input[col] = v
		}
	}
	msg := service.NewMessage(nil)
	msg.SetStructured(input)
	j.valueApi.SetMessage(msg)
	defer j.valueApi.SetMessage(nil)

	runCtx := ctx.Context
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, err := j.runner.Run(runCtx, j.program); err != nil {
		return fmt.Errorf("runner: exécution du JavaScript: %w", err)
	}

	out, err := j.valueApi.Message().AsStructured()
	if err != nil {
		return fmt.Errorf("runner: lecture du résultat JavaScript: %w", err)
	}
	outMap, ok := out.(map[string]any)
	if !ok {
		return fmt.Errorf("runner: résultat JavaScript inattendu (%T)", out)
	}
	for _, col := range j.writes {
		if err := row.Set(col, outMap[col]); err != nil {
			return err
		}
	}
	return nil
}

var _ transform.RowTransformer = (*javascriptRows)(nil)
