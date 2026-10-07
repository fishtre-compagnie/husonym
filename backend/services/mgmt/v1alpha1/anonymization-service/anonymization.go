package v1alpha_anonymizationservice

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/licenserefusal"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/metrics"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	jsonanonymizer "github.com/fishtre-compagnie/husonym/internal/json-anonymizer"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	inputMetricStr        = "input_received"
	outputMetricStr       = "output_sent"
	outputErrorCounterStr = "output_error"
)

// customTransformersRefusal gives the reason the license does not let the request run a
// transformer it carries, or nothing: JavaScript, to transform or to generate, or a
// user-defined transformer, in a mapping, as a default transformer or among the anonymizers of
// a PII text. The request executes what it carries, so it is refused whole: a user-defined
// transformer is not even resolved.
func (s *Service) customTransformersRefusal(msg transformerMsgToValidate) string {
	for cfg := range getTransformerConfigsToValidate(msg) {
		if job_util.RunsCustomTransformer(cfg) {
			return license.FeatureRefusal(s.license, license.FeatureCustomTransformers)
		}
	}
	return ""
}

// carriesPiiText tells whether a mapping or a default transformer of the request is a PII text.
func carriesPiiText(msg transformerMsgToValidate) bool {
	for cfg := range getTransformerConfigsToValidate(msg) {
		if cfg.GetTransformPiiTextConfig() != nil {
			return true
		}
	}
	return false
}

// countRefusal counts the refusal of the feature that AnonymizeMany answers as not implemented,
// and so not as a license refusal that an interceptor would see. The gate is the one the reason
// says: no license in force, or the feature the license does not include. The refusal is counted
// only once the caller is known to reach the account, so that no caller counts against an account
// of someone else; a caller that does not reach it gets the answer of the access check.
func (s *Service) countRefusal(ctx context.Context, accountId string, f license.Feature, reason string) error {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return err
	}
	if err := user.EnforceAccountAccess(ctx, accountId); err != nil {
		return err
	}
	gate := license.FeatureGate(f)
	if reason == license.NotInForceMessage {
		gate = license.GateNotInForce
	}
	licenserefusal.Count(ctx, s.refusals, accountId, []license.Gate{gate})
	return nil
}

func (s *Service) AnonymizeMany(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.AnonymizeManyRequest],
) (*connect.Response[mgmtv1alpha1.AnonymizeManyResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	notImplemented := func(reason string) error {
		return husonymerrors.NewNotImplemented(
			fmt.Sprintf(
				"%s is not implemented: %s",
				strings.TrimPrefix(
					mgmtv1alpha1connect.AnonymizationServiceAnonymizeManyProcedure,
					"/",
				),
				reason,
			),
		)
	}
	// A license that is not in force is said as such, as every gated call says it: it includes
	// no feature, and naming one as missing would name the wrong cause.
	// The refusal is counted once access to the account is verified, which comes before it is told.
	if reason := license.FeatureRefusal(s.license, license.FeaturePiiText); reason != "" {
		if err := s.countRefusal(ctx, req.Msg.GetAccountId(), license.FeaturePiiText, reason); err != nil {
			return nil, err
		}
		return nil, notImplemented(reason)
	}
	if reason := s.customTransformersRefusal(req.Msg); reason != "" {
		if err := s.countRefusal(ctx, req.Msg.GetAccountId(), license.FeatureCustomTransformers, reason); err != nil {
			return nil, err
		}
		return nil, notImplemented(reason)
	}

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceAccountAccess(ctx, req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	account, err := s.db.Q.GetAccount(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}
	if account.AccountType == int16(husonymdb.AccountType_Personal) {
		return nil, husonymerrors.NewForbidden(
			fmt.Sprintf(
				"%s is not implemented for personal accounts",
				strings.TrimPrefix(
					mgmtv1alpha1connect.AnonymizationServiceAnonymizeManyProcedure,
					"/",
				),
			),
		)
	}

	for cfg := range getTransformerConfigsToValidate(req.Msg) {
		if err := validateTransformerConfig(cfg); err != nil {
			return nil, err
		}
	}

	requestedCount := uint64(len(req.Msg.InputData))
	resp, err := s.useraccountService.IsAccountStatusValid(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
			AccountId:            req.Msg.GetAccountId(),
			RequestedRecordCount: &requestedCount,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve account status: %w", err)
	}

	if !resp.Msg.IsValid {
		return nil, husonymerrors.NewBadRequest(
			fmt.Sprintf(
				"unable to anonymize due to account in invalid state. Reason: %q",
				*resp.Msg.Reason,
			),
		)
	}

	anonymizer, err := jsonanonymizer.NewAnonymizer(
		ctx,
		jsonanonymizer.WithTransformerMappings(req.Msg.TransformerMappings),
		jsonanonymizer.WithDefaultTransformers(req.Msg.DefaultTransformers),
		jsonanonymizer.WithHaltOnFailure(req.Msg.HaltOnFailure),
		// The license was read above, for the whole request. The values of a bulk request
		// belong to no run: their hashes are computed under the key the process keeps for
		// the account.
		jsonanonymizer.WithPiiText(s.piiText, true, s.piiText.AccountHashKey(req.Msg.GetAccountId())),
		jsonanonymizer.WithUserDefinedTransformerResolver(
			transformer_executor.NewUserDefinedTransformerResolver(s.transformerClient, req.Msg.GetAccountId()),
		),
		jsonanonymizer.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	var outputErrorCounter, outputCounter metric.Int64Counter
	var labels []attribute.KeyValue
	if s.meter != nil {
		labels = getMetricLabels(ctx, "anonymizeMany", req.Msg.GetAccountId())
		counter, err := s.meter.Int64Counter(inputMetricStr)
		if err != nil {
			return nil, err
		}
		counter.Add(ctx, int64(len(req.Msg.InputData)), metric.WithAttributes(labels...))
		outputCounter, err = s.meter.Int64Counter(outputMetricStr)
		if err != nil {
			return nil, err
		}
		outputErrorCounter, err = s.meter.Int64Counter(outputErrorCounterStr)
		if err != nil {
			return nil, err
		}
	}

	outputData, anonymizeErrors := anonymizer.AnonymizeJSONObjects(req.Msg.InputData)

	if outputCounter != nil {
		anonymizedCounter := 0
		for _, js := range outputData {
			if js != "" {
				anonymizedCounter += 1
			}
		}
		outputCounter.Add(ctx, int64(anonymizedCounter), metric.WithAttributes(labels...))
	}

	if outputErrorCounter != nil && len(anonymizeErrors) > 0 {
		outputErrorCounter.Add(ctx, int64(len(anonymizeErrors)), metric.WithAttributes(labels...))
	}

	errors := []*mgmtv1alpha1.AnonymizeManyErrors{}
	for _, e := range anonymizeErrors {
		errors = append(errors, &mgmtv1alpha1.AnonymizeManyErrors{
			InputIndex:   e.InputIndex,
			ErrorMessage: e.Message,
		})
	}

	return connect.NewResponse(&mgmtv1alpha1.AnonymizeManyResponse{
		OutputData: outputData,
		Errors:     errors,
	}), nil
}

func (s *Service) AnonymizeSingle(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.AnonymizeSingleRequest],
) (*connect.Response[mgmtv1alpha1.AnonymizeSingleResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceAccountAccess(ctx, req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	if _, err := husonymdb.ToUuid(req.Msg.GetAccountId()); err != nil {
		return nil, err
	}

	licensed := s.license.HasFeature(license.FeaturePiiText)
	if !licensed && carriesPiiText(req.Msg) {
		return nil, userdata.FeatureRefusal(s.license, req.Msg.GetAccountId(), license.FeaturePiiText)
	}
	// The worker calls this during a run for a PII text whose anonymizers may be user-defined.
	// Such a job does not start without custom_transformers (the job gate counts what the
	// anonymizers of a PII text run), so a licensed run is never refused here.
	if s.customTransformersRefusal(req.Msg) != "" {
		return nil, userdata.FeatureRefusal(s.license, req.Msg.GetAccountId(), license.FeatureCustomTransformers)
	}

	for cfg := range getTransformerConfigsToValidate(req.Msg) {
		if err := validateTransformerConfig(cfg); err != nil {
			return nil, err
		}
	}

	hashKey, err := s.runHashKey(user, req.Header())
	if err != nil {
		return nil, err
	}
	if hashKey == nil {
		// No run: the key the process keeps for the account.
		hashKey = s.piiText.AccountHashKey(req.Msg.GetAccountId())
	}

	requestedCount := uint64(len(req.Msg.InputData))
	resp, err := s.useraccountService.IsAccountStatusValid(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
			AccountId:            req.Msg.GetAccountId(),
			RequestedRecordCount: &requestedCount,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve account status: %w", err)
	}

	if !resp.Msg.IsValid {
		return nil, husonymerrors.NewBadRequest(
			fmt.Sprintf(
				"unable to anonymize due to account in invalid state. Reason: %q",
				*resp.Msg.Reason,
			),
		)
	}

	anonymizer, err := jsonanonymizer.NewAnonymizer(
		ctx,
		jsonanonymizer.WithTransformerMappings(req.Msg.TransformerMappings),
		jsonanonymizer.WithDefaultTransformers(req.Msg.DefaultTransformers),
		jsonanonymizer.WithPiiText(s.piiText, licensed, hashKey),
		jsonanonymizer.WithUserDefinedTransformerResolver(
			transformer_executor.NewUserDefinedTransformerResolver(s.transformerClient, req.Msg.GetAccountId()),
		),
		jsonanonymizer.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	var outputCounter, outputErrorCounter metric.Int64Counter
	var labels []attribute.KeyValue
	if s.meter != nil {
		labels = getMetricLabels(ctx, "anonymizeSingle", req.Msg.GetAccountId())
		counter, err := s.meter.Int64Counter(inputMetricStr)
		if err != nil {
			return nil, err
		}
		counter.Add(ctx, 1, metric.WithAttributes(labels...))
		outputCounter, err = s.meter.Int64Counter(outputMetricStr)
		if err != nil {
			return nil, err
		}
		outputErrorCounter, err = s.meter.Int64Counter(outputErrorCounterStr)
		if err != nil {
			return nil, err
		}
	}

	outputData, err := anonymizer.AnonymizeJSONObject(req.Msg.InputData)
	if err != nil {
		if outputErrorCounter != nil {
			outputErrorCounter.Add(ctx, int64(1), metric.WithAttributes(labels...))
		}
		answer := husonymerrors.FromPresidio(ctx, err)
		if husonymerrors.IsServiceFault(answer) {
			// Why is logged, and not always told: the error of a Presidio that did not answer
			// can quote where it is reached.
			logger.Error("unable to anonymize the input", "error", err)
		}
		return nil, answer
	}

	if outputCounter != nil {
		outputCounter.Add(ctx, 1, metric.WithAttributes(labels...))
	}

	return connect.NewResponse(&mgmtv1alpha1.AnonymizeSingleResponse{
		OutputData: outputData,
	}), nil
}

func getMetricLabels(ctx context.Context, requestName, accountId string) []attribute.KeyValue {
	requestId := getTraceID(ctx)
	if requestId == "" {
		requestId = uuid.NewString()
	}
	return []attribute.KeyValue{
		attribute.String(metrics.AccountIdLabel, accountId),
		attribute.String(metrics.ApiRequestId, requestId),
		attribute.String(metrics.ApiRequestName, requestName),
		attribute.String(
			metrics.HusonymDateLabel,
			time.Now().UTC().Format(metrics.HusonymDateFormat),
		),
	}
}

func getTraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		traceID := spanCtx.TraceID()
		return traceID.String()
	}
	return ""
}

// runHashKey reads the key a run hands with its calls, under which the hashes of
// TransformPiiText are computed: the same text then has the same hash in every table of the
// run's consistency scope. Only the worker hands one, so the header counts from the worker
// alone: from any other caller it is not read at all, and the hashes are computed under the
// key the process keeps for the account, as for a request that carries none.
func (s *Service) runHashKey(user *userdata.User, header http.Header) (*piitext.HashKey, error) {
	encoded := header.Get(piitext.HashKeyHeader)
	if encoded == "" || s.cfg.WorkerOnly.Allow(user) != nil {
		return nil, nil
	}
	key, err := piitext.ParseHashKey(encoded)
	if err != nil {
		return nil, husonymerrors.NewBadRequest(fmt.Sprintf("%s: %s", piitext.HashKeyHeader, err.Error()))
	}
	return &key, nil
}

// validateTransformerConfig refuses a transformer config that cannot be applied: none at all,
// or a TransformPiiText one the transformer refuses, in its words.
func validateTransformerConfig(cfg *mgmtv1alpha1.TransformerConfig) error {
	if cfg == nil {
		return fmt.Errorf("transformer config is nil")
	}
	root := cfg.GetTransformPiiTextConfig()
	if root == nil {
		return nil
	}
	if err := piitext.Validate(root); err != nil {
		return husonymerrors.NewBadRequest(err.Error())
	}
	return nil
}

type transformerMsgToValidate interface {
	GetDefaultTransformers() *mgmtv1alpha1.DefaultTransformersConfig
	GetTransformerMappings() []*mgmtv1alpha1.TransformerMapping
}

func getTransformerConfigsToValidate(
	msg transformerMsgToValidate,
) iter.Seq[*mgmtv1alpha1.TransformerConfig] {
	return func(yield func(*mgmtv1alpha1.TransformerConfig) bool) {
		if msg.GetDefaultTransformers().GetBoolean() != nil {
			if !yield(msg.GetDefaultTransformers().GetBoolean()) {
				return
			}
		}
		if msg.GetDefaultTransformers().GetN() != nil {
			if !yield(msg.GetDefaultTransformers().GetN()) {
				return
			}
		}
		if msg.GetDefaultTransformers().GetS() != nil {
			if !yield(msg.GetDefaultTransformers().GetS()) {
				return
			}
		}

		for _, mapping := range msg.GetTransformerMappings() {
			if mapping.GetTransformer() != nil {
				if !yield(mapping.GetTransformer()) {
					return
				}
			}
		}
	}
}
