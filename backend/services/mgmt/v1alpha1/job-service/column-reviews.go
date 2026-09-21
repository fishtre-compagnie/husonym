package v1alpha1_jobservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A column a job does not map is copied untransformed and reported on every validation. Some of
// those are deliberate, and until there was somewhere to say so the list could only grow — a
// list that never shrinks stops being read, which is the failure the report existed to prevent.
// These three methods are that somewhere.

// GetColumnReviews returns the accepted passthroughs of a job.
func (s *Service) GetColumnReviews(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetColumnReviewsRequest],
) (*connect.Response[mgmtv1alpha1.GetColumnReviewsResponse], error) {
	jobResp, err := s.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{
		Id: req.Msg.GetJobId(),
	}))
	if err != nil {
		return nil, err
	}
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(ctx, jobResp.Msg.GetJob(), rbac.JobAction_View); err != nil {
		return nil, err
	}

	reviews, err := s.getColumnReviews(ctx, req.Msg.GetJobId(), req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetColumnReviewsResponse{Reviews: reviews}), nil
}

// SetColumnReview accepts the passthrough of one unmapped column.
//
// The column's type and detected category are read from the source rather than taken from the
// request: they are the record of what was accepted, and a caller cannot be the one to describe
// what it is asking to have forgiven.
func (s *Service) SetColumnReview(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetColumnReviewRequest],
) (*connect.Response[mgmtv1alpha1.SetColumnReviewResponse], error) {
	jobResp, err := s.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{
		Id: req.Msg.GetJobId(),
	}))
	if err != nil {
		return nil, err
	}
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	// Accepting that a column ships untransformed is an edit of what the job protects, not a
	// note in the margin, so it takes the same right as changing a mapping.
	if err := user.EnforceJob(ctx, jobResp.Msg.GetJob(), rbac.JobAction_Edit); err != nil {
		return nil, err
	}

	dataType, piiCategory, err := s.describeSourceColumn(
		ctx,
		jobResp.Msg.GetJob(),
		req.Msg.GetTableSchema(),
		req.Msg.GetTableName(),
		req.Msg.GetColumnName(),
	)
	if err != nil {
		return nil, err
	}

	jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	var note pgtype.Text
	if req.Msg.Note != nil {
		note = pgtype.Text{String: req.Msg.GetNote(), Valid: true}
	}

	record, err := s.db.Q.SetColumnReview(ctx, s.db.Db, db_queries.SetColumnReviewParams{
		AccountID:           accountUuid,
		JobID:               jobUuid,
		TableSchema:         req.Msg.GetTableSchema(),
		TableName:           req.Msg.GetTableName(),
		ColumnName:          req.Msg.GetColumnName(),
		ReviewedDataType:    dataType,
		ReviewedPiiCategory: piiCategory,
		Note:                note,
		CreatedByID:         user.PgId(),
		UpdatedByID:         user.PgId(),
	})
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.SetColumnReviewResponse{
		Review: toColumnReviewDto(record),
	}), nil
}

// RemoveColumnReview withdraws an acceptance, putting the column back in the report.
func (s *Service) RemoveColumnReview(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RemoveColumnReviewRequest],
) (*connect.Response[mgmtv1alpha1.RemoveColumnReviewResponse], error) {
	jobResp, err := s.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{
		Id: req.Msg.GetJobId(),
	}))
	if err != nil {
		return nil, err
	}
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(ctx, jobResp.Msg.GetJob(), rbac.JobAction_Edit); err != nil {
		return nil, err
	}

	jobUuid, err := husonymdb.ToUuid(req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Q.RemoveColumnReview(ctx, s.db.Db, db_queries.RemoveColumnReviewParams{
		JobId:       jobUuid,
		AccountId:   accountUuid,
		TableSchema: req.Msg.GetTableSchema(),
		TableName:   req.Msg.GetTableName(),
		ColumnName:  req.Msg.GetColumnName(),
	})
	if err != nil {
		return nil, err
	}

	// Not an error: withdrawing a decision that is not there leaves the world as the caller
	// wants it. Saying so lets a client tell that apart from having removed something.
	return connect.NewResponse(&mgmtv1alpha1.RemoveColumnReviewResponse{
		Removed: rows > 0,
	}), nil
}

func (s *Service) getColumnReviews(
	ctx context.Context,
	jobId, accountId string,
) ([]*mgmtv1alpha1.ColumnReview, error) {
	jobUuid, err := husonymdb.ToUuid(jobId)
	if err != nil {
		return nil, err
	}
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, err
	}
	records, err := s.db.Q.GetColumnReviewsByJob(ctx, s.db.Db, db_queries.GetColumnReviewsByJobParams{
		JobId:     jobUuid,
		AccountId: accountUuid,
	})
	if err != nil {
		return nil, err
	}
	reviews := make([]*mgmtv1alpha1.ColumnReview, 0, len(records))
	for idx := range records {
		reviews = append(reviews, toColumnReviewDto(records[idx]))
	}
	return reviews, nil
}

// acceptedPassthroughsByTable shapes the job's decisions the way the validator looks them up.
func (s *Service) acceptedPassthroughsByTable(
	ctx context.Context,
	jobId, accountId string,
) (map[string]map[string]job_util.AcceptedPassthrough, error) {
	reviews, err := s.getColumnReviews(ctx, jobId, accountId)
	if err != nil {
		return nil, err
	}
	accepted := map[string]map[string]job_util.AcceptedPassthrough{}
	for _, review := range reviews {
		table := sqlmanager_shared.BuildTable(review.GetTableSchema(), review.GetTableName())
		if _, ok := accepted[table]; !ok {
			accepted[table] = map[string]job_util.AcceptedPassthrough{}
		}
		accepted[table][review.GetColumnName()] = job_util.AcceptedPassthrough{
			DataType:    review.GetReviewedDataType(),
			PiiCategory: review.GetReviewedPiiCategory(),
		}
	}
	return accepted, nil
}

// describeSourceColumn reads the column as it stands right now, which is what the decision is
// recorded against.
func (s *Service) describeSourceColumn(
	ctx context.Context,
	job *mgmtv1alpha1.Job,
	schema, table, column string,
) (dataType, piiCategory string, err error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	connectionId, err := getJobSourceConnectionId(job.GetSource())
	if err != nil {
		return "", "", err
	}
	if connectionId == nil {
		return "", "", connect.NewError(
			connect.CodeInvalidArgument,
			fmt.Errorf("job %q has no source connection to read the column from", job.GetId()),
		)
	}

	connResp, err := s.connectionService.GetConnection(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: *connectionId}),
	)
	if err != nil {
		return "", "", err
	}
	dataconn, err := s.connectiondatabuilder.NewDataConnection(logger, connResp.Msg.GetConnection())
	if err != nil {
		return "", "", err
	}
	columns, err := dataconn.GetTableSchema(ctx, schema, table)
	if err != nil {
		return "", "", err
	}
	for _, candidate := range columns {
		if candidate.GetColumn() != column {
			continue
		}
		// The same verdict the validator and the run use, so what is accepted here is what was
		// reported there.
		category, _ := job_util.LooksSensitive(column, candidate.GetDataType())
		return candidate.GetDataType(), category, nil
	}

	// Refused rather than recorded blind: a decision about a column that is not in the source
	// would sit in the table forever, matching nothing and forgiving nothing.
	return "", "", connect.NewError(connect.CodeNotFound, fmt.Errorf(
		"no column %q in %s.%s of the job's source", column, schema, table,
	))
}

func toColumnReviewDto(record db_queries.HusonymApiColumnReview) *mgmtv1alpha1.ColumnReview {
	review := &mgmtv1alpha1.ColumnReview{
		TableSchema:         record.TableSchema,
		TableName:           record.TableName,
		ColumnName:          record.ColumnName,
		ReviewedDataType:    record.ReviewedDataType,
		ReviewedPiiCategory: record.ReviewedPiiCategory,
		UpdatedByUserId:     husonymdb.UUIDString(record.UpdatedByID),
	}
	if record.Note.Valid {
		note := record.Note.String
		review.Note = &note
	}
	if record.UpdatedAt.Valid {
		review.UpdatedAt = timestamppb.New(record.UpdatedAt.Time)
	}
	return review
}
