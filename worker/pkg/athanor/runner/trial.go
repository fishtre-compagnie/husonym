package runner

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sort"

	"github.com/dop251/goja"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	javascript_userland "github.com/fishtre-compagnie/husonym/internal/javascript/userland"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// JavascriptRule is a JavaScript transformer of a column, as a job maps it.
type JavascriptRule struct {
	Column string
	Config *mgmtv1alpha1.TransformerConfig
}

// RuleFailure is the first failure of a trial.
type RuleFailure struct {
	// Row is the index of the row the rule failed on.
	Row int
	// Column is the column whose rule failed, "" when the failure comes from none.
	Column string
	// Message is the error of the script, or of the engine.
	Message string
}

// ErrNotARule is returned for a rule that is not a JavaScript transformer.
var ErrNotARule = errors.New("a rule is a TransformJavascript or GenerateJavascript transformer")

// TryJavascriptRules runs the JavaScript rules of one table on rows, the way a run of
// Athanor does: in mapping order, sharing their state for the row and never with the
// next one, with the same guards. The pseudo functions derive from a key drawn for the
// call, so their outputs have the shape of a run's, never its values: no key of a run
// is needed, nor revealed.
//
// It returns the rows once transformed, or the first failure.
func TryJavascriptRules(
	ctx context.Context,
	rules []JavascriptRule,
	rows []map[string]any,
) ([]map[string]any, *RuleFailure, error) {
	ruleColumns := make([]string, 0, len(rules))
	isRule := map[string]bool{}
	for _, rule := range rules {
		if _, ok := javascriptColumnOf(rule.Column, rule.Config); !ok {
			return nil, nil, fmt.Errorf("%w: column %q", ErrNotARule, rule.Column)
		}
		if isRule[rule.Column] {
			return nil, nil, fmt.Errorf("two rules write the column %q", rule.Column)
		}
		isRule[rule.Column] = true
		ruleColumns = append(ruleColumns, rule.Column)
	}

	// The columns of the rows the rules do not write are read as they are.
	var others []string
	seen := map[string]bool{}
	for _, row := range rows {
		for column := range row {
			if !isRule[column] && !seen[column] {
				seen[column] = true
				others = append(others, column)
			}
		}
	}
	sort.Strings(others)

	const schema, table = "trial", "rules"
	mappings := make([]*mgmtv1alpha1.JobMapping, 0, len(others)+len(rules))
	for _, column := range others {
		mappings = append(mappings, &mgmtv1alpha1.JobMapping{Schema: schema, Table: table, Column: column, Transformer: passthrough()})
	}
	for _, rule := range rules {
		mappings = append(mappings, &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: rule.Column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: rule.Config},
		})
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, fmt.Errorf("drawing the key of the trial: %w", err)
	}
	cols, spec, err := SpecForTable(ctx, mappings, schema, table, consistency.New(key, "run:trial"), &TransformEnv{})
	if err != nil {
		return nil, nil, err
	}
	plan, err := engine.Compile(cols, spec)
	if err != nil {
		return nil, nil, err
	}

	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		batch := engine.NewBatch(cols, 1)
		for _, column := range cols {
			batch.Cols[column][0] = row[column]
		}
		if err := plan.Execute(transform.Ctx{Context: ctx}, batch); err != nil {
			return nil, &RuleFailure{
				Row:     i,
				Column:  javascript_userland.FailedColumn(err, ruleColumns),
				Message: scriptMessage(err),
			}, nil
		}
		out[i] = make(map[string]any, len(cols))
		for _, column := range cols {
			out[i][column] = batch.Cols[column][0]
		}
	}
	return out, nil, nil
}

// scriptMessage returns the error a script raised, without the layers of the engine
// around it.
func scriptMessage(err error) string {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return interrupted.Error()
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return exception.Error()
	}
	return err.Error()
}

func passthrough() *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}}
}
