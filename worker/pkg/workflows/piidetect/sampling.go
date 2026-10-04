package piidetect

import (
	"bytes"
	"context"
	"encoding/gob"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
)

// rowStream receives the sampled rows of a table, one at a time, and hands each to read.
// It is the connectiondata.SampleDataStream of the activities: the rows are never held
// together, and a row is gone once read has returned.
type rowStream struct {
	read func(row map[string]any)

	rows int
	// undecodable holds the positions, from 1, of the first rows that could not be
	// decoded. A position is all that is ever said of such a row.
	undecodable []int
}

var _ connectiondata.SampleDataStream = (*rowStream)(nil)

// maxUndecodablePositions is how many positions of undecodable rows are kept.
const maxUndecodablePositions = 5

func (s *rowStream) Send(resp *mgmtv1alpha1.GetConnectionDataStreamResponse) error {
	s.rows++
	var row map[string]any
	if err := gob.NewDecoder(bytes.NewReader(resp.GetRowBytes())).Decode(&row); err != nil {
		if len(s.undecodable) < maxUndecodablePositions {
			s.undecodable = append(s.undecodable, s.rows)
		}
		return nil
	}
	s.read(row)
	return nil
}

// sample reads at most sampledRows rows of a table and hands each to read, within the
// time the sampling is given.
func (a *Activities) sample(
	ctx context.Context,
	data connectiondata.ConnectionDataService,
	schema, table string,
	read func(row map[string]any),
) (*rowStream, error) {
	ctx, cancel := context.WithTimeout(ctx, a.samplingTimeout)
	defer cancel()
	stream := &rowStream{read: read}
	return stream, data.SampleData(ctx, stream, schema, table, sampledRows)
}

const (
	// What a column may show of itself to the model, when the job sends values: this
	// many values, distinct, each of this many characters at most.
	maxValues      = 5
	maxValueLength = 64
	// cutMark ends a value that was cut. It is the last of its maxValueLength characters.
	cutMark = "…"
)

// valuePicker keeps, for each column, the first values that may be shown: not null, not
// blank, of a kind that has a text form, cut, and distinct once cut. It keeps nothing
// else of a row.
//
// The rows come in the random order of the sampling query, so that the first ones are a
// draw among the rows read, without a draw of its own.
type valuePicker struct {
	values map[string][]string
}

func newValuePicker() *valuePicker {
	return &valuePicker{values: map[string][]string{}}
}

func (p *valuePicker) add(row map[string]any) {
	for column, value := range row {
		picked := p.values[column]
		if len(picked) >= maxValues {
			continue
		}
		text, ok := profile.TextOf(value)
		if !ok {
			continue
		}
		text = cutValue(strings.TrimSpace(text))
		if text == "" || containsValue(picked, text) {
			continue
		}
		p.values[column] = append(picked, text)
	}
}

func containsValue(values []string, value string) bool {
	for _, known := range values {
		if known == value {
			return true
		}
	}
	return false
}

// cutValue cuts a value to maxValueLength characters, the cut marked by its last one.
func cutValue(text string) string {
	if head := profile.FirstRunes(text, maxValueLength); head == text {
		return text
	}
	return profile.FirstRunes(text, maxValueLength-1) + cutMark
}
