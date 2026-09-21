package v1alpha1_connectiondataservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	jsonanonymizer "github.com/fishtre-compagnie/husonym/internal/json-anonymizer"
)

const (
	defaultPreviewLimit = 20
	// Lower than the plain sample: every value goes through the transformer.
	maxPreviewLimit = 50
)

// PreviewColumnTransformer shows what a transformer would make of a column's values, before
// anybody maps the column with it or accepts that it stays as it is.
//
// The server reads the values itself. Taking them from the caller would make this an unmetered
// copy of AnonymizeMany — which is licensed, refused to personal accounts and counted against the
// account's record quota — able to anonymize anything, twenty-five values at a time. Reading a
// column of a connection the caller can already open, capped like the plain sample, keeps it what
// it is for: a look at a real column.
//
// It runs the transformer the way it would run for real: a javascript rule through the rule
// trial, which uses the engine's runner, and any other transformer through the anonymizer
// AnonymizeMany uses, Presidio-backed ones included. A user-defined transformer is resolved first,
// so it takes the path of what it actually is.
func (s *Service) PreviewColumnTransformer(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.PreviewColumnTransformerRequest],
) (*connect.Response[mgmtv1alpha1.PreviewColumnTransformerResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	sampled, err := s.sampleRows(
		ctx,
		req.Msg.GetConnectionId(),
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		clampLimit(req.Msg.GetLimit(), defaultPreviewLimit, maxPreviewLimit),
	)
	if err != nil {
		return nil, err
	}
	raws, err := columnValues(sampled.rows, req.Msg.GetSchema(), req.Msg.GetTable(), req.Msg.GetColumn())
	if err != nil {
		return nil, err
	}

	config, err := s.resolveTransformer(ctx, req.Msg.GetTransformer())
	if err != nil {
		return nil, err
	}
	// A javascript rule goes through the trial a rule's author uses, which runs it with the
	// engine's own runner. The anonymizer below would hand it Benthos' structured values, where
	// a number arrives as a string-like object: `value + 1` would read "281" here and 29 in the
	// run, and a preview that shows something else than the run is worse than none.
	if isJavascriptRule(config) {
		return connect.NewResponse(s.previewJavascript(ctx, sampled, req.Msg.GetColumn(), raws, config)), nil
	}

	anonymizer, err := jsonanonymizer.NewAnonymizer(
		jsonanonymizer.WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression:  ".value",
			Transformer: config,
		}}),
		jsonanonymizer.WithConditionalAnonymizeConfig(
			s.transformers.IsPresidioEnabled,
			s.transformers.Analyze,
			s.transformers.Anonymize,
			s.cfg.PresidioDefaultLanguage,
		),
		jsonanonymizer.WithTransformerClient(s.transformers.Client),
		jsonanonymizer.WithLogger(logger),
	)
	if err != nil {
		// The transformer itself cannot be built — a bad configuration, not a bad value.
		return nil, connect.NewError(
			connect.CodeInvalidArgument,
			fmt.Errorf("unable to build the transformer: %w", err),
		)
	}

	return connect.NewResponse(previewValues(raws, func(raw any) (any, error) {
		return transformValue(anonymizer, raw)
	})), nil
}

// transformValue runs one value through the anonymizer, wrapped the way the anonymizer takes it.
func transformValue(anonymizer *jsonanonymizer.JsonAnonymizer, raw any) (any, error) {
	in, err := json.Marshal(map[string]any{"value": raw})
	if err != nil {
		return nil, fmt.Errorf("unable to hand the value to the transformer: %w", err)
	}
	out, err := anonymizer.AnonymizeJSONObject(string(in))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return nil, fmt.Errorf("unable to read the transformer's output: %w", err)
	}
	return doc["value"], nil
}

// previewValues pairs every sampled value with what transform made of it, and counts the distinct
// values on both sides.
//
// A value the transformer fails on is reported against that value and does not stop the others:
// a preview that gave up at the first bad row would hide how many rows are bad.
func previewValues(
	raws []any,
	transform func(any) (any, error),
) *mgmtv1alpha1.PreviewColumnTransformerResponse {
	resp := &mgmtv1alpha1.PreviewColumnTransformerResponse{
		Values: make([]*mgmtv1alpha1.ColumnTransformerPreview, 0, len(raws)),
	}
	inputs := map[string]struct{}{}
	outputs := map[string]struct{}{}

	for _, raw := range raws {
		row := &mgmtv1alpha1.ColumnTransformerPreview{Input: toSampleValue(raw)}
		if raw != nil {
			inputs[valueToText(raw)] = struct{}{}
		}

		transformed, err := transform(raw)
		if err != nil {
			message := err.Error()
			row.Error = &message
		} else {
			row.Output = toSampleValue(transformed)
			if transformed != nil {
				outputs[valueToText(transformed)] = struct{}{}
			}
		}
		resp.Values = append(resp.Values, row)
	}

	resp.DistinctInputs = uint32(len(inputs))
	resp.DistinctOutputs = uint32(len(outputs))
	return resp
}

// resolveTransformer replaces a reference to a user-defined transformer by its configuration, so
// the preview can tell what kind of transformer it is actually running.
func (s *Service) resolveTransformer(
	ctx context.Context,
	config *mgmtv1alpha1.TransformerConfig,
) (*mgmtv1alpha1.TransformerConfig, error) {
	userDefined := config.GetUserDefinedTransformerConfig()
	if userDefined == nil {
		return config, nil
	}
	resp, err := s.transformers.Client.GetUserDefinedTransformerById(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetUserDefinedTransformerByIdRequest{
			TransformerId: userDefined.GetId(),
		}),
	)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTransformer().GetConfig(), nil
}

func isJavascriptRule(config *mgmtv1alpha1.TransformerConfig) bool {
	return config.GetTransformJavascriptConfig() != nil || config.GetGenerateJavascriptConfig() != nil
}

// maxJavascriptTrialRows is the most rows TryJavascriptRules takes in one trial.
const maxJavascriptTrialRows = 20

// previewJavascript tries the rule on the sampled rows, all in one trial.
//
// One trial, never several: each trial draws its own consistency key, so the deterministic
// pseudo functions agree within a trial and not across two. Split over several trials, the same
// value could come out two different ways in the same preview, and the one property that keeps a
// foreign key on its parent — same input, same output — could not be seen. That is also why the
// sample is capped at what one trial takes.
//
// The price is the run's own: a trial stops at the first row a rule fails on and hands back
// nothing else. The failing value gets the message; the others say they were not tried, rather
// than the preview inventing what they would have been.
//
// The whole row goes in, since a rule may read the row's other columns; only the reviewed column
// comes out.
func (s *Service) previewJavascript(
	ctx context.Context,
	sampled *sampledTable,
	column string,
	raws []any,
	config *mgmtv1alpha1.TransformerConfig,
) *mgmtv1alpha1.PreviewColumnTransformerResponse {
	if len(raws) > maxJavascriptTrialRows {
		raws = raws[:maxJavascriptTrialRows]
	}
	rows := make([]string, 0, len(raws))
	for _, row := range sampled.rows[:len(raws)] {
		bits, err := json.Marshal(row)
		if err != nil {
			return failAll(raws, fmt.Errorf("unable to hand the row to the rule: %w", err))
		}
		rows = append(rows, string(bits))
	}
	if len(rows) == 0 {
		return previewValues(raws, func(any) (any, error) { return nil, nil })
	}

	resp, err := s.transformers.Client.TryJavascriptRules(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.TryJavascriptRulesRequest{
			AccountId: sampled.accountId,
			Rules:     []*mgmtv1alpha1.JavascriptRule{{Column: column, Transformer: config}},
			Rows:      rows,
		}),
	)
	if err != nil {
		return failAll(raws, err)
	}

	failure := resp.Msg.GetFailure()
	index := 0
	return previewValues(raws, func(any) (any, error) {
		i := index
		index++
		if failure != nil {
			if uint32(i) == failure.GetRow() { //nolint:gosec // an index among at most 20 rows
				return nil, errors.New(failure.GetMessage())
			}
			return nil, errors.New("not tried: the rule stopped at the first row it failed on, as a run does")
		}
		if i >= len(resp.Msg.GetRows()) {
			return nil, fmt.Errorf("the rule returned %d rows for %d", len(resp.Msg.GetRows()), len(rows))
		}
		return columnOf(resp.Msg.GetRows()[i], column)
	})
}

// failAll reports the same error against every value.
func failAll(raws []any, err error) *mgmtv1alpha1.PreviewColumnTransformerResponse {
	return previewValues(raws, func(any) (any, error) { return nil, err })
}

// columnOf reads one column of a row the trial returned, integers kept exact.
func columnOf(rowJson, column string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(rowJson))
	decoder.UseNumber()
	var row map[string]any
	if err := decoder.Decode(&row); err != nil {
		return nil, fmt.Errorf("unable to read the rule's output: %w", err)
	}
	return row[column], nil
}
