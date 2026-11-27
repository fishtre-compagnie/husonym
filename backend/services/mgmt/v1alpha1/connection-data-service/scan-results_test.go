package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// storedScans hands back the results it holds.
type storedScans struct {
	results []*mgmtv1alpha1.PiiScanResult
}

func (s *storedScans) Keep(
	context.Context, string, string, string, []*mgmtv1alpha1.PiiScannedColumn, *mgmtv1alpha1.DetectPiiInConnectionDataResponse,
) error {
	return nil
}

func (s *storedScans) Read(
	context.Context, string, []*mgmtv1alpha1.PiiScanTable,
) ([]*mgmtv1alpha1.PiiScanResult, error) {
	return s.results, nil
}

func keptScanOf(table string, columns ...string) *mgmtv1alpha1.PiiScanResult {
	result := &mgmtv1alpha1.PiiScanResult{Schema: "shop", Table: table}
	for i := 0; i+1 < len(columns); i += 2 {
		result.Columns = append(result.Columns, &mgmtv1alpha1.PiiScannedColumn{Column: columns[i], DataType: columns[i+1]})
	}
	return result
}

func sourceColumn(table, column, dataType string) *mgmtv1alpha1.DatabaseColumn {
	return &mgmtv1alpha1.DatabaseColumn{Schema: "shop", Table: table, Column: column, DataType: dataType}
}

// scanResultsFrom asks the kept scans of a service whose source has the given columns, or fails to
// read them with readErr.
func scanResultsFrom(
	t *testing.T,
	kept []*mgmtv1alpha1.PiiScanResult,
	columns []*mgmtv1alpha1.DatabaseColumn,
	readErr error,
) []*mgmtv1alpha1.PiiScanResult {
	t.Helper()
	dataconn := connectiondata.NewMockConnectionDataService(t)
	dataconn.EXPECT().GetTablesSchema(mock.Anything, mock.Anything).Return(columns, readErr)
	builder := connectiondata.NewMockConnectionDataBuilder(t)
	builder.EXPECT().NewDataConnection(mock.Anything, mock.Anything).Return(dataconn, nil)
	connections := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
	connections.EXPECT().GetConnection(mock.Anything, mock.Anything).Return(
		connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: &mgmtv1alpha1.Connection{Id: "c1"}}), nil,
	)
	service := &Service{
		connectionService:     connections,
		connectiondatabuilder: builder,
		transformers:          Transformers{License: testutil.NewFakeEELicense(testutil.WithIsValid())},
		scanResults:           &storedScans{results: kept},
	}
	resp, err := service.GetPiiScanResults(t.Context(), connect.NewRequest(
		&mgmtv1alpha1.GetPiiScanResultsRequest{ConnectionId: "c1"},
	))
	require.NoError(t, err)
	return resp.Msg.GetResults()
}

// statuses gives the status of each result, by table.
func statuses(results []*mgmtv1alpha1.PiiScanResult) map[string]mgmtv1alpha1.PiiScanStatus {
	byTable := make(map[string]mgmtv1alpha1.PiiScanStatus, len(results))
	for _, r := range results {
		byTable[r.GetTable()] = r.GetStatus()
	}
	return byTable
}

func detectionOn(table string) *mgmtv1alpha1.ColumnPiiDetection {
	return &mgmtv1alpha1.ColumnPiiDetection{Schema: "shop", Table: table, Column: "email"}
}

// A kept scan holds while its table has the columns it saw, read the way the scan read them: on
// MySQL that is the whole column type, varchar(255) and not varchar. The screen used to compare it
// with the schema of the connection, which writes varchar, and scanned every table again on each
// visit.
func Test_GetPiiScanResults_KeepsTheScansOfUnchangedTables(t *testing.T) {
	customers := keptScanOf("customers", "email", "varchar(255)", "active", "tinyint(1)")
	customers.Detections = []*mgmtv1alpha1.ColumnPiiDetection{detectionOn("customers")}
	results := scanResultsFrom(t,
		[]*mgmtv1alpha1.PiiScanResult{customers, keptScanOf("orders", "id", "int unsigned")},
		[]*mgmtv1alpha1.DatabaseColumn{
			sourceColumn("customers", "active", "tinyint(1)"),
			sourceColumn("customers", "email", "varchar(255)"),
			sourceColumn("orders", "id", "int unsigned"),
		},
		nil,
	)

	require.Equal(t, map[string]mgmtv1alpha1.PiiScanStatus{
		"customers": mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_HOLDS,
		"orders":    mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_HOLDS,
	}, statuses(results))
	require.Len(t, results[0].GetDetections(), 1, "a scan that holds keeps what it found")
}

// A table whose columns changed since its scan is told changed, and its result carries no
// detection, to be scanned again: a column of another type, even of another length, one added,
// one removed, or the table gone.
func Test_GetPiiScanResults_TellsTheTablesThatChanged(t *testing.T) {
	kept := []*mgmtv1alpha1.PiiScanResult{
		keptScanOf("lengthened", "country", "varchar(2)"),
		keptScanOf("retyped", "active", "tinyint(1)"),
		keptScanOf("widened", "email", "varchar(255)"),
		keptScanOf("narrowed", "email", "varchar(255)", "phone", "varchar(20)"),
		keptScanOf("dropped", "email", "varchar(255)"),
		keptScanOf("unchanged", "email", "varchar(255)"),
	}
	for _, r := range kept {
		r.Detections = []*mgmtv1alpha1.ColumnPiiDetection{detectionOn(r.GetTable())}
	}
	results := scanResultsFrom(t, kept,
		[]*mgmtv1alpha1.DatabaseColumn{
			sourceColumn("lengthened", "country", "varchar(255)"),
			sourceColumn("retyped", "active", "tinyint(4)"),
			sourceColumn("widened", "email", "varchar(255)"),
			sourceColumn("widened", "phone", "varchar(20)"),
			sourceColumn("narrowed", "email", "varchar(255)"),
			sourceColumn("unchanged", "email", "varchar(255)"),
		},
		nil,
	)

	changed := mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_CHANGED
	require.Equal(t, map[string]mgmtv1alpha1.PiiScanStatus{
		"lengthened": changed,
		"retyped":    changed,
		"widened":    changed,
		"narrowed":   changed,
		"dropped":    changed,
		"unchanged":  mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_HOLDS,
	}, statuses(results))
	for _, r := range results {
		if r.GetStatus() == changed {
			require.Empty(t, r.GetDetections(), "%s: a changed table is not described by its old scan", r.GetTable())
		}
	}
}

// A kept scan that cannot be checked against the source is returned as it was kept, unverified:
// a failed check is no sign of a change, and taking it for one had the screen scan every table of
// the job again.
func Test_GetPiiScanResults_ReturnsTheScansUnverifiedWhenTheSourceCannotBeRead(t *testing.T) {
	customers := keptScanOf("customers", "email", "varchar(255)")
	customers.Detections = []*mgmtv1alpha1.ColumnPiiDetection{detectionOn("customers")}
	results := scanResultsFrom(t,
		[]*mgmtv1alpha1.PiiScanResult{customers},
		nil,
		errors.New("connection refused"),
	)

	require.Len(t, results, 1)
	require.Equal(t, mgmtv1alpha1.PiiScanStatus_PII_SCAN_STATUS_UNVERIFIED, results[0].GetStatus())
	require.Len(t, results[0].GetDetections(), 1)
}

func Test_sameColumns(t *testing.T) {
	col := func(name, dataType string) *mgmtv1alpha1.PiiScannedColumn {
		return &mgmtv1alpha1.PiiScannedColumn{Column: name, DataType: dataType}
	}
	a, b := col("a", "text"), col("b", "int")

	require.True(t, sameColumns([]*mgmtv1alpha1.PiiScannedColumn{a, b}, []*mgmtv1alpha1.PiiScannedColumn{b, a}), "order does not matter")
	require.False(t, sameColumns(nil, nil), "a table without columns is not taken for unchanged")
	require.False(t, sameColumns([]*mgmtv1alpha1.PiiScannedColumn{a, a}, []*mgmtv1alpha1.PiiScannedColumn{a, b}), "a column seen twice")
	require.False(t, sameColumns([]*mgmtv1alpha1.PiiScannedColumn{a}, []*mgmtv1alpha1.PiiScannedColumn{col("a", "varchar")}))
}
