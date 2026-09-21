package v1alpha1_jobservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SetJobUnmappedPassthroughs records what a run copied untransformed for want of a mapping.
//
// The job's set is replaced, not appended to: every column the run reported is written with the
// run's id, then the rows that run did not report are removed. A column that has been mapped
// since therefore leaves the list on its own, and a run that reports nothing clears it. Both
// steps share one transaction, or a reader could catch the list half replaced — the bell showing
// zero between the two statements.
func (s *Service) SetJobUnmappedPassthroughs(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetJobUnmappedPassthroughsRequest],
) (*connect.Response[mgmtv1alpha1.SetJobUnmappedPassthroughsResponse], error) {
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
	// The same guard as the other endpoints only a run should call. These rows are the record of
	// what left the source in clear; letting anyone but the worker write them would let anyone
	// empty the bell.
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

	if err := s.db.WithTx(ctx, nil, func(dbtx husonymdb.BaseDBTX) error {
		for _, column := range req.Msg.GetColumns() {
			if err := s.db.Q.UpsertUnmappedPassthrough(ctx, dbtx, db_queries.UpsertUnmappedPassthroughParams{
				AccountID:        accountUuid,
				JobID:            jobUuid,
				TableSchema:      column.GetTableSchema(),
				TableName:        column.GetTableName(),
				ColumnName:       column.GetColumnName(),
				DataType:         column.GetDataType(),
				LastSeenJobRunID: req.Msg.GetJobRunId(),
			}); err != nil {
				return fmt.Errorf("unable to record unmapped passthrough: %w", err)
			}
		}
		return s.db.Q.DeleteUnmappedPassthroughsNotSeenInRun(
			ctx,
			dbtx,
			db_queries.DeleteUnmappedPassthroughsNotSeenInRunParams{
				JobId:     jobUuid,
				AccountId: accountUuid,
				JobRunId:  req.Msg.GetJobRunId(),
			},
		)
	}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.SetJobUnmappedPassthroughsResponse{}), nil
}

// GetPendingColumnReviews returns what is waiting for a decision, for one job or for the account.
func (s *Service) GetPendingColumnReviews(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetPendingColumnReviewsRequest],
) (*connect.Response[mgmtv1alpha1.GetPendingColumnReviewsResponse], error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	var copied []db_queries.HusonymApiUnmappedPassthrough
	var accepted []db_queries.HusonymApiColumnReview
	var jobs []db_queries.HusonymApiJob
	if req.Msg.JobId != nil {
		jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
		if err != nil {
			return nil, err
		}
		if err := user.EnforceJob(
			ctx,
			userdata.NewDbDomainEntity(accountUuid, jobUuid),
			rbac.JobAction_View,
		); err != nil {
			return nil, err
		}
		copied, err = s.db.Q.GetUnmappedPassthroughsByJob(ctx, s.db.Db, db_queries.GetUnmappedPassthroughsByJobParams{
			JobId:     jobUuid,
			AccountId: accountUuid,
		})
		if err != nil {
			return nil, err
		}
		accepted, err = s.db.Q.GetColumnReviewsByJob(ctx, s.db.Db, db_queries.GetColumnReviewsByJobParams{
			JobId:     jobUuid,
			AccountId: accountUuid,
		})
		if err != nil {
			return nil, err
		}
		job, err := s.db.Q.GetJobById(ctx, s.db.Db, jobUuid)
		if err != nil {
			return nil, err
		}
		jobs = []db_queries.HusonymApiJob{job}
	} else {
		copied, err = s.db.Q.GetUnmappedPassthroughsByAccount(ctx, s.db.Db, accountUuid)
		if err != nil {
			return nil, err
		}
		accepted, err = s.db.Q.GetColumnReviewsByAccount(ctx, s.db.Db, accountUuid)
		if err != nil {
			return nil, err
		}
		jobs, err = s.db.Q.GetJobsByAccount(ctx, s.db.Db, accountUuid)
		if err != nil {
			return nil, err
		}
		// The bell covers the account, but a person may see only some of its jobs. Checked job
		// by job rather than once for the account: a count that includes a job someone cannot
		// open is a leak of the one thing this endpoint exists to reveal — which columns ship in
		// clear.
		copied = s.keepViewableJobs(ctx, user, accountUuid, copied)
	}

	return connect.NewResponse(&mgmtv1alpha1.GetPendingColumnReviewsResponse{
		Columns: pendingColumns(copied, mappedColumns(jobs), accepted),
	}), nil
}

// MapUnmappedColumns adds mappings for columns the job does not map yet: the "anonymize" of the
// review tab.
//
// It only adds. The tab is a snapshot, and between the moment it was loaded and the click
// somebody may have mapped one of these columns from the source page; overwriting their choice
// with a suggestion would undo a decision in the name of making one. Such a column is skipped
// and left out of the response, so the caller can tell.
func (s *Service) MapUnmappedColumns(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.MapUnmappedColumnsRequest],
) (*connect.Response[mgmtv1alpha1.MapUnmappedColumnsResponse], error) {
	jobResp, err := s.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{
		Id: req.Msg.GetJobId(),
	}))
	if err != nil {
		return nil, err
	}
	jobDto := jobResp.Msg.GetJob()
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	// The same checks as saving the whole source: this writes the job's mappings too.
	if err := user.EnforceJob(ctx, jobDto, rbac.JobAction_Edit); err != nil {
		return nil, err
	}
	if err := user.EnforceLicense(ctx, jobDto.GetAccountId()); err != nil {
		return nil, err
	}

	mappingKey := func(m *mgmtv1alpha1.JobMapping) string {
		return fmt.Sprintf("%s.%s.%s", m.GetSchema(), m.GetTable(), m.GetColumn())
	}

	existing := map[string]struct{}{}
	mappings := make([]*pg_models.JobMapping, 0, len(jobDto.GetMappings())+len(req.Msg.GetMappings()))
	for _, mapping := range jobDto.GetMappings() {
		stored := &pg_models.JobMapping{}
		if err := stored.FromDto(mapping); err != nil {
			return nil, err
		}
		mappings = append(mappings, stored)
		existing[mappingKey(mapping)] = struct{}{}
	}

	added := []*mgmtv1alpha1.JobMapping{}
	for _, mapping := range req.Msg.GetMappings() {
		// A mapping with no transformer would be written as a column the job maps and does
		// nothing to — a passthrough under another name, from a button labelled "anonymize".
		if mapping.GetTransformer().GetConfig().GetConfig() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(
				"no transformer given for %s", mappingKey(mapping),
			))
		}
		if _, ok := existing[mappingKey(mapping)]; ok {
			continue
		}
		stored := &pg_models.JobMapping{}
		if err := stored.FromDto(mapping); err != nil {
			return nil, err
		}
		mappings = append(mappings, stored)
		existing[mappingKey(mapping)] = struct{}{}
		added = append(added, mapping)
	}

	if len(added) > 0 {
		jobUuid, err := husonymdb.ToUuid(jobDto.GetId())
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Q.UpdateJobMappings(ctx, s.db.Db, db_queries.UpdateJobMappingsParams{
			ID:          jobUuid,
			Mappings:    mappings,
			UpdatedByID: user.PgId(),
		}); err != nil {
			return nil, fmt.Errorf("unable to update job mappings: %w", err)
		}
	}

	return connect.NewResponse(&mgmtv1alpha1.MapUnmappedColumnsResponse{Added: added}), nil
}

func (s *Service) keepViewableJobs(
	ctx context.Context,
	user *userdata.User,
	accountUuid pgtype.UUID,
	copied []db_queries.HusonymApiUnmappedPassthrough,
) []db_queries.HusonymApiUnmappedPassthrough {
	viewable := map[string]bool{}
	kept := make([]db_queries.HusonymApiUnmappedPassthrough, 0, len(copied))
	for _, row := range copied {
		jobId := husonymdb.UUIDString(row.JobID)
		allowed, seen := viewable[jobId]
		if !seen {
			allowed = user.EnforceJob(
				ctx,
				userdata.NewDbDomainEntity(accountUuid, row.JobID),
				rbac.JobAction_View,
			) == nil
			viewable[jobId] = allowed
		}
		if allowed {
			kept = append(kept, row)
		}
	}
	return kept
}

// columnKey names one column of one job.
type columnKey struct{ job, schema, table, column string }

// mappedColumns are the columns the jobs map now. The last run copied them in clear, but the
// next one will not, so they are settled — and they have to leave the list the moment they are
// mapped, not at the next run, or the bell would still count what somebody has just fixed.
func mappedColumns(jobs []db_queries.HusonymApiJob) map[columnKey]struct{} {
	mapped := map[columnKey]struct{}{}
	for _, job := range jobs {
		jobId := husonymdb.UUIDString(job.ID)
		for _, mapping := range job.Mappings {
			mapped[columnKey{jobId, mapping.Schema, mapping.Table, mapping.Column}] = struct{}{}
		}
	}
	return mapped
}

// pendingColumns derives what is waiting from what the runs copied, what is mapped now, and what
// was accepted.
//
// A copied column is pending unless it has been mapped since, or an acceptance covers it and
// still holds — that is, unless the column is the one that was accepted. One that was accepted
// and has changed since is pending again, with its own reason: its reviewer is asked to confirm,
// not to look for the first time.
func pendingColumns(
	copied []db_queries.HusonymApiUnmappedPassthrough,
	mapped map[columnKey]struct{},
	accepted []db_queries.HusonymApiColumnReview,
) []*mgmtv1alpha1.PendingColumnReview {
	decisions := make(map[columnKey]job_util.AcceptedPassthrough, len(accepted))
	for _, review := range accepted {
		decisions[columnKey{
			husonymdb.UUIDString(review.JobID),
			review.TableSchema,
			review.TableName,
			review.ColumnName,
		}] = job_util.AcceptedPassthrough{
			DataType:    review.ReviewedDataType,
			PiiCategory: review.ReviewedPiiCategory,
		}
	}

	pending := make([]*mgmtv1alpha1.PendingColumnReview, 0, len(copied))
	for _, row := range copied {
		jobId := husonymdb.UUIDString(row.JobID)
		key := columnKey{jobId, row.TableSchema, row.TableName, row.ColumnName}
		if _, ok := mapped[key]; ok {
			continue
		}
		reason := mgmtv1alpha1.PendingColumnReason_PENDING_COLUMN_REASON_NEVER_REVIEWED
		if decision, ok := decisions[key]; ok {
			if decision.StillHoldsFor(row.ColumnName, row.DataType) {
				continue
			}
			reason = mgmtv1alpha1.PendingColumnReason_PENDING_COLUMN_REASON_CHANGED_SINCE_ACCEPTED
		}

		item := &mgmtv1alpha1.PendingColumnReview{
			JobId:       jobId,
			TableSchema: row.TableSchema,
			TableName:   row.TableName,
			ColumnName:  row.ColumnName,
			DataType:    row.DataType,
			Reason:      reason,
		}
		// The same verdict the validator and the run use. The suggestion is only offered for a
		// column that reads as personal data: for the others there is nothing to go on, and a
		// guessed transformer would look more certain than it is.
		if category, sensitive := job_util.LooksSensitive(row.ColumnName, row.DataType); sensitive {
			item.PiiCategory = category
			if classification, ok := piidetect.Classify(row.ColumnName, row.DataType); ok {
				item.SuggestedTransformerSource = classification.Suggested
			}
		}
		if row.FirstSeenAt.Valid {
			item.FirstSeenAt = timestamppb.New(row.FirstSeenAt.Time)
		}
		pending = append(pending, item)
	}
	return pending
}
