package piidetect

import (
	"bytes"
	"context"
	"encoding/gob"
	"slices"
	"strings"
	"unicode"

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
	// sendable are the columns whose values may be shown: those of the catalogue whose
	// type is not binary.
	sendable map[string]bool
	values   map[string][]string
}

// newValuePicker returns a picker for the columns of a table on an engine. A column of
// a binary type is left out by its type in the catalogue, whatever the driver returns
// for it: the blobs of MySQL and the image and rowversion of SQL Server arrive as texts.
func newValuePicker(engine string, columns []*ColumnData) *valuePicker {
	picker := &valuePicker{sendable: map[string]bool{}, values: map[string][]string{}}
	for _, column := range columns {
		if column != nil && !binaryType(engine, column.DataType) {
			picker.sendable[column.Column] = true
		}
	}
	return picker
}

func (p *valuePicker) add(row map[string]any) {
	for column, value := range row {
		picked := p.values[column]
		if !p.sendable[column] || len(picked) >= maxValues {
			continue
		}
		// A value that has no text form is not shown: among them a text that is not valid
		// UTF-8, which is bytes whatever its column says.
		text, ok := profile.TextOf(value)
		if !ok || strings.ContainsFunc(text, unprintable) {
			continue
		}
		text = cutValue(strings.TrimSpace(text))
		if text == "" || slices.Contains(picked, text) {
			continue
		}
		p.values[column] = append(picked, text)
	}
}

// unprintable tells whether a character is a control character other than a tab and the
// ends of a line. A text that holds one is bytes read as text, and is not shown.
func unprintable(r rune) bool {
	return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r'
}

// The engines whose tables are scanned.
const (
	enginePostgres = "postgres"
	engineMysql    = "mysql"
	engineMssql    = "mssql"
)

// engineOf names the engine of a connection, "" for one whose tables are not scanned.
func engineOf(connection *mgmtv1alpha1.Connection) string {
	switch connection.GetConnectionConfig().GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig:
		return enginePostgres
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig:
		return engineMysql
	case *mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return engineMssql
	}
	return ""
}

// The types whose values are bytes, by the name each catalogue gives them: bytea and the
// bit strings of PostgreSQL; the binary, blob, bit and spatial types of MySQL; binary,
// varbinary, image, rowversion and the types SQL Server stores serialized. A sql_variant
// of SQL Server holds a value of any type, bytes among them.
//
// A domain is named by its own name in the catalogue, not by its base type: a domain over
// one of these types is not found here, and is told by its values (see profile.TextOf).
var binaryTypes = map[string]bool{
	"bytea": true, "bit": true, "bit varying": true, "varbit": true,
	"binary": true, "varbinary": true, "tinyblob": true, "blob": true, "mediumblob": true, "longblob": true,
	"image": true, "rowversion": true, "hierarchyid": true, "sql_variant": true,
	"geometry": true, "geography": true, "point": true, "linestring": true, "polygon": true,
	"multipoint": true, "multilinestring": true, "multipolygon": true, "geometrycollection": true,
	"geomcollection": true,
}

// binaryType tells whether a type of the catalogue of an engine holds bytes. The name is
// read without its length and without the marks of an array. On SQL Server, timestamp is
// the earlier name of rowversion; elsewhere it is a moment.
func binaryType(engine, dataType string) bool {
	name := strings.ToLower(strings.TrimSpace(dataType))
	if open := strings.IndexByte(name, '('); open >= 0 {
		closing := strings.IndexByte(name, ')')
		if closing < open {
			closing = len(name) - 1
		}
		name = name[:open] + name[closing+1:]
	}
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(name), "[]"), "_"))
	return binaryTypes[name] || (engine == engineMssql && name == "timestamp")
}

// cutValue cuts a value to maxValueLength characters, the cut marked by its last one.
func cutValue(text string) string {
	if head := profile.FirstRunes(text, maxValueLength); head == text {
		return text
	}
	return profile.FirstRunes(text, maxValueLength-1) + cutMark
}
