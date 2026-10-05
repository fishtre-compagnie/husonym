package piidetect

import (
	"context"
	"errors"
	"fmt"

	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/rules"
	"go.temporal.io/sdk/activity"
)

// Past this number of columns the profiles of a table are returned without their
// shapes, so that the payload they travel in stays well under what a payload may weigh.
const maxColumnsWithShapes = 1000

type GetColumnDataRequest struct {
	ConnectionId string
	TableSchema  string
	TableName    string
	// Sample asks for rows of the table to be read and profiled.
	Sample bool `json:",omitempty"`
}

type ColumnData struct {
	Column     string
	DataType   string
	IsNullable bool
	Comment    *string
	// Profile is what is kept of the sampled values of the column; nil when no row was
	// read.
	Profile *profile.Profile `json:",omitempty"`
}

type GetColumnDataResponse struct {
	ColumnData  []*ColumnData
	SampledRows int `json:",omitempty"`
}

// GetColumnData reads the columns of a table from the catalogue of its database and, when
// asked, profiles a sample of its rows. The rows are read one at a time and nothing of
// them leaves the activity but the profiles.
//
// A sample that cannot be read is not a table that cannot be scanned: the columns are
// then returned without profile, and the table is scanned on its names and types.
func (a *Activities) GetColumnData(ctx context.Context, req *GetColumnDataRequest) (*GetColumnDataResponse, error) {
	logger := activity.GetLogger(ctx)

	connection, err := a.connection(ctx, req.ConnectionId)
	if err != nil {
		return nil, fmt.Errorf("the connection cannot be read: %w", err)
	}
	if kind, ok := scannableConnection(connection); !ok {
		return nil, errUnsupportedSource(kind)
	}
	data, err := a.data.NewDataConnection(temporallogger.NewSlogger(logger), connection)
	if err != nil {
		return nil, fmt.Errorf("the source cannot be opened: %w", err)
	}
	catalogue, err := data.GetTableSchema(ctx, req.TableSchema, req.TableName)
	if errors.Is(err, errors.ErrUnsupported) {
		return nil, errUnsupportedSource("of a kind whose columns cannot be read")
	}
	if err != nil {
		return nil, fmt.Errorf("the columns of the table cannot be read: %w", err)
	}

	response := &GetColumnDataResponse{ColumnData: make([]*ColumnData, 0, len(catalogue))}
	for _, column := range catalogue {
		response.ColumnData = append(response.ColumnData, &ColumnData{
			Column:     column.GetColumn(),
			DataType:   column.GetDataType(),
			IsNullable: column.GetIsNullable() == "YES",
		})
	}
	if !req.Sample || len(catalogue) == 0 {
		return response, nil
	}

	profiles := profile.NewTable(rules.Detectors())
	stream, err := a.sample(ctx, data, req.TableSchema, req.TableName, profiles.Add)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The error of a row that could not be read may quote the row: it is not logged.
		logger.Warn(
			"the rows of the table could not be sampled: it is scanned on the names and types of its columns",
			"tableSchema", req.TableSchema, "tableName", req.TableName,
			"timedOut", errors.Is(err, context.DeadlineExceeded), "rowsRead", stream.rows,
		)
		return response, nil
	}
	if len(stream.undecodable) > 0 {
		logger.Warn(
			"sampled rows could not be decoded and were left out",
			"tableSchema", req.TableSchema, "tableName", req.TableName, "positions", stream.undecodable,
		)
	}

	response.SampledRows = profiles.Rows()
	for _, column := range response.ColumnData {
		column.Profile = profiles.Profile(column.Column)
		if len(response.ColumnData) > maxColumnsWithShapes {
			column.Profile = column.Profile.WithoutShapes()
		}
	}
	return response, nil
}
