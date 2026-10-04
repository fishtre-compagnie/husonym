package v1alpha1_jobservice

import (
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/stretchr/testify/require"
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
