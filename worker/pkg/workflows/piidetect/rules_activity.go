package piidetect

import (
	"context"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/rules"
)

type DetectPiiRegexRequest struct {
	ColumnData []*ColumnData
}

type DetectPiiRegexResponse struct {
	// PiiColumns holds the category of each column the rules found personal data in.
	PiiColumns map[string]report.Category
	// Evidence says, for each of them, what the finding rests on.
	Evidence map[string]string `json:",omitempty"`
}

// DetectPiiRegex asks the rules about each column: its name, its type and, when its
// values were sampled, their profile. It is a computation: it reads nothing.
func (a *Activities) DetectPiiRegex(_ context.Context, req *DetectPiiRegexRequest) (*DetectPiiRegexResponse, error) {
	response := &DetectPiiRegexResponse{PiiColumns: map[string]report.Category{}}
	for _, column := range req.ColumnData {
		if column == nil {
			continue
		}
		finding, found := rules.Find(column.Column, column.DataType, column.Profile)
		if !found {
			continue
		}
		response.PiiColumns[column.Column] = finding.Category
		if response.Evidence == nil {
			response.Evidence = map[string]string{}
		}
		response.Evidence[column.Column] = finding.Evidence
	}
	return response, nil
}
