package v1alpha1_connectiondataservice

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ScanResults keeps the last content scan of each table of a connection, with the columns the
// table had then. The screen of a job reads them back instead of reading a sample of every
// table and sending it to the analyzer each time it is opened, and scans again only a table
// that changed since.
type ScanResults interface {
	Keep(
		ctx context.Context,
		connectionID, schema, table string,
		columns []*mgmtv1alpha1.PiiScannedColumn,
		result *mgmtv1alpha1.DetectPiiInConnectionDataResponse,
	) error
	// Read returns the kept results of the given tables, or of every table of the connection
	// when none is given. A table never scanned has none.
	Read(
		ctx context.Context,
		connectionID string,
		tables []*mgmtv1alpha1.PiiScanTable,
	) ([]*mgmtv1alpha1.PiiScanResult, error)
}

// NewScanResults keeps the scans in the database of the API.
func NewScanResults(db *husonymdb.HusonymDb) ScanResults {
	return &dbScanResults{db: db}
}

type dbScanResults struct {
	db *husonymdb.HusonymDb
}

// scannedColumn is a column as the columns of a kept scan hold it.
type scannedColumn struct {
	Column   string `json:"column"`
	DataType string `json:"dataType"`
}

func (r *dbScanResults) Keep(
	ctx context.Context,
	connectionID, schema, table string,
	columns []*mgmtv1alpha1.PiiScannedColumn,
	result *mgmtv1alpha1.DetectPiiInConnectionDataResponse,
) error {
	connectionUuid, err := husonymdb.ToUuid(connectionID)
	if err != nil {
		return err
	}
	kept := make([]scannedColumn, 0, len(columns))
	for _, c := range columns {
		kept = append(kept, scannedColumn{Column: c.GetColumn(), DataType: c.GetDataType()})
	}
	columnsJSON, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	resultJSON, err := protojson.Marshal(result)
	if err != nil {
		return err
	}
	return r.db.Q.UpsertPiiScanResult(ctx, r.db.Db, db_queries.UpsertPiiScanResultParams{
		ConnectionID: connectionUuid,
		SchemaName:   schema,
		TableName:    table,
		Columns:      columnsJSON,
		Result:       resultJSON,
	})
}

func (r *dbScanResults) Read(
	ctx context.Context,
	connectionID string,
	tables []*mgmtv1alpha1.PiiScanTable,
) ([]*mgmtv1alpha1.PiiScanResult, error) {
	connectionUuid, err := husonymdb.ToUuid(connectionID)
	if err != nil {
		return nil, err
	}
	var rows []db_queries.HusonymApiPiiScanResult
	if len(tables) == 0 {
		rows, err = r.db.Q.GetPiiScanResultsByConnection(ctx, r.db.Db, connectionUuid)
	} else {
		schemas := make([]string, 0, len(tables))
		names := make([]string, 0, len(tables))
		for _, t := range tables {
			schemas = append(schemas, t.GetSchema())
			names = append(names, t.GetTable())
		}
		rows, err = r.db.Q.GetPiiScanResultsByTables(ctx, r.db.Db, db_queries.GetPiiScanResultsByTablesParams{
			ConnectionID: connectionUuid,
			SchemaNames:  schemas,
			TableNames:   names,
		})
	}
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	}
	results := make([]*mgmtv1alpha1.PiiScanResult, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		result, err := scanResultFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("unable to read the kept scan of %s.%s: %w", row.SchemaName, row.TableName, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func scanResultFromRow(row *db_queries.HusonymApiPiiScanResult) (*mgmtv1alpha1.PiiScanResult, error) {
	var columns []scannedColumn
	if err := json.Unmarshal(row.Columns, &columns); err != nil {
		return nil, err
	}
	// A result kept by a later version may carry fields this one does not know.
	var scan mgmtv1alpha1.DetectPiiInConnectionDataResponse
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(row.Result, &scan); err != nil {
		return nil, err
	}
	result := &mgmtv1alpha1.PiiScanResult{
		Schema:     row.SchemaName,
		Table:      row.TableName,
		Columns:    make([]*mgmtv1alpha1.PiiScannedColumn, 0, len(columns)),
		Detections: scan.GetDetections(),
		Verdicts:   scan.GetVerdicts(),
	}
	for _, c := range columns {
		result.Columns = append(result.Columns, &mgmtv1alpha1.PiiScannedColumn{Column: c.Column, DataType: c.DataType})
	}
	if row.ScannedAt.Valid {
		result.ScannedAt = timestamppb.New(row.ScannedAt.Time)
	}
	return result, nil
}

// GetPiiScanResults returns the last content scan of the tables of a connection.
func (s *Service) GetPiiScanResults(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetPiiScanResultsRequest],
) (*connect.Response[mgmtv1alpha1.GetPiiScanResultsResponse], error) {
	// The connection service answering is what tells that the caller may see the connection.
	connResp, err := s.connectionService.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: req.Msg.GetConnectionId()}),
	)
	if err != nil {
		return nil, err
	}
	if err := userdata.FeatureRefusal(
		s.transformers.License, connResp.Msg.GetConnection().GetAccountId(), license.FeaturePiiDetection,
	); err != nil {
		return nil, err
	}
	if s.scanResults == nil {
		return connect.NewResponse(&mgmtv1alpha1.GetPiiScanResultsResponse{}), nil
	}
	results, err := s.scanResults.Read(ctx, req.Msg.GetConnectionId(), req.Msg.GetTables())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetPiiScanResultsResponse{
		Results: s.checkAgainstSource(ctx, connResp.Msg.GetConnection(), results),
	}), nil
}

// checkAgainstSource tells of each result whether it still describes its table: the columns the
// table has now are read the way the scan read them when it was kept (GetTableSchema). A result
// whose table has the same columns, of the same types, holds. One whose table changed since, or
// is gone, is returned without its detections, to be scanned again.
//
// The check is made here, and not by the screen against the schema it shows, because the two
// readers do not write the types alike: on MySQL the schema of the connection gives varchar where
// the table's own reader gives varchar(255). Compared across them, every table looked changed.
//
// When the columns cannot be read, the results are returned as they were kept, unverified. A
// failed check says nothing of the tables: taking it for a change had the screen scan every
// table of the job again, each time the source was slow to answer.
func (s *Service) checkAgainstSource(
	ctx context.Context,
	connection *mgmtv1alpha1.Connection,
	results []*mgmtv1alpha1.PiiScanResult,
) []*mgmtv1alpha1.PiiScanResult {
	if len(results) == 0 {
		return results
	}
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	unverified := func(err error) []*mgmtv1alpha1.PiiScanResult {
		logger.Warn(fmt.Sprintf("unable to check the kept content scans against the source: %v", err))
		for _, r := range results {
			r.Status = mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_UNVERIFIED
		}
		return results
	}
	dataconn, err := s.connectiondatabuilder.NewDataConnection(logger, connection)
	if err != nil {
		return unverified(err)
	}
	tables := make([]connectiondata.TableIdentifier, 0, len(results))
	for _, r := range results {
		tables = append(tables, connectiondata.TableIdentifier{Schema: r.GetSchema(), Table: r.GetTable()})
	}
	columns, err := dataconn.GetTablesSchema(ctx, tables)
	if err != nil {
		return unverified(err)
	}
	current := map[string][]*mgmtv1alpha1.PiiScannedColumn{}
	for _, c := range columns {
		key := c.GetSchema() + "." + c.GetTable()
		current[key] = append(current[key], &mgmtv1alpha1.PiiScannedColumn{
			Column: c.GetColumn(), DataType: c.GetDataType(),
		})
	}
	for _, r := range results {
		if sameColumns(r.GetColumns(), current[r.GetSchema()+"."+r.GetTable()]) {
			r.Status = mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_HOLDS
			continue
		}
		r.Status = mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_CHANGED
		r.Detections = nil
		r.Verdicts = nil
	}
	return results
}

// sameColumns tells whether a table has the columns a scan saw: the same names, each of the same
// type. A table whose columns are unknown is not taken for unchanged.
func sameColumns(scanned, current []*mgmtv1alpha1.PiiScannedColumn) bool {
	if len(current) == 0 || len(scanned) != len(current) {
		return false
	}
	types := make(map[string]string, len(scanned))
	for _, c := range scanned {
		types[c.GetColumn()] = c.GetDataType()
	}
	if len(types) != len(scanned) {
		return false
	}
	for _, c := range current {
		if t, ok := types[c.GetColumn()]; !ok || t != c.GetDataType() {
			return false
		}
	}
	return true
}

// keepScan keeps the scan of a whole table, whose columns are known. A scan of some columns
// only, or one in which the analyzer failed on a column, is not the table's result: kept, it
// would be read back as the table's and spare it the scan it still needs.
func (s *Service) keepScan(
	ctx context.Context,
	req *mgmtv1alpha1.DetectPiiInConnectionDataRequest,
	tableColumns []*mgmtv1alpha1.DatabaseColumn,
	notAnalyzed map[string]struct{},
	result *mgmtv1alpha1.DetectPiiInConnectionDataResponse,
) error {
	if s.scanResults == nil || len(req.GetColumns()) > 0 || len(tableColumns) == 0 || len(notAnalyzed) > 0 {
		return nil
	}
	columns := make([]*mgmtv1alpha1.PiiScannedColumn, 0, len(tableColumns))
	for _, c := range tableColumns {
		columns = append(columns, &mgmtv1alpha1.PiiScannedColumn{Column: c.GetColumn(), DataType: c.GetDataType()})
	}
	return s.scanResults.Keep(ctx, req.GetConnectionId(), req.GetSchema(), req.GetTable(), columns, result)
}
