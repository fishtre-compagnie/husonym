package licensegate

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/jackc/pgx/v5/pgtype"
)

// Usage is what an account uses of the license of its instance, whatever the license allows.
type Usage struct {
	// SourcesInInstance counts the sources of every account: it is what the license caps.
	SourcesInInstance int
	// SourcesInAccount are the account's own among them, in the order of SourcesOf.
	SourcesInAccount []AccountSource
	// FeaturesInUse are the licensed features the account uses, in the order of
	// license.AllFeatures, each at most once.
	FeaturesInUse []license.Feature
}

// AccountSource is a source of the account that asks, with the name of its connection.
// ConnectionName is empty when the account has no connection of that id any more.
type AccountSource struct {
	ConnectionId   string
	ConnectionName string
	Database       string
}

// Roles gives the role each of the users holds in an account. rbac.Interface is one.
type Roles interface {
	Roles(users []rbac.User, account rbac.Account) map[rbac.User]mgmtv1alpha1.AccountRole
}

// UsageReader tells what an account uses of the license, from what the database stores. It
// does not read the license: what is used is told whether or not the license allows it.
type UsageReader struct {
	db    *husonymdb.HusonymDb
	roles Roles
}

func NewUsageReader(db *husonymdb.HusonymDb, roles Roles) *UsageReader {
	return &UsageReader{db: db, roles: roles}
}

// Of gives the usage of an account. The count of sources is the only thing it tells about the
// other accounts of the instance: everything else is read for this account alone.
func (r *UsageReader) Of(ctx context.Context, accountId string) (*Usage, error) {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, err
	}

	usage := &Usage{}
	if err := r.readSources(ctx, accountUuid, usage); err != nil {
		return nil, err
	}
	usage.FeaturesInUse, err = r.featuresInUse(ctx, accountUuid)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

func (r *UsageReader) readSources(ctx context.Context, accountUuid pgtype.UUID, usage *Usage) error {
	jobs, err := r.db.Q.ListJobSourcesOfInstance(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to list the sources of the instance: %w", err)
	}
	connections, err := r.db.Q.GetConnectionsByAccount(ctx, r.db.Db, accountUuid)
	if err != nil {
		return fmt.Errorf("unable to get the connections of the account: %w", err)
	}
	// The names are the account's connections' only: a source that names the connection of
	// another account is listed without one.
	names := make(map[string]string, len(connections))
	for i := range connections {
		names[husonymdb.UUIDString(connections[i].ID)] = connections[i].Name
	}

	sources := SourcesOf(jobs)
	usage.SourcesInInstance = len(sources)
	account := husonymdb.UUIDString(accountUuid)
	for _, source := range sources {
		if source.AccountId != account {
			continue
		}
		usage.SourcesInAccount = append(usage.SourcesInAccount, AccountSource{
			ConnectionId:   source.ConnectionId,
			ConnectionName: names[source.ConnectionId],
			Database:       source.Database,
		})
	}
	return nil
}

// featuresInUse lists the features the account uses, as far as what is stored tells. mcp,
// mapping_review and run_logs leave nothing stored to tell their use by: they are never listed.
func (r *UsageReader) featuresInUse(ctx context.Context, accountUuid pgtype.UUID) ([]license.Feature, error) {
	used := map[license.Feature]bool{}
	if err := r.markJobFeatures(ctx, accountUuid, used); err != nil {
		return nil, err
	}

	hooks, err := r.db.Q.GetAccountHooksByAccount(ctx, r.db.Db, accountUuid)
	if err != nil {
		return nil, fmt.Errorf("unable to get the hooks of the account: %w", err)
	}
	for i := range hooks {
		// As for the hooks of a job, a disabled one does not run.
		if hooks[i].Enabled {
			used[license.FeatureAccountHooks] = true
		}
	}

	apiKeys, err := r.db.Q.GetAccountApiKeys(ctx, r.db.Db, accountUuid)
	if err != nil {
		return nil, fmt.Errorf("unable to get the API keys of the account: %w", err)
	}
	used[license.FeatureApiKeys] = len(apiKeys) > 0

	_, err = r.db.Q.GetAccountOidcProvider(ctx, r.db.Db, accountUuid)
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, fmt.Errorf("unable to get the identity provider of the account: %w", err)
	}
	used[license.FeatureSso] = err == nil

	account := rbac.NewAccount(husonymdb.UUIDString(accountUuid))
	members, err := rbac.NewAccounts(r.db.Q, r.db.Db).HumanMembers(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("unable to get the members of the account: %w", err)
	}
	// Every member being an administrator is what an account has without the feature. A member
	// who holds no role was given none.
	for _, role := range r.roles.Roles(members, account) {
		if role != mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN {
			used[license.FeatureRbac] = true
		}
	}

	var features []license.Feature
	for _, feature := range license.AllFeatures() {
		if used[feature] {
			features = append(features, feature)
		}
	}
	return features, nil
}

// markJobFeatures marks what the jobs of the account use: what the job gate asks the license
// for when one of them starts, and scheduling, which the gate cannot tell when a run starts
// but the stored schedule does.
func (r *UsageReader) markJobFeatures(ctx context.Context, accountUuid pgtype.UUID, used map[license.Feature]bool) error {
	jobs, err := r.db.Q.GetJobsByAccount(ctx, r.db.Db, accountUuid)
	if err != nil {
		return fmt.Errorf("unable to get the jobs of the account: %w", err)
	}
	for i := range jobs {
		dbJob := &jobs[i]
		if cron := dbJob.CronSchedule.String; cron != "" && cron != job_util.UnscheduledCron {
			used[license.FeatureScheduling] = true
		}

		// The destinations are left out: no feature is told by them, and they are one more
		// query per job.
		job, err := dtomaps.ToJobDto(dbJob, nil)
		if err != nil {
			// One job that cannot be read must not hide what every other one uses.
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).ErrorContext(
				ctx,
				"a job could not be read and is left out of what the account uses of the license",
				"jobId", husonymdb.UUIDString(dbJob.ID), "error", err,
			)
			continue
		}
		features, err := featuresOfJob(ctx, r.db, r.db.Db, job)
		if err != nil {
			return err
		}
		for _, feature := range features {
			used[feature] = true
		}
	}
	return nil
}
