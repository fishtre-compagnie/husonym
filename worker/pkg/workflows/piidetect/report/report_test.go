package report

import (
	"encoding/json"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func Test_ExternalIds(t *testing.T) {
	require.Equal(t, "job-1--job-pii-report", JobReportExternalId("job-1"))
	require.Equal(t, "public.users--table-pii-report", TableReportExternalId("public", "users"))
	require.Equal(t, "--table-pii-report", TableReportSuffix)
}

// The six names are stored in the reports and shown by the readers.
func Test_Category_Valid(t *testing.T) {
	for _, name := range []string{"national_id", "contact", "financial", "personal", "location", "authentication"} {
		require.True(t, Category(name).Valid(), name)
	}
	for _, name := range []string{"", "none", "Contact", "email"} {
		require.False(t, Category(name).Valid(), name)
	}
}

// A report that holds none of the optional members is stored with the members every
// reader knows, and nothing else.
func Test_TableReport_StoredFormWithoutOptionalMembers(t *testing.T) {
	stored, err := json.Marshal(&TableReport{
		TableSchema: "public",
		TableName:   "users",
		ColumnReports: []ColumnReport{
			{ColumnName: "age", Report: Combined{Regex: &RuleFinding{Category: Personal}}},
			{ColumnName: "email", Report: Combined{
				Regex: &RuleFinding{Category: Contact},
				LLM:   &ModelFinding{Category: Contact, Confidence: 0.95},
			}},
			{ColumnName: "ref", Report: Combined{LLM: &ModelFinding{Category: NationalID, Confidence: 0.7}}},
		},
		ScannedColumns: []string{"id", "age", "email", "ref"},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"table_schema": "public",
		"table_name": "users",
		"column_reports": [
			{"column_name": "age", "report": {"regex": {"category": "personal"}, "llm": null}},
			{"column_name": "email", "report": {"regex": {"category": "contact"}, "llm": {"category": "contact", "confidence": 0.95}}},
			{"column_name": "ref", "report": {"regex": null, "llm": {"category": "national_id", "confidence": 0.7}}}
		],
		"scanned_columns": ["id", "age", "email", "ref"]
	}`, string(stored))

	empty, err := json.Marshal(&TableReport{TableSchema: "public", TableName: "empty", ColumnReports: []ColumnReport{}})
	require.NoError(t, err)
	require.JSONEq(t, `{"table_schema":"public","table_name":"empty","column_reports":[]}`, string(empty))
}

func Test_TableReport_StoredFormWithEveryMember(t *testing.T) {
	stored, err := json.Marshal(&TableReport{
		TableSchema: "public",
		TableName:   "users",
		ColumnReports: []ColumnReport{
			{ColumnName: "iban", Report: Combined{Regex: &RuleFinding{Category: Financial, Evidence: "values:iban 0.97"}}},
		},
		ScannedColumns: []string{"iban", "note"},
		Scan: &Scan{
			SampledRows:    200,
			Input:          "profiles",
			Model:          "local-model",
			ModelStatus:    "partial",
			Sources:        []string{SourceRules, SourceModel},
			Unanswered:     []string{"note"},
			BelowThreshold: []Dismissed{{ColumnName: "iban", Category: Financial, Confidence: 0.3}},
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"table_schema": "public",
		"table_name": "users",
		"column_reports": [
			{"column_name": "iban", "report": {"regex": {"category": "financial", "evidence": "values:iban 0.97"}, "llm": null}}
		],
		"scanned_columns": ["iban", "note"],
		"scan": {
			"sampled_rows": 200,
			"input": "profiles",
			"model": "local-model",
			"model_status": "partial",
			"sources": ["rules", "model"],
			"unanswered": ["note"],
			"below_threshold": [{"column_name": "iban", "category": "financial", "confidence": 0.3}]
		}
	}`, string(stored))

	bare, err := json.Marshal(&Scan{ModelStatus: "none"})
	require.NoError(t, err)
	require.JSONEq(t, `{"sampled_rows":0,"model_status":"none"}`, string(bare))
}

// A stored report may hold a label outside the six names: it reads back as it is.
func Test_TableReport_ReadsALabelOutsideTheSixNames(t *testing.T) {
	var read TableReport
	require.NoError(t, json.Unmarshal([]byte(`{
		"table_schema": "public", "table_name": "users",
		"column_reports": [{"column_name": "c", "report": {"regex": null, "llm": {"category": "PII", "confidence": 7}}}]
	}`), &read))
	require.Equal(t, Category("PII"), read.ColumnReports[0].Report.LLM.Category)
	require.Equal(t, float32(7), read.ColumnReports[0].Report.LLM.Confidence)
	require.Nil(t, read.Scan)
}

func Test_JobReport_StoredForm(t *testing.T) {
	key := &mgmtv1alpha1.RunContextKey{JobRunId: "run-1", ExternalId: "public.users--table-pii-report", AccountId: "account-1"}

	stored, err := json.Marshal(&JobReport{
		SuccessfulTableReports: []*TableEntry{
			{TableSchema: "public", TableName: "users", ReportKey: key, ScanFingerprint: "abc"},
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"successfulTableReports":[{
		"tableSchema": "public", "tableName": "users",
		"reportKey": {"jobRunId": "run-1", "externalId": "public.users--table-pii-report", "accountId": "account-1"},
		"scanFingerprint": "abc"
	}]}`, string(stored))

	stored, err = json.Marshal(&JobReport{
		SuccessfulTableReports: []*TableEntry{
			{TableSchema: "public", TableName: "users", ReportKey: key, ScanFingerprint: "abc", Incomplete: true},
		},
		FailedTables: []*FailedTable{{TableSchema: "public", TableName: "orders", Reason: "the columns cannot be read"}},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"successfulTableReports": [{
			"tableSchema": "public", "tableName": "users",
			"reportKey": {"jobRunId": "run-1", "externalId": "public.users--table-pii-report", "accountId": "account-1"},
			"scanFingerprint": "abc",
			"incomplete": true
		}],
		"failedTables": [{"tableSchema": "public", "tableName": "orders", "reason": "the columns cannot be read"}]
	}`, string(stored))

	none, err := json.Marshal(&JobReport{SuccessfulTableReports: []*TableEntry{}})
	require.NoError(t, err)
	require.JSONEq(t, `{"successfulTableReports":[]}`, string(none))
}

// An index stored without the optional members reads back with them at their zero value.
func Test_JobReport_ReadsAnIndexWithoutOptionalMembers(t *testing.T) {
	var read JobReport
	require.NoError(t, json.Unmarshal([]byte(`{"successfulTableReports":[{
		"tableSchema": "public", "tableName": "users",
		"reportKey": {"jobRunId": "run-0", "externalId": "public.users--table-pii-report", "accountId": "account-1"},
		"scanFingerprint": "abc"
	}]}`), &read))
	require.Len(t, read.SuccessfulTableReports, 1)
	entry := read.SuccessfulTableReports[0]
	require.Equal(t, "public", entry.TableSchema)
	require.Equal(t, "users", entry.TableName)
	require.Equal(t, "abc", entry.ScanFingerprint)
	require.Equal(t, "run-0", entry.ReportKey.GetJobRunId())
	require.False(t, entry.Incomplete)
	require.Nil(t, read.FailedTables)
}
