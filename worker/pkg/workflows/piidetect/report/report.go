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

// The detections a report rests on, as stored in Scan.Sources.
const (
	SourceRules = "rules"
	SourceModel = "model"
	// SourceAnalyzer is the analysis of the values of a column by the content analyzer.
	SourceAnalyzer = "analyzer"
)

// What became of the analyzer step of a table, as stored in Scan.AnalyzerStatus.
const (
	AnalyzerAnswered = "answered" // every column was analyzed
	AnalyzerPartial  = "partial"  // some columns were not analyzed
	AnalyzerFailed   = "failed"   // the analyzer could not be asked
	AnalyzerNone     = "none"     // no analyzer is configured
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
	// Incomplete says that the model or the analyzer could not be asked about the table:
	// its report holds the findings of the other detections only.
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

// Combined holds what each detection found for a column, side by side. They are not
// merged into one verdict.
type Combined struct {
	Regex *RuleFinding  `json:"regex"`
	LLM   *ModelFinding `json:"llm"`
	// Analyzer is what the content analyzer found in the values of the column.
	Analyzer *AnalyzerFinding `json:"analyzer,omitempty"`
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
	// Sources are the detections the report rests on: SourceRules alone when no model
	// was asked or when it could not be, SourceRules and SourceModel otherwise. A report
	// that rests on the rules alone finds no personal data under a neutral column name
	// unless the values have a format the rules check.
	Sources []string `json:"sources,omitempty"`
	// AnalyzerStatus is what became of the analyzer step: one of the Analyzer constants.
	// Empty when the table was scanned without a step for the analyzer.
	AnalyzerStatus string `json:"analyzer_status,omitempty"`
	// NotAnalyzed are the columns the analyzer did not analyze.
	NotAnalyzed []string `json:"not_analyzed,omitempty"`
}

// AnalyzerFinding is what the content analyzer found in the sampled values of a column.
type AnalyzerFinding struct {
	Category Category `json:"category"`
	// Entity is the kind of entity the analyzer recognized most in the values.
	Entity string `json:"entity,omitempty"`
	// Matches is the number of sampled values in which the entity was found.
	Matches int `json:"matches"`
	// Sampled is the number of values the analyzer was given.
	Sampled int `json:"sampled"`
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
