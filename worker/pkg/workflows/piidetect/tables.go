package piidetect

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/rules"
	"go.temporal.io/sdk/activity"
)

type IncrementalConfig struct {
	// LastWorkflowId is the id of the run whose index says which tables are unchanged.
	LastWorkflowId string
}

type GetTablesToPiiScanRequest struct {
	AccountId          string
	JobId              string
	SourceConnectionId string
	Filter             *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter
	IncrementalConfig  *IncrementalConfig

	// How the job scans a table: each enters the fingerprint of the tables.
	Sampling   bool   `json:",omitempty"`
	UserPrompt string `json:",omitempty"`
	ModelInput string `json:",omitempty"`
	// MarksIncomplete says that the caller records, in the index of its run, the tables
	// that were scanned without the model. It enters the fingerprint too: an entry that
	// a caller which does not record them left in an index has another fingerprint, so
	// that a table whose model failed there is never taken for scanned here.
	MarksIncomplete bool `json:",omitempty"`
}

type TableToScan struct {
	Schema      string
	Table       string
	Fingerprint string
}

type GetTablesToPiiScanResponse struct {
	Tables []TableToScan
	// PreviousReports are the entries of the earlier run for the tables that still exist
	// and that the job still scans.
	PreviousReports []*report.TableEntry
}

// GetTablesToPiiScan lists the tables of the source that the job scans, each with the
// fingerprint of how it will be scanned. An incremental run leaves out the tables the
// earlier run scanned whole under the same fingerprint.
func (a *Activities) GetTablesToPiiScan(
	ctx context.Context,
	req *GetTablesToPiiScanRequest,
) (*GetTablesToPiiScanResponse, error) {
	logger := activity.GetLogger(ctx)

	connection, err := a.connection(ctx, req.SourceConnectionId)
	if err != nil {
		return nil, fmt.Errorf("the source connection cannot be read: %w", err)
	}
	if kind, ok := scannableConnection(connection); !ok {
		return nil, errUnsupportedSource(kind)
	}
	data, err := a.data.NewDataConnection(temporallogger.NewSlogger(logger), connection)
	if err != nil {
		return nil, fmt.Errorf("the source cannot be opened: %w", err)
	}
	columns, err := data.GetSchema(ctx, &mgmtv1alpha1.ConnectionSchemaConfig{})
	if errors.Is(err, errors.ErrUnsupported) {
		return nil, errUnsupportedSource("of a kind whose tables cannot be listed")
	}
	if err != nil {
		return nil, fmt.Errorf("the tables of the source cannot be listed: %w", err)
	}

	// A table is listed once, from its columns. A row of the catalogue that names no
	// table is not one.
	type tableName struct{ schema, table string }
	byTable := map[tableName][]*mgmtv1alpha1.DatabaseColumn{}
	for _, column := range columns {
		if column.GetTable() == "" {
			continue
		}
		name := tableName{column.GetSchema(), column.GetTable()}
		byTable[name] = append(byTable[name], column)
	}
	names := make([]tableName, 0, len(byTable))
	for name := range byTable {
		if scans(req.Filter, name.schema, name.table) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b tableName) int {
		return cmp.Or(cmp.Compare(a.schema, b.schema), cmp.Compare(a.table, b.table))
	})

	var earlier []*report.TableEntry
	if req.IncrementalConfig != nil {
		earlier, err = a.earlierEntries(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	earlierByTable := make(map[tableName]*report.TableEntry, len(earlier))
	for _, entry := range earlier {
		if entry != nil {
			earlierByTable[tableName{entry.TableSchema, entry.TableName}] = entry
		}
	}

	response := &GetTablesToPiiScanResponse{Tables: []TableToScan{}}
	for _, name := range names {
		fingerprint := a.fingerprint(req, name.schema, name.table, byTable[name])
		entry, known := earlierByTable[name]
		if known {
			response.PreviousReports = append(response.PreviousReports, entry)
		}
		if known && entry.ScanFingerprint == fingerprint && !entry.Incomplete {
			continue
		}
		response.Tables = append(response.Tables, TableToScan{
			Schema: name.schema, Table: name.table, Fingerprint: fingerprint,
		})
	}
	logger.Debug("listed the tables to scan", "tables", len(response.Tables), "unchanged", len(names)-len(response.Tables))
	return response, nil
}

// scans tells whether the filter of the job keeps a table. Names are compared as they
// are written. A filter that sets no mode keeps nothing.
func scans(filter *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter, schema, table string) bool {
	if filter == nil {
		return true
	}
	switch mode := filter.GetMode().(type) {
	case *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll:
		return true
	case *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include:
		return namesTable(mode.Include, schema, table)
	case *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Exclude:
		return !namesTable(mode.Exclude, schema, table)
	}
	return false
}

// namesTable tells whether the patterns name a table: by its schema, or by itself.
func namesTable(patterns *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns, schema, table string) bool {
	if slices.Contains(patterns.GetSchemas(), schema) {
		return true
	}
	return slices.ContainsFunc(
		patterns.GetTables(),
		func(named *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableIdentifier) bool {
			return named.GetSchema() == schema && named.GetTable() == table
		},
	)
}

// earlierEntries reads the index of the run an incremental run starts from. An index
// that is not there means that nothing is known of the earlier run.
func (a *Activities) earlierEntries(ctx context.Context, req *GetTablesToPiiScanRequest) ([]*report.TableEntry, error) {
	stored, err := a.jobs.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			AccountId:  req.AccountId,
			JobRunId:   req.IncrementalConfig.LastWorkflowId,
			ExternalId: report.JobReportExternalId(req.JobId),
		},
	}))
	if connect.CodeOf(err) == connect.CodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the index of the earlier run cannot be read: %w", err)
	}
	var index report.JobReport
	if err := json.Unmarshal(stored.Msg.GetValue(), &index); err != nil {
		return nil, fmt.Errorf("the index of the earlier run cannot be decoded: %w", err)
	}
	return index.SuccessfulTableReports, nil
}

// fingerprintVersion names the way a fingerprint is computed. An index whose
// fingerprints were computed another way matches none: its tables are scanned again.
const fingerprintVersion = "v3"

// fingerprint identifies how a table is scanned: its columns with their types, and what
// of the job and of the worker changes what a scan finds. A table whose fingerprint is
// the one of the earlier run would be found the same, and is not scanned again.
//
// Every part is followed by a zero byte, so that two lists of parts never write the same
// bytes.
func (a *Activities) fingerprint(
	req *GetTablesToPiiScanRequest,
	schema, table string,
	columns []*mgmtv1alpha1.DatabaseColumn,
) string {
	sorted := slices.Clone(columns)
	slices.SortFunc(sorted, func(a, b *mgmtv1alpha1.DatabaseColumn) int {
		return cmp.Compare(a.GetColumn(), b.GetColumn())
	})

	hash := sha256.New()
	part := func(value string) {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	part(fingerprintVersion)
	part(schema)
	part(table)
	for _, column := range sorted {
		part(column.GetColumn())
		part(column.GetDataType())
	}
	part(strconv.FormatBool(req.Sampling))
	part(req.ModelInput)
	part(req.UserPrompt)
	part(a.modelName())
	part(rules.Version)
	part(strconv.FormatBool(req.MarksIncomplete))
	return hex.EncodeToString(hash.Sum(nil))
}
