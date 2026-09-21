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
		if len(result.added) == 0 && len(result.removed) == 0 {
			return nil
		}
		return s.db.Q.SetJobMappingsFromRun(ctx, dbtx, db_queries.SetJobMappingsFromRunParams{
			Mappings: result.mappings,
			ID:       jobUuid,
		})
	}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.ReconcileJobMappingsResponse{
		Added:   result.added,
		Removed: result.removed,
	}), nil
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
	type key struct{ schema, table, column string }

	gone := make(map[key]struct{}, len(removed))
	for _, column := range removed {
		gone[key{column.GetSchema(), column.GetTable(), column.GetColumn()}] = struct{}{}
	}

	result := &reconciledMappings{mappings: make([]*pg_models.JobMapping, 0, len(stored)+len(added))}
	mapped := make(map[key]struct{}, len(stored))
	for _, mapping := range stored {
		k := key{mapping.Schema, mapping.Table, mapping.Column}
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
		k := key{mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn()}
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
