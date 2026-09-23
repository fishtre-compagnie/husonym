package v1alpha1_connectiondataservice

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	javascript_draft "github.com/fishtre-compagnie/husonym/internal/javascript/draft"
)

// GetJavascriptDraftPrompt builds the prompt that gets a javascript rule drafted for one column.
//
// The work is not the wording — that lives in internal/javascript/draft — it is collecting the
// facts. A caller drafting a rule by hand, or an agent doing it for them, knows the column name
// and nothing else; the server knows the type, the length, whether a unique index covers the
// column alone, which parent it points at and who points at it. Those are exactly the facts a
// wrong draft turns out to have been missing.
//
// It reads the schema and the constraints. It never reads the column's values.
func (s *Service) GetJavascriptDraftPrompt(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetJavascriptDraftPromptRequest],
) (*connect.Response[mgmtv1alpha1.GetJavascriptDraftPromptResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	connResp, err := s.connectionService.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: req.Msg.GetConnectionId()}),
	)
	if err != nil {
		return nil, err
	}
	dataconn, err := s.connectiondatabuilder.NewDataConnection(logger, connResp.Msg.GetConnection())
	if err != nil {
		return nil, err
	}

	// One table, not the whole database. GetConnectionSchema would read every column of every
	// table to hand back the one asked for, and an agent drafting rules column by column would
	// pay that on every call.
	columns, err := dataconn.GetTableSchema(ctx, req.Msg.GetSchema(), req.Msg.GetTable())
	if err != nil {
		return nil, err
	}
	// GetConnectionSchema enriches what it returns; reading the table directly skips that, and
	// the detected category is what tells the model the column holds personal data.
	piidetect.Enrich(columns)

	column := findColumn(
		columns,
		req.Msg.GetSchema(),
		req.Msg.GetTable(),
		req.Msg.GetColumn(),
	)
	if column == nil {
		// Named rather than reported as an empty result: a caller that mistyped a column has no
		// other way to tell that from a column the server declined to describe.
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf(
			"no column %q in %s.%s",
			req.Msg.GetColumn(), req.Msg.GetSchema(), req.Msg.GetTable(),
		))
	}

	constraintsResp, err := s.GetConnectionTableConstraints(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionTableConstraintsRequest{
			ConnectionId: req.Msg.GetConnectionId(),
		}),
	)
	if err != nil {
		return nil, err
	}

	facts := javascript_draft.ColumnFacts{
		Schema:      req.Msg.GetSchema(),
		Table:       req.Msg.GetTable(),
		Column:      req.Msg.GetColumn(),
		DataType:    column.GetDataType(),
		MaxLength:   maxLengthOf(column),
		IsNullable:  isNullable(column.GetIsNullable()),
		IsGenerated: column.GetGeneratedType() != "",
		IsIdentity:  column.GetIdentityGeneration() != "",
		PiiCategory: column.GetDataCategory(),
		Mode:        draftMode(req.Msg.GetMode()),
		Engine:      draftEngine(req.Msg.GetEngine()),
	}

	table := sqlmanager_shared.SchemaTable{
		Schema: req.Msg.GetSchema(),
		Table:  req.Msg.GetTable(),
	}.String()
	facts.IsUnique = isUniqueAlone(constraintsResp.Msg, table, req.Msg.GetColumn())
	facts.ForeignKey = foreignKeyOf(constraintsResp.Msg, table, req.Msg.GetColumn())
	facts.ReferencedBy = countReferencesTo(constraintsResp.Msg, table, req.Msg.GetColumn())

	return connect.NewResponse(&mgmtv1alpha1.GetJavascriptDraftPromptResponse{
		Prompt: javascript_draft.BuildPrompt(&facts),
	}), nil
}

func findColumn(
	columns []*mgmtv1alpha1.DatabaseColumn,
	schema, table, column string,
) *mgmtv1alpha1.DatabaseColumn {
	for _, candidate := range columns {
		if candidate.GetSchema() == schema &&
			candidate.GetTable() == table &&
			candidate.GetColumn() == column {
			return candidate
		}
	}
	return nil
}

// isNullable reads the flag the way the drivers write it: postgres reports the information
// schema's "YES"/"NO", others a boolean rendered as text.
func isNullable(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}

// maxLengthOf reads the bound the schema reports, and nothing else.
//
// It used to parse it out of data_type, which cannot work: MySQL reports `varchar` there and
// keeps the length in column_type, so every MySQL column came back unbounded and the drafted
// rule was free to overflow it — the exact failure the prompt exists to prevent. The schema now
// carries character_maximum_length, already normalised to absent when the type bounds nothing.
func maxLengthOf(column *mgmtv1alpha1.DatabaseColumn) *int32 {
	length := column.GetCharacterMaximumLength()
	if length <= 0 {
		return nil
	}
	return &length
}

// isUniqueAlone is true only when a key covers this column and nothing else. A composite key
// constrains the combination, not the column, so a rule that repeats a value in it is fine —
// and telling a model otherwise would rule out the deterministic functions that are usually the
// right answer.
func isUniqueAlone(
	constraints *mgmtv1alpha1.GetConnectionTableConstraintsResponse,
	table, column string,
) bool {
	if primary, ok := constraints.GetPrimaryKeyConstraints()[table]; ok {
		if slices.Equal(primary.GetColumns(), []string{column}) {
			return true
		}
	}
	if unique, ok := constraints.GetUniqueConstraints()[table]; ok {
		for _, constraint := range unique.GetConstraints() {
			if slices.Equal(constraint.GetColumns(), []string{column}) {
				return true
			}
		}
	}
	if indexes, ok := constraints.GetUniqueIndexes()[table]; ok {
		for _, index := range indexes.GetIndexes() {
			if slices.Equal(index.GetColumns(), []string{column}) {
				return true
			}
		}
	}
	return false
}

// foreignKeyOf finds the parent this column points at. Columns and foreign_key.columns line up
// by position, which is what makes a composite key readable at all.
func foreignKeyOf(
	constraints *mgmtv1alpha1.GetConnectionTableConstraintsResponse,
	table, column string,
) *javascript_draft.ForeignKey {
	tables, ok := constraints.GetForeignKeyConstraints()[table]
	if !ok {
		return nil
	}
	for _, constraint := range tables.GetConstraints() {
		for idx, col := range constraint.GetColumns() {
			if col != column {
				continue
			}
			parentColumns := constraint.GetForeignKey().GetColumns()
			if idx >= len(parentColumns) {
				continue
			}
			parentSchema, parentTable := splitSchemaTable(constraint.GetForeignKey().GetTable())
			return &javascript_draft.ForeignKey{
				Schema: parentSchema,
				Table:  parentTable,
				Column: parentColumns[idx],
			}
		}
	}
	return nil
}

// countReferencesTo counts the foreign keys pointing at this column, from any table. One is
// enough to make the rule's determinism mandatory, but the number is worth showing: it says how
// much of the database a careless draft would break.
func countReferencesTo(
	constraints *mgmtv1alpha1.GetConnectionTableConstraintsResponse,
	table, column string,
) int32 {
	var count int32
	for _, tables := range constraints.GetForeignKeyConstraints() {
		for _, constraint := range tables.GetConstraints() {
			if constraint.GetForeignKey().GetTable() != table {
				continue
			}
			if slices.Contains(constraint.GetForeignKey().GetColumns(), column) {
				count++
			}
		}
	}
	return count
}

func splitSchemaTable(qualified string) (schema, table string) {
	schema, table = sqlmanager_shared.SplitTableKey(qualified)
	return schema, table
}

func draftMode(mode mgmtv1alpha1.JavascriptDraftMode) javascript_draft.Mode {
	if mode == mgmtv1alpha1.JavascriptDraftMode_JAVASCRIPT_DRAFT_MODE_GENERATE {
		return javascript_draft.ModeGenerate
	}
	return javascript_draft.ModeTransform
}

func draftEngine(engine mgmtv1alpha1.JavascriptDraftEngine) javascript_draft.Engine {
	if engine == mgmtv1alpha1.JavascriptDraftEngine_JAVASCRIPT_DRAFT_ENGINE_BENTHOS {
		return javascript_draft.EngineBenthos
	}
	return javascript_draft.EngineAthanor
}
