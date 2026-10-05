package hooks

import (
	"context"
	"fmt"
	"math"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// storedJobHook is what a job hook holds, checked and in the form the database stores.
type storedJobHook struct {
	config   []byte
	priority int32
}

// checkJobHook checks what a request gives a job hook, against the job it belongs to: an SQL
// hook, with a timing, on a connection the job uses, at a priority the database can hold (the
// contract bounds it further). The SQL is stored as it is given.
func (s *JobService) checkJobHook(
	ctx context.Context,
	jobID pgtype.UUID,
	config *mgmtv1alpha1.JobHookConfig,
	priority uint32,
) (*storedJobHook, error) {
	sql := config.GetSql()
	if sql == nil {
		return nil, husonymerrors.NewBadRequest("job hook config is required: an sql hook")
	}
	if sql.GetTiming().GetTiming() == nil {
		return nil, husonymerrors.NewBadRequest("job hook timing is required: pre_sync or post_sync")
	}
	if priority > math.MaxInt32 {
		return nil, husonymerrors.NewBadRequest("job hook priority is out of range")
	}
	connectionID, err := husonymdb.ToUuid(sql.GetConnectionId())
	if err != nil {
		return nil, husonymerrors.NewBadRequest("connection id specified in hook is not a valid uuid")
	}
	used, err := s.db.Q.DoesJobHaveConnectionId(ctx, s.db.Db, db_queries.DoesJobHaveConnectionIdParams{
		JobId:        jobID,
		ConnectionId: connectionID,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to tell whether the job uses the connection of the hook: %w", err)
	}
	if !used {
		return nil, husonymerrors.NewBadRequest("connection id specified in hook is not a part of job")
	}
	encoded, err := protojson.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the job hook config: %w", err)
	}
	return &storedJobHook{config: encoded, priority: int32(priority)}, nil
}

// stored reads a configuration the way the database holds it. A key this version does not
// know is passed over, so that a hook of an unknown kind reads as a hook without a
// configuration rather than failing the list it is in.
var stored = protojson.UnmarshalOptions{DiscardUnknown: true}

func toJobHook(ctx context.Context, row *db_queries.HusonymApiJobHook) (*mgmtv1alpha1.JobHook, error) {
	config := &mgmtv1alpha1.JobHookConfig{}
	if err := stored.Unmarshal(row.Config, config); err != nil {
		return nil, unreadableConfig(ctx, "job", husonymdb.UUIDString(row.ID))
	}
	return &mgmtv1alpha1.JobHook{
		Id:              husonymdb.UUIDString(row.ID),
		Name:            row.Name,
		Description:     row.Description,
		JobId:           husonymdb.UUIDString(row.JobID),
		Config:          config,
		CreatedByUserId: husonymdb.UUIDString(row.CreatedByUserID),
		CreatedAt:       timestamppb.New(row.CreatedAt.Time),
		UpdatedByUserId: husonymdb.UUIDString(row.UpdatedByUserID),
		UpdatedAt:       timestamppb.New(row.UpdatedAt.Time),
		Enabled:         row.Enabled,
		Priority:        uint32(max(row.Priority, 0)),
	}, nil
}

func toJobHooks(ctx context.Context, rows []db_queries.HusonymApiJobHook) ([]*mgmtv1alpha1.JobHook, error) {
	hooks := make([]*mgmtv1alpha1.JobHook, 0, len(rows))
	for i := range rows {
		hook, err := toJobHook(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}
