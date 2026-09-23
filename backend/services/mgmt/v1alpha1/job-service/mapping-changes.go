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
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetPendingMappingChanges returns what runs changed in the mappings of jobs under
// AutoMap & Review and nobody has reviewed yet, for one job or for the account.
func (s *Service) GetPendingMappingChanges(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetPendingMappingChangesRequest],
) (*connect.Response[mgmtv1alpha1.GetPendingMappingChangesResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	var rows []db_queries.HusonymApiJobMappingChange
	var jobs []db_queries.HusonymApiJob
	if req.Msg.JobId != nil {
		jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
		if err != nil {
			return nil, err
		}
		if err := user.EnforceJob(ctx, userdata.NewDbDomainEntity(accountUuid, jobUuid), rbac.JobAction_View); err != nil {
			return nil, err
		}
		rows, err = s.db.Q.GetPendingJobMappingChangesByJob(ctx, s.db.Db, db_queries.GetPendingJobMappingChangesByJobParams{
			AccountId: accountUuid,
			JobId:     jobUuid,
		})
		if err != nil {
			return nil, err
		}
		// The job is read to tell what its mappings say now, which only the changes need. A job
		// deleted while somebody had the review tab open takes its changes with it: the poll
		// answers an empty list rather than failing on a job that is gone.
		if len(rows) > 0 {
			job, err := s.db.Q.GetJobById(ctx, s.db.Db, jobUuid)
			if err != nil && husonymdb.IsNoRows(err) {
				return nil, husonymerrors.NewNotFound("job with that id does not exist")
			} else if err != nil {
				return nil, err
			}
			jobs = []db_queries.HusonymApiJob{job}
		}
	} else {
		if err := user.EnforceAccountAccess(ctx, req.Msg.GetAccountId()); err != nil {
			return nil, err
		}
		rows, err = s.db.Q.GetPendingJobMappingChangesByAccount(ctx, s.db.Db, accountUuid)
		if err != nil {
			return nil, err
		}
		rows = s.keepViewableChanges(ctx, user, accountUuid, rows)
		if len(rows) > 0 {
			jobs, err = s.db.Q.GetJobsByAccount(ctx, s.db.Db, accountUuid)
			if err != nil {
				return nil, err
			}
		}
	}

	changes, err := pendingChanges(rows, currentMappings(jobs))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetPendingMappingChangesResponse{Changes: changes}), nil
}

// ReviewMappingChanges marks changes of a job reviewed.
func (s *Service) ReviewMappingChanges(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ReviewMappingChangesRequest],
) (*connect.Response[mgmtv1alpha1.ReviewMappingChangesResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	// Reviewing is deciding about the job's anonymization: the right to edit the job.
	if err := user.EnforceJob(ctx, userdata.NewDbDomainEntity(accountUuid, jobUuid), rbac.JobAction_Edit); err != nil {
		return nil, err
	}

	params, err := reviewParams(user.PgId(), accountUuid, jobUuid, req.Msg.GetChangeIds(), req.Msg.Note)
	if err != nil {
		return nil, err
	}
	reviewed, err := s.db.Q.ReviewJobMappingChanges(ctx, s.db.Db, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.ReviewMappingChangesResponse{ChangeIds: uuidStrings(reviewed)}), nil
}

// ApplyMappingChanges sets the transformer of columns the job maps and marks the changes this
// settles reviewed, in one transaction: the correction and its trace cannot come apart.
func (s *Service) ApplyMappingChanges(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.ApplyMappingChangesRequest],
) (*connect.Response[mgmtv1alpha1.ApplyMappingChangesResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(ctx, userdata.NewDbDomainEntity(accountUuid, jobUuid), rbac.JobAction_Edit); err != nil {
		return nil, err
	}
	params, err := reviewParams(user.PgId(), accountUuid, jobUuid, req.Msg.GetChangeIds(), req.Msg.Note)
	if err != nil {
		return nil, err
	}

	var applied []*mgmtv1alpha1.JobMapping
	var reviewed []pgtype.UUID
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
		var mappings []*pg_models.JobMapping
		mappings, applied, err = setTransformers(job.Mappings, req.Msg.GetMappings())
		if err != nil {
			return err
		}
		if _, err := s.db.Q.UpdateJobMappings(ctx, dbtx, db_queries.UpdateJobMappingsParams{
			ID:          jobUuid,
			Mappings:    mappings,
			UpdatedByID: user.PgId(),
		}); err != nil {
			return fmt.Errorf("unable to update job mappings: %w", err)
		}
		if len(params.Ids) == 0 {
			return nil
		}
		reviewed, err = s.db.Q.ReviewJobMappingChanges(ctx, dbtx, params)
		return err
	}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.ApplyMappingChangesResponse{
		Mappings:  applied,
		ChangeIds: uuidStrings(reviewed),
	}), nil
}

// setTransformers replaces the transformer of the given columns among the job's mappings. A column
// the job does not map is refused rather than added: it may be one a run has just removed, and
// bringing it back from a page that showed its removal would undo the mirror of the source.
func setTransformers(
	stored []*pg_models.JobMapping,
	mappings []*mgmtv1alpha1.JobMapping,
) (out []*pg_models.JobMapping, applied []*mgmtv1alpha1.JobMapping, err error) {
	index := make(map[columnRef]int, len(stored))
	for i, m := range stored {
		index[columnRef{m.Schema, m.Table, m.Column}] = i
	}
	out = make([]*pg_models.JobMapping, len(stored))
	copy(out, stored)
	applied = make([]*mgmtv1alpha1.JobMapping, 0, len(mappings))
	for _, m := range mappings {
		name := fmt.Sprintf("%s.%s.%s", m.GetSchema(), m.GetTable(), m.GetColumn())
		if m.GetTransformer().GetConfig().GetConfig() == nil {
			return nil, nil, husonymerrors.NewBadRequest("no transformer given for " + name)
		}
		i, ok := index[columnRef{m.GetSchema(), m.GetTable(), m.GetColumn()}]
		if !ok {
			return nil, nil, husonymerrors.NewBadRequest("the job does not map " + name)
		}
		updated := &pg_models.JobMapping{}
		if err := updated.FromDto(m); err != nil {
			return nil, nil, err
		}
		out[i] = updated
		applied = append(applied, m)
	}
	return out, applied, nil
}

// reviewParams binds a review to one job of one account. The account is not decoration: the
// enforcement above checks that the caller may edit jobs of the account they named, never that
// the job id they passed belongs to it.
func reviewParams(
	reviewer, accountUuid, jobUuid pgtype.UUID,
	changeIds []string,
	note *string,
) (db_queries.ReviewJobMappingChangesParams, error) {
	params := db_queries.ReviewJobMappingChangesParams{
		ReviewedById: reviewer,
		AccountId:    accountUuid,
		JobId:        jobUuid,
	}
	for _, id := range changeIds {
		uuid, err := husonymdb.ToUuid(id)
		if err != nil {
			return params, err
		}
		params.Ids = append(params.Ids, uuid)
	}
	if note != nil && *note != "" {
		params.Note = pgtype.Text{String: *note, Valid: true}
	}
	return params, nil
}

func uuidStrings(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, husonymdb.UUIDString(id))
	}
	return out
}

func (s *Service) keepViewableChanges(
	ctx context.Context,
	user *userdata.User,
	accountUuid pgtype.UUID,
	rows []db_queries.HusonymApiJobMappingChange,
) []db_queries.HusonymApiJobMappingChange {
	viewable := map[string]bool{}
	kept := make([]db_queries.HusonymApiJobMappingChange, 0, len(rows))
	for i := range rows {
		jobId := husonymdb.UUIDString(rows[i].JobID)
		allowed, seen := viewable[jobId]
		if !seen {
			allowed = user.EnforceJob(
				ctx,
				userdata.NewDbDomainEntity(accountUuid, rows[i].JobID),
				rbac.JobAction_View,
			) == nil
			viewable[jobId] = allowed
		}
		if allowed {
			kept = append(kept, rows[i])
		}
	}
	return kept
}

// jobColumn names a column of one job.
type jobColumn struct {
	job string
	columnRef
}

// currentMappings indexes the jobs' mappings as they stand now.
func currentMappings(jobs []db_queries.HusonymApiJob) map[jobColumn]*pg_models.JobMapping {
	out := map[jobColumn]*pg_models.JobMapping{}
	for i := range jobs {
		jobId := husonymdb.UUIDString(jobs[i].ID)
		for _, m := range jobs[i].Mappings {
			out[jobColumn{jobId, columnRef{m.Schema, m.Table, m.Column}}] = m
		}
	}
	return out
}

// pendingChanges keeps, among the unreviewed changes, those still waiting for somebody.
//
// An added column whose mapping is no longer the one the run chose has been decided about: the
// change is settled without anybody having to review it too. A type change of a column the job no
// longer maps has nothing left to review. A removal waits until it is reviewed.
func pendingChanges(
	rows []db_queries.HusonymApiJobMappingChange,
	mappings map[jobColumn]*pg_models.JobMapping,
) ([]*mgmtv1alpha1.JobMappingChange, error) {
	out := make([]*mgmtv1alpha1.JobMappingChange, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		key := jobColumn{husonymdb.UUIDString(row.JobID), columnRef{row.TableSchema, row.TableName, row.ColumnName}}
		mapping := mappings[key]

		var recorded *mgmtv1alpha1.JobMappingTransformer
		if row.Transformer != nil {
			dto, err := row.Transformer.ToTransformerDto()
			if err != nil {
				return nil, err
			}
			recorded = dto
		}

		var kind mgmtv1alpha1.JobMappingChangeKind
		transformer := recorded
		switch row.Kind {
		case changeAdded:
			kind = mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_ADDED
			if mapping == nil {
				continue
			}
			current, err := mapping.JobMappingTransformer.ToTransformerDto()
			if err != nil {
				return nil, err
			}
			if !proto.Equal(current.GetConfig(), recorded.GetConfig()) {
				continue
			}
		case changeRemoved:
			kind = mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_REMOVED
		case changeTypeChanged:
			kind = mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_TYPE_CHANGED
			if mapping == nil {
				continue
			}
			current, err := mapping.JobMappingTransformer.ToTransformerDto()
			if err != nil {
				return nil, err
			}
			transformer = current
		default:
			continue
		}

		out = append(out, &mgmtv1alpha1.JobMappingChange{
			Id:               husonymdb.UUIDString(row.ID),
			JobId:            husonymdb.UUIDString(row.JobID),
			JobRunId:         row.JobRunID,
			CreatedAt:        timestamppb.New(row.CreatedAt.Time),
			Column:           &mgmtv1alpha1.JobColumn{Schema: row.TableSchema, Table: row.TableName, Column: row.ColumnName},
			Kind:             kind,
			Transformer:      transformer,
			DataType:         row.DataType,
			PreviousDataType: row.PreviousDataType,
			PiiCategory:      row.PiiCategory,
		})
	}
	return out, nil
}
