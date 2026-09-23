package v1alpha1_jobservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReconcileJobMappings brings a job's mappings in step with the source a run read.
//
// The run decides what changes — it read the source, and the job's strategy for new columns
// tells it how to map one — and this applies the change to the job as it stands now, under a
// lock on its row. Between the run reading the job and this call, somebody may have mapped a
// new column by hand: their mapping stays. Applied twice, as Temporal may retry the run's
// activity, the call changes nothing the second time.
func (s *Service) ReconcileJobMappings(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ReconcileJobMappingsRequest],
) (*connect.Response[mgmtv1alpha1.ReconcileJobMappingsResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()),
		rbac.JobAction_Edit,
	); err != nil {
		return nil, err
	}
	// Only a run reconciles: it is the one that read the source.
	if s.cfg.IsHusonymCloud && !user.IsWorkerApiKey() {
		return nil, husonymerrors.NewUnauthenticated(
			"must provide valid authentication credentials for this endpoint",
		)
	}

	jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	var result *reconciledMappings
	if err := s.db.WithTx(ctx, nil, func(dbtx husonymdb.BaseDBTX) error {
		job, err := s.db.Q.GetJobForUpdate(ctx, dbtx, db_queries.GetJobForUpdateParams{
			ID:        jobUuid,
			AccountID: accountUuid,
		})
		if err != nil && husonymdb.IsNoRows(err) {
			return husonymerrors.NewNotFound("unable to find job")
		} else if err != nil {
			return err
		}

		result, err = reconcileMappings(job.Mappings, req.Msg.GetAdded(), req.Msg.GetRemoved())
		if err != nil {
			return err
		}
		if len(result.added) > 0 || len(result.removed) > 0 {
			if err := s.db.Q.SetJobMappingsFromRun(ctx, dbtx, db_queries.SetJobMappingsFromRunParams{
				Mappings: result.mappings,
				ID:       jobUuid,
			}); err != nil {
				return err
			}
		}

		previous, err := s.db.Q.GetJobSourceColumns(ctx, dbtx, jobUuid)
		if err != nil {
			return err
		}
		entries, err := journalEntries(result, previousTypes(previous), req.Msg.GetColumns())
		if err != nil {
			return err
		}
		// A run that read no columns (a source that is not SQL) leaves the last ones in place.
		if len(req.Msg.GetColumns()) > 0 {
			if err := s.replaceJobSourceColumns(ctx, dbtx, jobUuid, req.Msg.GetColumns()); err != nil {
				return err
			}
		}
		if !req.Msg.GetRecordChanges() {
			return nil
		}
		for _, entry := range entries {
			if err := s.db.Q.InsertJobMappingChange(ctx, dbtx, db_queries.InsertJobMappingChangeParams{
				AccountID:        accountUuid,
				JobID:            jobUuid,
				JobRunID:         req.Msg.GetJobRunId(),
				TableSchema:      entry.column.schema,
				TableName:        entry.column.table,
				ColumnName:       entry.column.column,
				Kind:             entry.kind,
				Transformer:      entry.transformer,
				DataType:         entry.dataType,
				PreviousDataType: entry.previousDataType,
				PiiCategory:      entry.piiCategory,
			}); err != nil {
				return fmt.Errorf("unable to record a change of the job's mappings: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.ReconcileJobMappingsResponse{
		Added:   result.added,
		Removed: result.removed,
	}), nil
}

func (s *Service) replaceJobSourceColumns(
	ctx context.Context,
	dbtx husonymdb.BaseDBTX,
	jobUuid pgtype.UUID,
	columns []*mgmtv1alpha1.JobSourceColumn,
) error {
	if err := s.db.Q.DeleteJobSourceColumns(ctx, dbtx, jobUuid); err != nil {
		return err
	}
	params := db_queries.InsertJobSourceColumnsParams{JobId: jobUuid}
	for _, c := range columns {
		params.Schemas = append(params.Schemas, c.GetColumn().GetSchema())
		params.Tables = append(params.Tables, c.GetColumn().GetTable())
		params.Columns = append(params.Columns, c.GetColumn().GetColumn())
		params.DataTypes = append(params.DataTypes, c.GetDataType())
	}
	return s.db.Q.InsertJobSourceColumns(ctx, dbtx, params)
}

// columnRef names a column of a job's source.
type columnRef struct{ schema, table, column string }

func previousTypes(rows []db_queries.HusonymApiJobSourceColumn) map[columnRef]string {
	out := make(map[columnRef]string, len(rows))
	for i := range rows {
		out[columnRef{rows[i].TableSchema, rows[i].TableName, rows[i].ColumnName}] = rows[i].DataType
	}
	return out
}

const (
	changeAdded       = "added"
	changeRemoved     = "removed"
	changeTypeChanged = "type_changed"
)

// journalEntry is one change a run made to a job's mappings, as the journal records it.
type journalEntry struct {
	column           columnRef
	kind             string
	transformer      *pg_models.JobMappingTransformerModel
	dataType         string
	previousDataType string
	piiCategory      string
}

// journalEntries describes what the reconciliation changed: the columns it mapped, the mappings it
// removed, and the mapped columns whose type is not the one the previous run saw. Only what was
// actually applied: a column mapped by hand in the meantime is not the run's change.
func journalEntries(
	result *reconciledMappings,
	previous map[columnRef]string,
	columns []*mgmtv1alpha1.JobSourceColumn,
) ([]journalEntry, error) {
	current := make(map[columnRef]string, len(columns))
	for _, c := range columns {
		current[columnRef{c.GetColumn().GetSchema(), c.GetColumn().GetTable(), c.GetColumn().GetColumn()}] = c.GetDataType()
	}

	entries := []journalEntry{}
	added := map[columnRef]struct{}{}
	for _, m := range result.added {
		ref := columnRef{m.GetSchema(), m.GetTable(), m.GetColumn()}
		added[ref] = struct{}{}
		entry, err := newJournalEntry(ref, changeAdded, m.GetTransformer(), current[ref], "")
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	for _, m := range result.removed {
		ref := columnRef{m.GetSchema(), m.GetTable(), m.GetColumn()}
		entry, err := newJournalEntry(ref, changeRemoved, m.GetTransformer(), previous[ref], "")
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	for _, m := range result.mappings {
		ref := columnRef{m.Schema, m.Table, m.Column}
		if _, ok := added[ref]; ok {
			continue
		}
		was, seen := previous[ref]
		now, read := current[ref]
		if !seen || !read || was == now {
			continue
		}
		dto, err := m.ToDto()
		if err != nil {
			return nil, err
		}
		entry, err := newJournalEntry(ref, changeTypeChanged, dto.GetTransformer(), now, was)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func newJournalEntry(
	ref columnRef,
	kind string,
	transformer *mgmtv1alpha1.JobMappingTransformer,
	dataType, previousDataType string,
) (journalEntry, error) {
	stored := &pg_models.JobMappingTransformerModel{}
	if err := stored.FromTransformerDto(transformer); err != nil {
		return journalEntry{}, err
	}
	category, _ := job_util.LooksSensitive(ref.column, dataType)
	return journalEntry{
		column:           ref,
		kind:             kind,
		transformer:      stored,
		dataType:         dataType,
		previousDataType: previousDataType,
		piiCategory:      category,
	}, nil
}

type reconciledMappings struct {
	// The job's mappings once reconciled
	mappings []*pg_models.JobMapping
	// What actually changed
	added, removed []*mgmtv1alpha1.JobMapping
}

// reconcileMappings removes the mappings of the removed columns, then adds a mapping for each
// added column the job does not map yet. The order of the job's other mappings is kept.
func reconcileMappings(
	stored []*pg_models.JobMapping,
	added []*mgmtv1alpha1.JobMapping,
	removed []*mgmtv1alpha1.JobColumn,
) (*reconciledMappings, error) {
	gone := make(map[columnRef]struct{}, len(removed))
	for _, column := range removed {
		gone[columnRef{column.GetSchema(), column.GetTable(), column.GetColumn()}] = struct{}{}
	}

	result := &reconciledMappings{mappings: make([]*pg_models.JobMapping, 0, len(stored)+len(added))}
	mapped := make(map[columnRef]struct{}, len(stored))
	for _, mapping := range stored {
		k := columnRef{mapping.Schema, mapping.Table, mapping.Column}
		if _, ok := gone[k]; ok {
			dto, err := mapping.ToDto()
			if err != nil {
				return nil, err
			}
			result.removed = append(result.removed, dto)
			continue
		}
		mapped[k] = struct{}{}
		result.mappings = append(result.mappings, mapping)
	}

	for _, mapping := range added {
		// A mapping with no transformer would read as a column the job maps and does nothing to.
		if mapping.GetTransformer().GetConfig().GetConfig() == nil {
			return nil, husonymerrors.NewBadRequest(fmt.Sprintf(
				"no transformer given for %s.%s.%s", mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn(),
			))
		}
		k := columnRef{mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn()}
		if _, ok := mapped[k]; ok {
			continue
		}
		if _, ok := gone[k]; ok {
			continue
		}
		stored := &pg_models.JobMapping{}
		if err := stored.FromDto(mapping); err != nil {
			return nil, err
		}
		mapped[k] = struct{}{}
		result.mappings = append(result.mappings, stored)
		result.added = append(result.added, mapping)
	}
	return result, nil
}
