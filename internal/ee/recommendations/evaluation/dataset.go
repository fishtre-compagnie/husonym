// Package evaluation holds the versioned multilingual evaluation dataset used to
// measure the PII detection pipeline (stage-1 regex/dictionary and the LLM stage)
// and the helpers to compute precision/recall metrics against its ground truth.
//
// The package is only imported from tests; the embedded dataset never ends up in
// a production binary.
package evaluation

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed testdata/*.json
var datasetFiles embed.FS

// Dataset is a synthetic schema for one language, with ground-truth PII
// categories per column.
type Dataset struct {
	// Language is the ISO 639-1 code of the language the column names are written in.
	Language string   `json:"language"`
	Tables   []*Table `json:"tables"`
}

type Table struct {
	Schema  string    `json:"schema"`
	Table   string    `json:"table"`
	Columns []*Column `json:"columns"`
}

type Column struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	// ExpectedCategory is the ground-truth PII category; empty means the column is not PII.
	ExpectedCategory string `json:"expected_category"`
	// Stage1Expected marks columns that the metadata stage (column-name regex +
	// dictionary) must classify on its own, without the LLM stage.
	Stage1Expected bool `json:"stage1_expected,omitempty"`
	// Samples are synthetic values, used by the PROFILE and RAW sampling modes
	// of the LLM evaluation. Never real data.
	Samples []string `json:"samples,omitempty"`
	// Note documents why the column is in the dataset (hard case, false friend…).
	Note string `json:"note,omitempty"`
}

// LoadDatasets parses every embedded dataset file, sorted by file name.
func LoadDatasets() ([]*Dataset, error) {
	entries, err := datasetFiles.ReadDir("testdata")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	datasets := make([]*Dataset, 0, len(entries))
	for _, entry := range entries {
		contents, err := datasetFiles.ReadFile("testdata/" + entry.Name())
		if err != nil {
			return nil, err
		}
		dataset := &Dataset{}
		if err := json.Unmarshal(contents, dataset); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		datasets = append(datasets, dataset)
	}
	return datasets, nil
}

// Validate checks the internal consistency of the dataset against the list of
// valid PII categories (callers pass the categories of the detection pipeline).
func (d *Dataset) Validate(validCategories []string) error {
	valid := map[string]struct{}{}
	for _, category := range validCategories {
		valid[category] = struct{}{}
	}

	if d.Language == "" {
		return fmt.Errorf("dataset has no language")
	}
	if len(d.Tables) == 0 {
		return fmt.Errorf("dataset %q has no tables", d.Language)
	}
	for _, table := range d.Tables {
		if table.Schema == "" || table.Table == "" {
			return fmt.Errorf("dataset %q: table with empty schema or name", d.Language)
		}
		if len(table.Columns) == 0 {
			return fmt.Errorf("dataset %q: table %s.%s has no columns", d.Language, table.Schema, table.Table)
		}
		for _, column := range table.Columns {
			key := ColumnKey(table.Schema, table.Table, column.Name)
			if column.Name == "" {
				return fmt.Errorf("dataset %q: table %s.%s has a column without a name", d.Language, table.Schema, table.Table)
			}
			if column.ExpectedCategory != "" {
				if _, ok := valid[column.ExpectedCategory]; !ok {
					return fmt.Errorf("dataset %q: column %s has unknown category %q", d.Language, key, column.ExpectedCategory)
				}
			}
			if column.Stage1Expected && column.ExpectedCategory == "" {
				return fmt.Errorf("dataset %q: column %s is stage1_expected but has no expected category", d.Language, key)
			}
		}
	}
	return nil
}

// ColumnKey builds the prediction-map key for a column.
func ColumnKey(schema, table, column string) string {
	return fmt.Sprintf("%s.%s.%s", schema, table, column)
}
