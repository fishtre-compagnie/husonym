package v1alpha1_transformersservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/dop251/goja"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	javascript_userland "github.com/fishtre-compagnie/husonym/internal/javascript/userland"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
)

// tryTimeLimit is what a trial of rules may take, all rows together: it runs in the API.
const tryTimeLimit = 10 * time.Second

// ValidateUserJavascriptCode compiles the code the way a run does, and reports where it
// writes state outside its own variables: that state lives for one row.
func (s *Service) ValidateUserJavascriptCode(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ValidateUserJavascriptCodeRequest],
) (*connect.Response[mgmtv1alpha1.ValidateUserJavascriptCodeResponse], error) {
	code := req.Msg.GetCode()
	if _, err := goja.Compile("rule.js", javascript_userland.GetTransformJavascriptFunction(code, "rule", true), false); err != nil {
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
	if err := user.EnforceJob(ctx, userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()), rbac.JobAction_View); err != nil {
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

	ctx, cancel := context.WithTimeout(ctx, tryTimeLimit)
	defer cancel()
	out, failure, err := runner.TryJavascriptRules(ctx, rules, rows)
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
		bits, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		resp.Rows = append(resp.Rows, string(bits))
	}
	return connect.NewResponse(resp), nil
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
