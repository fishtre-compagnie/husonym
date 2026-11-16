package piidetect

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
)

type DetectPiiLLMRequest struct {
	TableSchema string
	TableName   string
	ColumnData  []*ColumnData
	// ShouldSample is a member of the recorded form of the request. The workflow leaves
	// it false and this activity never reads it: rows are only read for Input.
	ShouldSample bool
	// ConnectionId is the connection to read values from; empty unless Input asks for
	// values.
	ConnectionId string
	UserPrompt   string
	// Input is "values" when the job sends sample values to the model.
	Input string `json:",omitempty"`
}

type DetectPiiLLMResponse struct {
	// PiiColumns holds what the model found, for each column it says holds personal
	// data with a confidence at or above the threshold.
	PiiColumns map[string]report.ModelFinding
	// Input is what the model was given: report.InputNames, InputProfiles or
	// InputValues. Empty when it was not asked.
	Input string `json:",omitempty"`
	// Status is report.ModelAnswered, ModelPartial or ModelNone.
	Status string `json:",omitempty"`
	// Model is the name of the model that was asked.
	Model          string             `json:",omitempty"`
	Unanswered     []string           `json:",omitempty"`
	BelowThreshold []report.Dismissed `json:",omitempty"`
}

// modelProgress is what the activity has learned so far. It is the detail of its
// heartbeats, which a new attempt starts from: it holds column names, categories and
// confidences, never what was sent.
type modelProgress struct {
	Batches        int
	Findings       map[string]report.ModelFinding
	BelowThreshold []report.Dismissed
	Unanswered     []string
}

// DetectPiiLLM asks the model about the columns of a table, a batch of columns per
// request. Without a configured model it asks nothing and says so.
//
// The model is given the names, the types and the profiles the request carries. Only
// when Input asks for values does the activity read rows, by itself, and add to each
// column a few values, bounded in number and in length: values are never carried by a
// payload. When they cannot be read the model is asked without them.
//
// The activity says that it is alive at a steady pace, also while a request is in flight
// or waited for, each time with what it has learned so far; an attempt that follows
// another asks only the batches that are missing. Nothing of a request or of an answer is
// logged.
//
// A table for which most columns are left without a valid answer was not scanned by the
// model: the activity then fails, see unansweredError.
func (a *Activities) DetectPiiLLM(ctx context.Context, req *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
	if a.classifier == nil {
		return &DetectPiiLLMResponse{PiiColumns: map[string]report.ModelFinding{}, Status: report.ModelNone}, nil
	}
	logger := log.With(activity.GetLogger(ctx), "tableSchema", req.TableSchema, "tableName", req.TableName)
	response := &DetectPiiLLMResponse{
		PiiColumns: map[string]report.ModelFinding{},
		Status:     report.ModelAnswered,
		Model:      a.classifier.Model(),
	}
	if len(req.ColumnData) == 0 {
		return response, nil
	}

	columns := make([]model.Column, 0, len(req.ColumnData))
	response.Input = report.InputNames
	for _, column := range req.ColumnData {
		if column == nil {
			continue
		}
		if column.Profile != nil {
			response.Input = report.InputProfiles
		}
		columns = append(columns, model.Column{
			Name:     column.Column,
			DataType: column.DataType,
			Nullable: column.IsNullable,
			Profile:  column.Profile.ForModel(),
		})
	}
	table := model.Table{Name: req.TableName, Hints: req.UserPrompt}
	if req.Input == report.InputValues && req.ConnectionId != "" {
		if a.addValues(ctx, req, columns, logger) {
			response.Input, table.SendsValues = report.InputValues, true
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if utf8.RuneCountInString(req.UserPrompt) > model.MaxHints {
		logger.Warn("the user prompt of the job is longer than what the model is given: it is cut", "limit", model.MaxHints)
	}

	var progress modelProgress
	if activity.HasHeartbeatDetails(ctx) {
		if err := activity.GetHeartbeatDetails(ctx, &progress); err != nil {
			progress = modelProgress{}
		}
	}
	// What the heartbeats carry: the progress as of the last batch that was answered.
	var mu sync.Mutex
	learned := progress.clone()
	stop := a.keepAlive(ctx, func() any {
		mu.Lock()
		defer mu.Unlock()
		return learned
	})
	defer stop()

	batches := model.Batches(columns)
	ignored := 0
	for i := min(progress.Batches, len(batches)); i < len(batches); i++ {
		asked := time.Now()
		result, err := a.classifier.Classify(ctx, table, batches[i])
		if err != nil {
			var failure *model.Error
			status := 0
			if errors.As(err, &failure) {
				status = failure.Status
			}
			logger.Warn(
				"a request to the model failed",
				"batch", i+1, "batches", len(batches), "columns", len(batches[i]), "input", response.Input,
				"status", status, "duration", time.Since(asked), "attempt", activity.GetInfo(ctx).Attempt,
			)
			return nil, modelError(err)
		}
		logger.Debug(
			"the model answered",
			"batch", i+1, "batches", len(batches), "columns", len(batches[i]), "input", response.Input,
			"duration", time.Since(asked), "attempt", activity.GetInfo(ctx).Attempt,
		)
		if progress.Findings == nil {
			progress.Findings = map[string]report.ModelFinding{}
		}
		for column, finding := range result.Findings {
			progress.Findings[column] = finding
		}
		progress.BelowThreshold = append(progress.BelowThreshold, result.BelowThreshold...)
		progress.Unanswered = append(progress.Unanswered, result.Unanswered...)
		ignored += result.Ignored
		progress.Batches = i + 1
		mu.Lock()
		learned = progress.clone()
		mu.Unlock()
		a.heartbeat(ctx, learned)
	}
	if err := unansweredError(len(progress.Unanswered), len(columns)); err != nil {
		logger.Warn(
			"the model gave no valid answer for most columns of the table",
			"columns", len(columns), "unanswered", len(progress.Unanswered),
		)
		return nil, err
	}

	for column, finding := range progress.Findings {
		response.PiiColumns[column] = finding
	}
	response.BelowThreshold = progress.BelowThreshold
	response.Unanswered = progress.Unanswered
	if len(response.Unanswered) > 0 {
		response.Status = report.ModelPartial
		logger.Warn("the model gave no valid answer for some columns", "columns", len(response.Unanswered))
	}
	if ignored > 0 {
		logger.Warn("the model answered for columns it was not asked about: these answers are ignored", "answers", ignored)
	}
	return response, nil
}

// clone copies the progress, so that a heartbeat does not read what a batch is writing.
func (p modelProgress) clone() modelProgress {
	return modelProgress{
		Batches:        p.Batches,
		Findings:       maps.Clone(p.Findings),
		BelowThreshold: slices.Clone(p.BelowThreshold),
		Unanswered:     slices.Clone(p.Unanswered),
	}
}

// keepAlive records a heartbeat every heartbeatEvery until stop is called, each with the
// progress of the moment. The heartbeat timeout of the activity then only ends an attempt
// whose worker is gone: a request may last longer than that timeout, and so may the waits
// between the tries of a request.
func (a *Activities) keepAlive(ctx context.Context, progress func() any) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(a.heartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.heartbeat(ctx, progress())
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// unansweredError is the failure of a table whose model left most columns without a
// valid answer, nil otherwise. "Most" is more than half: up to half, what the model said
// of the other columns is worth storing and the unanswered ones are listed in the report;
// past it the report would rest on the rules for most of the table while saying that the
// model answered. The failure is not retried: at a temperature of 0 the answers would be
// the same.
func unansweredError(unanswered, columns int) error {
	if unanswered*2 <= columns {
		return nil
	}
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("the model gave no valid answer for %d of the %d columns of the table", unanswered, columns),
		errorTypeModelUnanswered,
		nil,
	)
}

// addValues reads rows of the table and gives each column the values it may show. It
// returns false when no column got any: the table could not be read, is empty, or holds
// nothing that may be shown.
func (a *Activities) addValues(
	ctx context.Context,
	req *DetectPiiLLMRequest,
	columns []model.Column,
	logger log.Logger,
) bool {
	const without = "the model is asked without sample values"

	connection, err := a.connection(ctx, req.ConnectionId)
	if err != nil {
		logger.Warn("the connection could not be read: "+without, "error", err)
		return false
	}
	data, err := a.data.NewDataConnection(temporallogger.NewSlogger(logger), connection)
	if err != nil {
		logger.Warn("the source could not be opened: "+without, "error", err)
		return false
	}
	picker := newValuePicker(engineOf(connection), req.ColumnData)
	stream, err := a.sample(ctx, data, req.TableSchema, req.TableName, picker.add)
	if err != nil {
		// The error of a row that could not be read may quote the row: it is not logged.
		logger.Warn(
			"the rows of the table could not be read: "+without,
			"timedOut", errors.Is(err, context.DeadlineExceeded), "rowsRead", stream.rows,
		)
		return false
	}
	added := false
	for i := range columns {
		columns[i].Values = picker.values[columns[i].Name]
		added = added || len(columns[i].Values) > 0
	}
	if !added {
		logger.Warn("the table holds no value that may be shown: " + without)
	}
	return added
}
