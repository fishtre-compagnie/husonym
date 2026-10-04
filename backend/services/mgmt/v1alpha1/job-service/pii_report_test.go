package v1alpha1_jobservice

import (
	"errors"
	"log/slog"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/converter"
)

// A table report is read whether or not it holds the members a worker may add to it: the
// answer is made of the members every report has.
func Test_PiiDetectionReport_ReadsAStoredTableReport(t *testing.T) {
	stored := map[string]string{
		"without the optional members": `{
			"table_schema": "public", "table_name": "users",
			"column_reports": [
				{"column_name": "age", "report": {"regex": {"category": "personal"}, "llm": null}},
				{"column_name": "email", "report": {"regex": {"category": "contact"}, "llm": {"category": "contact", "confidence": 0.95}}},
				{"column_name": "ref", "report": {"regex": null, "llm": {"category": "a label of the model", "confidence": 7}}}
			],
			"scanned_columns": ["id", "age", "email", "ref"]
		}`,
		"with them": `{
			"table_schema": "public", "table_name": "users",
			"column_reports": [
				{"column_name": "age", "report": {"regex": {"category": "personal", "evidence": "name"}, "llm": null}},
				{"column_name": "email", "report": {"regex": {"category": "contact", "evidence": "values:email 0.99"}, "llm": {"category": "contact", "confidence": 0.95}}},
				{"column_name": "ref", "report": {"regex": null, "llm": {"category": "a label of the model", "confidence": 7}}}
			],
			"scanned_columns": ["id", "age", "email", "ref"],
			"scan": {"sampled_rows": 200, "input": "profiles", "model": "local-model", "model_status": "partial",
				"unanswered": ["id"], "below_threshold": [{"column_name": "id", "category": "personal", "confidence": 0.2}]}
		}`,
	}
	for name, value := range stored {
		t.Run(name, func(t *testing.T) {
			reports, err := getReportsFromTableContexts([]*db_queries.HusonymApiRuncontext{{Value: []byte(value)}})
			require.NoError(t, err)
			tables := getTableReportDtos(reports)

			require.Len(t, tables, 1)
			require.Equal(t, "public", tables[0].GetSchema())
			require.Equal(t, "users", tables[0].GetTable())
			columns := tables[0].GetColumns()
			require.Len(t, columns, 3)

			require.Equal(t, "age", columns[0].GetColumn())
			require.Equal(t, "personal", columns[0].GetRegexReport().GetCategory())
			require.Nil(t, columns[0].GetLlmReport())

			require.Equal(t, "email", columns[1].GetColumn())
			require.Equal(t, "contact", columns[1].GetRegexReport().GetCategory())
			require.Equal(t, "contact", columns[1].GetLlmReport().GetCategory())
			require.InDelta(t, 0.95, columns[1].GetLlmReport().GetConfidence(), 1e-6)

			// A label and a confidence stored as a model gave them are returned as stored.
			require.Equal(t, "ref", columns[2].GetColumn())
			require.Nil(t, columns[2].GetRegexReport())
			require.Equal(t, "a label of the model", columns[2].GetLlmReport().GetCategory())
			require.InDelta(t, 7, columns[2].GetLlmReport().GetConfidence(), 1e-6)
		})
	}
}

func Test_PiiDetectionReport_ATableInWhichNothingWasFound(t *testing.T) {
	reports, err := getReportsFromTableContexts([]*db_queries.HusonymApiRuncontext{
		{Value: []byte(`{"table_schema":"public","table_name":"empty","column_reports":[]}`)},
	})
	require.NoError(t, err)
	tables := getTableReportDtos(reports)
	require.Len(t, tables, 1)
	require.Equal(t, "empty", tables[0].GetTable())
	require.Empty(t, tables[0].GetColumns())
}

// The run of a table that fails does not end the run of a PII detection job, which goes on
// with the other tables: the events of the run are then not all there yet. The failure of
// any other child ends the run of its parent.
func Test_childFailureEndsRun(t *testing.T) {
	require.False(t, childFailureEndsRun("TablePiiDetect"))
	require.True(t, childFailureEndsRun("TableSync"))
	require.True(t, childFailureEndsRun("ProcessAccountHook"))
	require.True(t, childFailureEndsRun(""))
}

// events is the history of a run, as the events reader walks it.
type events struct {
	events []*historypb.HistoryEvent
	next   int
}

func (e *events) HasNext() bool { return e.next < len(e.events) }

func (e *events) Next() (*historypb.HistoryEvent, error) {
	e.next++
	return e.events[e.next-1], nil
}

// The events of a run whose child ended one of the ways a table may end without being
// scanned: failed, not started, timed out. For the run of a table of a PII detection job
// the run goes on, and its events are not all there yet; for any other child the run is
// over. The metadata of the table come from the input of its child.
func Test_getEventsByWorkflowId_AChildThatDidNotComplete(t *testing.T) {
	input, err := converter.GetDefaultDataConverter().ToPayloads(
		&piidetect.TablePiiDetectRequest{TableSchema: "public", TableName: "users"},
	)
	require.NoError(t, err)
	initiated := func(workflowType string) *historypb.HistoryEvent {
		return &historypb.HistoryEvent{
			EventId: 5, EventType: enums.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED,
			Attributes: &historypb.HistoryEvent_StartChildWorkflowExecutionInitiatedEventAttributes{
				StartChildWorkflowExecutionInitiatedEventAttributes: &historypb.StartChildWorkflowExecutionInitiatedEventAttributes{
					WorkflowId: "child", WorkflowType: &commonpb.WorkflowType{Name: workflowType}, Input: input,
				},
			},
		}
	}
	endings := map[string]func(workflowType string) *historypb.HistoryEvent{
		"failed": func(workflowType string) *historypb.HistoryEvent {
			return &historypb.HistoryEvent{
				EventId: 6, EventType: enums.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_FAILED,
				Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionFailedEventAttributes{
					ChildWorkflowExecutionFailedEventAttributes: &historypb.ChildWorkflowExecutionFailedEventAttributes{
						InitiatedEventId: 5, WorkflowType: &commonpb.WorkflowType{Name: workflowType},
						Failure: &failurepb.Failure{Message: "the columns cannot be read"},
					},
				},
			}
		},
		"not started": func(workflowType string) *historypb.HistoryEvent {
			return &historypb.HistoryEvent{
				EventId: 6, EventType: enums.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_FAILED,
				Attributes: &historypb.HistoryEvent_StartChildWorkflowExecutionFailedEventAttributes{
					StartChildWorkflowExecutionFailedEventAttributes: &historypb.StartChildWorkflowExecutionFailedEventAttributes{
						InitiatedEventId: 5, WorkflowType: &commonpb.WorkflowType{Name: workflowType},
					},
				},
			}
		},
		"timed out": func(workflowType string) *historypb.HistoryEvent {
			return &historypb.HistoryEvent{
				EventId: 6, EventType: enums.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_TIMED_OUT,
				Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionTimedOutEventAttributes{
					ChildWorkflowExecutionTimedOutEventAttributes: &historypb.ChildWorkflowExecutionTimedOutEventAttributes{
						InitiatedEventId: 5, WorkflowType: &commonpb.WorkflowType{Name: workflowType},
					},
				},
			}
		},
	}
	for ending, event := range endings {
		for workflowType, wantComplete := range map[string]bool{"TablePiiDetect": false, "TableSync": true} {
			t.Run(ending+" "+workflowType, func(t *testing.T) {
				temporal := clientmanager.NewMockInterface(t)
				temporal.On("GetWorkflowHistory", mock.Anything, "account-1", "run-1", mock.Anything).
					Return(&events{events: []*historypb.HistoryEvent{initiated(workflowType), event(workflowType)}}, nil)
				// A run that is over is asked about the children it did not see end.
				temporal.On("GetWorkflowExecutionById", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil, errors.New("not asked in this test")).Maybe()
				svc := New(&Config{}, nil, temporal, nil, nil, nil, nil, nil)

				resp, err := svc.getEventsByWorkflowId(t.Context(), "account-1", "run-1", slog.Default())
				require.NoError(t, err)
				require.Equal(t, wantComplete, resp.GetIsRunComplete())
				require.Len(t, resp.GetEvents(), 1)
				require.Equal(t, workflowType, resp.GetEvents()[0].GetType())
				if workflowType == "TablePiiDetect" {
					require.Equal(t, "public", resp.GetEvents()[0].GetMetadata().GetSyncMetadata().GetSchema())
					require.Equal(t, "users", resp.GetEvents()[0].GetMetadata().GetSyncMetadata().GetTable())
				}
			})
		}
	}
}
