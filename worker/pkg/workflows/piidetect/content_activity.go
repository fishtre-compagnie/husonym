package piidetect

import (
	"context"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	contentscan "github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

const (
	// contentSampleSize is how many values of a column the API is asked to analyze.
	contentSampleSize = 50
	// contentColumnsPerCall is how many columns a call to the API names: the API reads
	// and analyzes them one after the other, and a call must end well within the time
	// the activity has.
	contentColumnsPerCall = 20
)

// doubtfulColumns names, in name order, the text columns whose profile gives
// contentscan.FreeTextMinWords words or more and that the rules found nothing in: free
// text, in which personal data shows neither in a name nor in a format. A column without
// profile, as a job that samples nothing reads it, is not one.
func doubtfulColumns(columns []*ColumnData, byRules *DetectPiiRegexResponse) []string {
	var found map[string]report.Category
	if byRules != nil {
		found = byRules.PiiColumns
	}
	var doubtful []string
	for _, column := range columns {
		if column == nil || column.Profile == nil ||
			column.Profile.Kind != profile.KindText || column.Profile.Words < contentscan.FreeTextMinWords {
			continue
		}
		if _, ok := found[column.Column]; ok {
			continue
		}
		doubtful = append(doubtful, column.Column)
	}
	slices.Sort(doubtful)
	return doubtful
}

type DetectPiiContentRequest struct {
	ConnectionId string
	TableSchema  string
	TableName    string
	// Columns are the columns whose content is analyzed.
	Columns []string
}

type DetectPiiContentResponse struct {
	// PiiColumns holds what the analyzer found, for each column it detected something in,
	// under the category it gave.
	PiiColumns map[string]report.AnalyzerFinding
	// NotAnalyzed are the columns the analyzer could not analyze, in name order.
	NotAnalyzed []string `json:",omitempty"`
	// Status is report.AnalyzerAnswered, AnalyzerPartial or AnalyzerNone.
	Status string
}

// contentProgress is the detail of the heartbeats of the content activity: how many calls
// were answered, and counts of columns. It names no column.
type contentProgress struct {
	Calls       int
	Asked       int
	Found       int
	NotAnalyzed int
}

// DetectPiiContent asks the API to analyze the content of columns of a table, 20 columns
// per call. The API reads the values from the source and has them analyzed: no value
// comes to the worker. Of its answer the activity keeps, for each column it asked about,
// the category the API gave, the entity type and the counts of what was found, and the
// names of the columns that could not be analyzed. A detection for another column is
// ignored.
//
// An API that has no analyzer says so at the first call: the activity then asks nothing
// more, and answers that there is none.
//
// The activity says that it is alive at a steady pace, also while a call is in flight.
// Nothing of what the API answers is logged. A call that fails gives an error that holds a
// fixed message and the code of the call, never the text of the API's error, which may
// quote a value of a row.
func (a *Activities) DetectPiiContent(ctx context.Context, req *DetectPiiContentRequest) (*DetectPiiContentResponse, error) {
	logger := log.With(activity.GetLogger(ctx), "tableSchema", req.TableSchema, "tableName", req.TableName)
	response := &DetectPiiContentResponse{PiiColumns: map[string]report.AnalyzerFinding{}, Status: report.AnalyzerAnswered}
	if len(req.Columns) == 0 {
		return response, nil
	}

	// What the heartbeats carry: the progress as of the last call that was answered.
	var mu sync.Mutex
	var progress contentProgress
	stop := a.keepAlive(ctx, func() any {
		mu.Lock()
		defer mu.Unlock()
		return progress
	})
	defer stop()

	calls := (len(req.Columns) + contentColumnsPerCall - 1) / contentColumnsPerCall
	call := 0
	for columns := range slices.Chunk(req.Columns, contentColumnsPerCall) {
		call++
		asked := time.Now()
		answer, err := a.content.DetectPiiInConnectionData(ctx, connect.NewRequest(&mgmtv1alpha1.DetectPiiInConnectionDataRequest{
			ConnectionId: req.ConnectionId,
			Schema:       req.TableSchema,
			Table:        req.TableName,
			Columns:      columns,
			SampleSize:   contentSampleSize,
		}))
		if connect.CodeOf(err) == connect.CodeFailedPrecondition {
			logger.Info("the API has no analyzer: the content of the columns is not analyzed")
			return &DetectPiiContentResponse{PiiColumns: map[string]report.AnalyzerFinding{}, Status: report.AnalyzerNone}, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			logger.Warn(
				"a request to analyze the content of columns failed",
				"call", call, "calls", calls, "columns", len(columns), "code", connect.CodeOf(err).String(),
				"duration", time.Since(asked), "attempt", activity.GetInfo(ctx).Attempt,
			)
			return nil, contentError(err)
		}
		logger.Debug(
			"the API analyzed the content of columns",
			"call", call, "calls", calls, "columns", len(columns),
			"duration", time.Since(asked), "attempt", activity.GetInfo(ctx).Attempt,
		)

		for _, detection := range answer.Msg.GetDetections() {
			if !slices.Contains(columns, detection.GetColumn()) {
				continue
			}
			response.PiiColumns[detection.GetColumn()] = report.AnalyzerFinding{
				Category: report.Category(detection.GetDataCategory()),
				Entity:   detection.GetEntityType(),
				Matches:  int(detection.GetMatchCount()),
				Sampled:  int(detection.GetSampledCount()),
			}
		}
		for _, verdict := range answer.Msg.GetVerdicts() {
			if verdict.GetContentNotAnalyzed() {
				response.NotAnalyzed = append(response.NotAnalyzed, verdict.GetColumn())
			}
		}

		mu.Lock()
		progress = contentProgress{
			Calls:       call,
			Asked:       min(call*contentColumnsPerCall, len(req.Columns)),
			Found:       len(response.PiiColumns),
			NotAnalyzed: len(response.NotAnalyzed),
		}
		beat := progress
		mu.Unlock()
		a.heartbeat(ctx, beat)
	}

	slices.Sort(response.NotAnalyzed)
	if len(response.NotAnalyzed) > 0 {
		response.Status = report.AnalyzerPartial
		logger.Warn("the content of some columns was not analyzed", "columns", len(response.NotAnalyzed))
	}
	return response, nil
}
