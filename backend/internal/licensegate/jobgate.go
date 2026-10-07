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

// Refusal is the error of a job that uses features the license does not include. It is told
// apart from every other error by its type: an error that is not a Refusal means the question
// could not be answered, whatever code it carries.
type Refusal struct {
	// Missing lists the features the job uses and the license lacks, in the order of
	// license.AllFeatures.
	Missing []license.Feature
}

// Message is the sentence a person is told, which names every missing feature.
func (r *Refusal) Message() string {
	return RefusalMessage(r.Missing)
}

func (r *Refusal) Error() string {
	return r.Unwrap().Error()
}

// Unwrap gives the answer of a handler that returns the refusal to its caller: permission
// denied, with the sentence.
func (r *Refusal) Unwrap() error {
	return husonymerrors.NewForbidden(r.Message())
}

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
// It does not ask whether the license is in force: its callers have, and refuse first. The
// refusal is a *Refusal; any other error means the question could not be answered.
//
// The job may be one that is not stored yet, without an id: it is checked on its definition
// alone, since a job being created has no hook.
func (g *JobGate) Check(ctx context.Context, job *mgmtv1alpha1.Job) error {
	return g.CheckIn(ctx, g.db.Db, job)
}

// CheckIn is Check reading through the given handle. A caller that holds a transaction gives
// it: reading through the pool instead would take a second connection while the first is held,
// and callers queued on the same row could then exhaust the pool and wait on one another.
func (g *JobGate) CheckIn(ctx context.Context, dbtx husonymdb.BaseDBTX, job *mgmtv1alpha1.Job) error {
	used, err := featuresOfJob(ctx, g.db, dbtx, job)
	if err != nil {
		return err
	}
	missing := MissingFeatures(g.lic, used)
	if len(missing) == 0 {
		return nil
	}
	return &Refusal{Missing: missing}
}

// featuresOfJob lists the licensed features a job uses, from its definition and from what only
// the database knows about it: whether it has an enabled hook, and what the user-defined
// transformers it references store. It reads through the given handle.
func featuresOfJob(
	ctx context.Context,
	db *husonymdb.HusonymDb,
	dbtx husonymdb.BaseDBTX,
	job *mgmtv1alpha1.Job,
) ([]license.Feature, error) {
	accountUuid, err := husonymdb.ToUuid(job.GetAccountId())
	if err != nil {
		return nil, err
	}

	hasEnabledHooks := false
	if job.GetId() != "" {
		jobUuid, err := husonymdb.ToUuid(job.GetId())
		if err != nil {
			return nil, err
		}
		// A hook of any timing counts, as long as it is enabled: a disabled one does not run.
		hooks, err := db.Q.GetActiveJobHooks(ctx, dbtx, jobUuid)
		if err != nil {
			return nil, fmt.Errorf("unable to get the enabled hooks of job %s: %w", job.GetId(), err)
		}
		hasEnabledHooks = len(hooks) > 0
	}

	lookup := func(ctx context.Context, id string) (*mgmtv1alpha1.TransformerConfig, error) {
		transformerUuid, err := husonymdb.ToUuid(id)
		if err != nil {
			return nil, err
		}
		transformer, err := db.Q.GetUserDefinedTransformerById(ctx, dbtx, transformerUuid)
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, err
		}
		// The transformer of another account is none of this job's: it does not exist for it.
		if err != nil || transformer.AccountID != accountUuid {
			return nil, nil
		}
		return transformer.TransformerConfig.ToTransformerConfigDto()
	}

	return FeaturesUsedBy(ctx, JobFacts{Job: job, HasEnabledHooks: hasEnabledHooks}, lookup)
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
