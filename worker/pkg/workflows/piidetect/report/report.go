// Package report holds what a PII detection run stores in the run contexts of the API,
// and what the API reads back: the index of a run and the report of each table.
//
// The keys and the JSON member names are those of the stored rows: a row written by one
// version of the worker is read by another version of the API. A member is only ever
// added, and an added member is left out when it is empty.
package report

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// What the model was given about a table, as stored in Scan.Input.
const (
	InputNames    = "names"
	InputProfiles = "profiles"
	InputValues   = "values"
)

// What became of the model step of a table, as stored in Scan.ModelStatus.
const (
	ModelAnswered = "answered" // every column has a valid answer
	ModelPartial  = "partial"  // some columns have none
	ModelFailed   = "failed"   // the model could not be asked
	ModelNone     = "none"     // no model is configured
)

// JobReport is the index of a run, stored under JobReportExternalId(jobId).
type JobReport struct {
	// The tables of the report: those scanned by the run and those carried over from an
	// earlier run.
	SuccessfulTableReports []*TableEntry `json:"successfulTableReports"`
	// The tables the run could not scan.
	FailedTables []*FailedTable `json:"failedTables,omitempty"`
}

// TableEntry names the report of one table. Its key may name an earlier run.
type TableEntry struct {
	TableSchema     string                      `json:"tableSchema"`
	TableName       string                      `json:"tableName"`
	ReportKey       *mgmtv1alpha1.RunContextKey `json:"reportKey"`
	ScanFingerprint string                      `json:"scanFingerprint"`
	// Incomplete says that the model could not be asked about the table: its report holds
	// the findings of the rules only.
	Incomplete bool `json:"incomplete,omitempty"`
}

type FailedTable struct {
	TableSchema string `json:"tableSchema"`
	TableName   string `json:"tableName"`
	Reason      string `json:"reason"`
}

// TableReport is the report of a table, stored under TableReportExternalId(schema, table).
type TableReport struct {
	TableSchema    string         `json:"table_schema"`
	TableName      string         `json:"table_name"`
	ColumnReports  []ColumnReport `json:"column_reports"`
	ScannedColumns []string       `json:"scanned_columns,omitempty"`
	Scan           *Scan          `json:"scan,omitempty"`
}

type ColumnReport struct {
	ColumnName string   `json:"column_name"`
	Report     Combined `json:"report"`
}

// Combined holds what each of the two detections found for a column, side by side. They
// are not merged into one verdict.
type Combined struct {
	Regex *RuleFinding  `json:"regex"`
	LLM   *ModelFinding `json:"llm"`
}

type RuleFinding struct {
	Category Category `json:"category"`
	// Evidence says what the finding rests on: "name", or "values:<format> <share>".
	Evidence string `json:"evidence,omitempty"`
}

type ModelFinding struct {
	Category   Category `json:"category"`
	Confidence float32  `json:"confidence"`
}

// Scan says how a table was scanned.
type Scan struct {
	SampledRows int `json:"sampled_rows"`
	// Input is what the model received for the table: InputNames, InputProfiles or
	// InputValues. Empty when the model was not asked.
	Input       string `json:"input,omitempty"`
	Model       string `json:"model,omitempty"`
	ModelStatus string `json:"model_status"`
	// Unanswered are the columns the model gave no valid answer for.
	Unanswered []string `json:"unanswered,omitempty"`
	// BelowThreshold are the answers of the model that its confidence kept out of the
	// report.
	BelowThreshold []Dismissed `json:"below_threshold,omitempty"`
}

type Dismissed struct {
	ColumnName string   `json:"column_name"`
	Category   Category `json:"category"`
	Confidence float32  `json:"confidence"`
}

// TableReportSuffix ends the external id of every table report: the API lists the table
// reports of a run by it.
const TableReportSuffix = "--table-pii-report"

func JobReportExternalId(jobId string) string {
	return jobId + "--job-pii-report"
}

func TableReportExternalId(schema, table string) string {
	return schema + "." + table + TableReportSuffix
}
