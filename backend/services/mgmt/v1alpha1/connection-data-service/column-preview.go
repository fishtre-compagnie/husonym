package v1alpha1_connectiondataservice

import (
	"context"
	"encoding/json"
	"fmt"

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
// The transformer runs through the same anonymizer AnonymizeMany uses, so what the preview shows
// is what that transformer does, user-defined transformers and Presidio-backed ones included.
func (s *Service) PreviewColumnTransformer(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.PreviewColumnTransformerRequest],
) (*connect.Response[mgmtv1alpha1.PreviewColumnTransformerResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	raws, err := s.sampleColumn(
		ctx,
		req.Msg.GetConnectionId(),
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		req.Msg.GetColumn(),
		clampLimit(req.Msg.GetLimit(), defaultPreviewLimit, maxPreviewLimit),
	)
	if err != nil {
		return nil, err
	}

	anonymizer, err := jsonanonymizer.NewAnonymizer(
		jsonanonymizer.WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression:  ".value",
			Transformer: req.Msg.GetTransformer(),
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
