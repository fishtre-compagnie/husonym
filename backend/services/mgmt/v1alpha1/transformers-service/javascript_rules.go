package v1alpha1_transformersservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/metrics"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	javascript_userland "github.com/fishtre-compagnie/husonym/internal/javascript/userland"
	javascript_vm "github.com/fishtre-compagnie/husonym/internal/javascript/vm"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
)

// A trial runs a user's code in the API process: it is bounded in time, in memory, and
// runs alone.
const (
	// tryTimeLimit is what a trial may take, all rows together.
	tryTimeLimit = 10 * time.Second
	// tryMemoryLimit is how much the heap may grow while a trial runs. goja bounds no
	// allocation: a rule filling an array in a loop would take the process down.
	tryMemoryLimit = 256 << 20
)

// trialSlot lets one trial run at a time, so that their allocations never add up.
var trialSlot = make(chan struct{}, 1)

// errTrialMemory stops a trial whose heap grew past tryMemoryLimit.
var errTrialMemory = fmt.Errorf("the rules took more than %d MiB during the trial", tryMemoryLimit>>20)

// ValidateUserJavascriptCode compiles the code the way a run does, and reports where it
// writes state outside its own variables: that state lives for one row.
func (s *Service) ValidateUserJavascriptCode(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ValidateUserJavascriptCodeRequest],
) (*connect.Response[mgmtv1alpha1.ValidateUserJavascriptCodeResponse], error) {
	code := req.Msg.GetCode()
	// The same compilation the run does, so that what validates here runs there.
	if _, err := javascript_vm.Compile("rule.js", javascript_userland.GetTransformJavascriptFunction(code, "rule", true)); err != nil {
		return connect.NewResponse(&mgmtv1alpha1.ValidateUserJavascriptCodeResponse{Valid: false}), nil
	}
	analysis, err := javascript_userland.Analyze(code)
	if err != nil {
		return connect.NewResponse(&mgmtv1alpha1.ValidateUserJavascriptCodeResponse{Valid: false}), nil
	}
	return connect.NewResponse(&mgmtv1alpha1.ValidateUserJavascriptCodeResponse{
		Valid:        true,
		GlobalWrites: analysis.GlobalWrites,
	}), nil
}

// TryJavascriptRules runs JavaScript rules on the rows the user typed, the way Athanor
// runs them in a job, with a key drawn for the call.
func (s *Service) TryJavascriptRules(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest],
) (*connect.Response[mgmtv1alpha1.TryJavascriptRulesResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	// Trying a rule is part of writing one: the right to create transformers.
	if err := user.EnforceJob(ctx, userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()), rbac.JobAction_Edit); err != nil {
		return nil, err
	}

	rules := make([]runner.JavascriptRule, 0, len(req.Msg.GetRules()))
	for _, rule := range req.Msg.GetRules() {
		rules = append(rules, runner.JavascriptRule{Column: rule.GetColumn(), Config: rule.GetTransformer()})
	}
	rows := make([]map[string]any, 0, len(req.Msg.GetRows()))
	for i, raw := range req.Msg.GetRows() {
		row, err := parseRow(raw)
		if err != nil {
			return nil, husonymerrors.NewBadRequest(fmt.Sprintf("row %d: %s", i, err))
		}
		rows = append(rows, row)
	}

	out, failure, err := runTrial(ctx, rules, rows, tryMemoryLimit)
	if errors.Is(err, runner.ErrNotARule) {
		return nil, husonymerrors.NewBadRequest(err.Error())
	}
	if err != nil {
		return nil, err
	}
	if failure != nil {
		return connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{
			Failure: &mgmtv1alpha1.JavascriptRuleFailure{
				Row:     uint32(failure.Row), //nolint:gosec // an index among at most 20 rows
				Column:  failure.Column,
				Message: failure.Message,
			},
		}), nil
	}
	resp := &mgmtv1alpha1.TryJavascriptRulesResponse{Rows: make([]string, 0, len(out))}
	for i, row := range out {
		bits, err := json.Marshal(displayable(row))
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		resp.Rows = append(resp.Rows, string(bits))
	}
	return connect.NewResponse(resp), nil
}

// runTrial runs the rules alone, within tryTimeLimit, and stops them when the heap grows
// by more than memoryLimit.
func runTrial(
	ctx context.Context,
	rules []runner.JavascriptRule,
	rows []map[string]any,
	memoryLimit uint64,
) ([]map[string]any, *runner.RuleFailure, error) {
	select {
	case trialSlot <- struct{}{}:
		defer func() { <-trialSlot }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	ctx, cancelTime := context.WithTimeout(ctx, tryTimeLimit)
	defer cancelTime()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go watchHeap(ctx, cancel, memoryLimit)
	return runner.TryJavascriptRules(ctx, rules, rows)
}

// watchHeap cancels ctx with errTrialMemory once the heap has grown by more than limit.
// The heap is the process's: the limit is loose, and meant to stop a runaway rule, not to
// measure one.
func watchHeap(ctx context.Context, cancel context.CancelCauseFunc, limit uint64) {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	start := sample[0].Value.Uint64()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics.Read(sample)
			if heap := sample[0].Value.Uint64(); heap > start && heap-start > limit {
				cancel(errTrialMemory)
				return
			}
		}
	}
}

// displayable replaces what JSON cannot hold — NaN, infinities — by its JavaScript
// spelling: the trial shows what the rule returned.
func displayable(value any) any {
	switch v := value.(type) {
	case float64:
		switch {
		case math.IsNaN(v):
			return "NaN"
		case math.IsInf(v, 1):
			return "Infinity"
		case math.IsInf(v, -1):
			return "-Infinity"
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = displayable(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = displayable(e)
		}
		return out
	default:
		return v
	}
}

// parseRow reads a row typed as a JSON object. An integer stays an integer, exact
// whatever its size, as a database would hand it over.
func parseRow(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if row == nil {
		return nil, errors.New("not a JSON object")
	}
	for column, value := range row {
		row[column] = fromJSON(value)
	}
	return row, nil
}

func fromJSON(value any) any {
	switch v := value.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]any:
		for k, e := range v {
			v[k] = fromJSON(e)
		}
		return v
	case []any:
		for i, e := range v {
			v[i] = fromJSON(e)
		}
		return v
	default:
		return v
	}
}
