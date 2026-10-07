package licensegate

import (
	"context"
	"fmt"
	"slices"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// SourceCandidate is a job as a write is about to leave it, in the models the write stores.
type SourceCandidate struct {
	// JobId is left invalid for a job that is being created.
	JobId             pgtype.UUID
	AccountId         pgtype.UUID
	ConnectionOptions *pg_models.JobSourceOptions
	// JobtypeConfig is nil when the write leaves the job the type it has.
	JobtypeConfig []byte
	Mappings      []*pg_models.JobMapping
}

// SourceGuard gives what a write of the candidate must run first in its transaction, so that
// the write does not bring the instance over the cap of the license on sources. It gives nil
// when the license has no such cap: there is then nothing to run.
//
// The rule: a source is added if the instance, as stored now, does not have it; the guard
// refuses a write that adds a source and leaves the instance with more than the cap. A write
// that adds none is never refused, so an instance a newer key left over its cap can still
// change its jobs and give sources up. The instance is counted as the write leaves it, the
// candidate in place of the job as it is stored: at the cap, a job that moves from a source it
// alone reads to another leaves the count where it was.
//
// It holds the lock on the sources of the instance until the transaction ends, so that two
// writes cannot both count the room the other is about to take. This relies on read committed:
// the listing must see what the transaction that held the lock before it wrote, so a caller
// must not run the guard in a stricter isolation level. Unlike the check made when a run
// starts, it fails closed: a write that cannot be counted is not made.
//
// Like Check, it does not ask whether the license is in force: its callers have.
func (g *JobGate) SourceGuard(candidate *SourceCandidate) func(ctx context.Context, dbtx husonymdb.BaseDBTX) error {
	limits := g.lic.Limits()
	if limits == nil || limits.MaxSources == nil {
		return nil
	}
	maxSources := *limits.MaxSources

	return func(ctx context.Context, dbtx husonymdb.BaseDBTX) error {
		if err := g.db.Q.LockLicenseSources(ctx, dbtx); err != nil {
			return fmt.Errorf("unable to lock the sources of the instance: %w", err)
		}
		jobs, err := g.db.Q.ListJobSourcesOfInstance(ctx, dbtx)
		if err != nil {
			return fmt.Errorf("unable to list the sources of the instance: %w", err)
		}

		// The candidate is counted by the rules every stored job is: as a row of the listing.
		own := db_queries.ListJobSourcesOfInstanceRow{
			ID:                candidate.JobId,
			AccountID:         candidate.AccountId,
			ConnectionOptions: candidate.ConnectionOptions,
			JobtypeConfig:     candidate.JobtypeConfig,
			Schemas:           schemasOf(candidate.Mappings),
		}
		others := make([]db_queries.ListJobSourcesOfInstanceRow, 0, len(jobs))
		for _, job := range jobs {
			if candidate.JobId.Valid && job.ID == candidate.JobId {
				if own.JobtypeConfig == nil {
					own.JobtypeConfig = job.JobtypeConfig
				}
				continue
			}
			others = append(others, job)
		}

		current := SourcesOf(jobs)
		after := SourcesOf(append(others, own))
		if len(after) <= maxSources {
			return nil
		}
		addsSource := slices.ContainsFunc(after, func(source Source) bool {
			return !slices.Contains(current, source)
		})
		if !addsSource {
			return nil
		}
		return husonymerrors.NewForbidden(fmt.Sprintf(
			"this license allows %d source(s) and this change would bring the instance to %d; contact us to raise the limit",
			maxSources, len(after),
		))
	}
}

// schemasOf gives the distinct schemas the mappings name, as the listing of the stored jobs
// gives them: without empty names.
func schemasOf(mappings []*pg_models.JobMapping) []string {
	schemas := []string{}
	for _, mapping := range mappings {
		if mapping.Schema != "" && !slices.Contains(schemas, mapping.Schema) {
			schemas = append(schemas, mapping.Schema)
		}
	}
	return schemas
}
