package licensegate

import (
	"context"
	"errors"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// ErrJobNotFound is what CheckStored answers for a job the account does not have. It is not a
// refusal: nothing was decided about the job.
var ErrJobNotFound = errors.New("job not found in this account")

// JobGate decides whether a job may start under the license, from the job's definition and
// from what only the database knows about it.
type JobGate struct {
	db  *husonymdb.HusonymDb
	lic license.EEInterface
}

func NewJobGate(db *husonymdb.HusonymDb, lic license.EEInterface) *JobGate {
	return &JobGate{db: db, lic: lic}
}

// Check refuses a job that uses a feature the license does not include, naming every such
// feature. A job is never started without one of them: it starts whole or not at all.
//
// It does not ask whether the license is in force: its callers have, and refuse first. An error
// that is not the refusal means the question could not be answered.
//
// The job may be one that is not stored yet, without an id: it is checked on its definition
// alone, since a job being created has no hook.
func (g *JobGate) Check(ctx context.Context, job *mgmtv1alpha1.Job) error {
	accountUuid, err := husonymdb.ToUuid(job.GetAccountId())
	if err != nil {
		return err
	}

	hasEnabledHooks := false
	if job.GetId() != "" {
		jobUuid, err := husonymdb.ToUuid(job.GetId())
		if err != nil {
			return err
		}
		// A hook of any timing counts, as long as it is enabled: a disabled one does not run.
		hooks, err := g.db.Q.GetActiveJobHooks(ctx, g.db.Db, jobUuid)
		if err != nil {
			return fmt.Errorf("unable to get the enabled hooks of job %s: %w", job.GetId(), err)
		}
		hasEnabledHooks = len(hooks) > 0
	}

	lookup := func(ctx context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
		transformerUuid, err := husonymdb.ToUuid(id)
		if err != nil {
			return nil, err
		}
		transformer, err := g.db.Q.GetUserDefinedTransformerById(ctx, g.db.Db, transformerUuid)
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, err
		}
		// The transformer of another account is none of this job's: it does not exist for it.
		if err != nil || transformer.AccountID != accountUuid {
			return nil, nil
		}
		return transformer.TransformerConfig.ToTransformerConfigDto()
	}

	used, err := FeaturesUsedBy(ctx, JobFacts{Job: job, HasEnabledHooks: hasEnabledHooks}, lookup)
	if err != nil {
		return err
	}
	missing := MissingFeatures(g.lic, used)
	if len(missing) == 0 {
		return nil
	}
	return husonymerrors.NewForbidden(RefusalMessage(missing))
}

// CheckStored is Check for a job known by its id: the job is read from the database as the job
// service reads it. It answers ErrJobNotFound for a job the account does not have.
func (g *JobGate) CheckStored(ctx context.Context, accountId, jobId string) error {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return err
	}
	jobUuid, err := husonymdb.ToUuid(jobId)
	if err != nil {
		return err
	}

	dbJob, err := g.db.Q.GetJobById(ctx, g.db.Db, jobUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return fmt.Errorf("unable to get job by id: %w", err)
	}
	if err != nil || dbJob.AccountID != accountUuid {
		return ErrJobNotFound
	}
	destinations, err := g.db.Q.GetJobConnectionDestinations(ctx, g.db.Db, jobUuid)
	if err != nil {
		return fmt.Errorf("unable to get job connection destinations by job id: %w", err)
	}

	job, err := dtomaps.ToJobDto(&dbJob, destinations)
	if err != nil {
		return fmt.Errorf("unable to convert job to dto: %w", err)
	}
	return g.Check(ctx, job)
}
