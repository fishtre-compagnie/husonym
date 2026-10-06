// Package evaluation measures how well the PII detection finds personal data, on a
// labeled data set, through the code a run goes through: the sampled rows, the profiles,
// the rules and the model.
//
// The data set is made up. Its values were generated: names are common first names and
// surnames paired at random, identifiers and account numbers are random, emails use the
// reserved example domains, telephone numbers use ranges that are not assigned, IP
// addresses come from the documentation ranges. It holds no data of a customer.
//
// The detection by rules is measured on every run of the tests and held to a baseline:
// see the tests. The detection by a model is measured on demand, and so is the content
// analysis of free text, which takes an analyzer.
package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dataset is the labeled tables of one language.
type Dataset struct {
	Language string  `json:"language"`
	Tables   []Table `json:"tables"`
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Column is a column of a table of the data set: its name, its SQL type, its values, one
// per row, and the category it is expected to be found under, or None.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Expected string `json:"expected"`
	Values   []any  `json:"values"`
	// Persons are the positions of the values that name a person, in a column of free
	// text: the values that make the column personal.
	Persons []int `json:"persons,omitempty"`
}

// Load reads the data sets of a directory, one file per language, in the order of their
// languages.
func Load(dir string) ([]Dataset, error) {
	return load(dir, "??.json")
}

// LoadFreeText reads the data sets of free text of a directory, one file per language, in
// the order of their languages. Each holds one table of columns of sentences: some name
// persons in a few of their values, the others hold what business text holds and no
// person.
func LoadFreeText(dir string) ([]Dataset, error) {
	return load(dir, "free-text-??.json")
}

func load(dir, pattern string) ([]Dataset, error) {
	files, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	datasets := make([]Dataset, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var dataset Dataset
		if err := json.Unmarshal(content, &dataset); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		for _, table := range dataset.Tables {
			for _, column := range table.Columns {
				if len(column.Values) != len(table.Columns[0].Values) {
					return nil, fmt.Errorf("%s: the columns of %s do not hold as many values", file, table.Name)
				}
			}
		}
		datasets = append(datasets, dataset)
	}
	return datasets, nil
}

// Rows returns the rows of the table, each value as the record mapper of a database
// returns it for the type of its column: a whole number for an integer column, a moment
// for a date or a timestamp column, and what the file holds otherwise.
func (t Table) Rows() []map[string]any {
	if len(t.Columns) == 0 {
		return nil
	}
	rows := make([]map[string]any, len(t.Columns[0].Values))
	for i := range rows {
		rows[i] = make(map[string]any, len(t.Columns))
		for _, column := range t.Columns {
			rows[i][column.Name] = typed(column.Type, column.Values[i])
		}
	}
	return rows
}

func typed(sqlType string, value any) any {
	switch v := value.(type) {
	case float64:
		if strings.Contains(sqlType, "int") && v == math.Trunc(v) {
			return int64(v)
		}
	case string:
		switch sqlType {
		case "date":
			if day, err := time.Parse(time.DateOnly, v); err == nil {
				return day
			}
		case "timestamp":
			if moment, err := time.Parse(time.RFC3339, v); err == nil {
				return moment
			}
		}
	}
	return value
}
