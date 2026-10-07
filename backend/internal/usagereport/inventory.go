// Package usagereport prepares the usage report of the instance from what its database holds.
//
// Everything it reads that a customer entered is dropped or counted. A text read from the
// database reaches a block of the report only through a function of the telemetry package, which
// answers with a member of a closed list; the only other texts are the names of the license
// features, which are constants of the code.
package usagereport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
)

// activeWindow is how far back a user counts as active.
const activeWindow = 30 * 24 * time.Hour

// Inventory is what the instance holds when the report is prepared, as the blocks of the report
// that tell a state. Every array is sorted by its keys.
type Inventory struct {
	Connections  []telemetry.ConnectionCount
	Jobs         telemetry.Jobs
	Transformers telemetry.Transformers
	ColumnTypes  []telemetry.ColumnTypeCount
	Features     []telemetry.FeatureUse
	Users        telemetry.Users
	// AccountOidcProviders is how many accounts declared an identity provider of their own.
	AccountOidcProviders int64
	// SourceTypeOfJob gives the connection type of the source of each job, for the run counters.
	// Only a database is named: a job that reads none, as a generation without a database to
	// take its foreign keys from, or whose connection is gone or cannot be read, is not in it.
	SourceTypeOfJob map[string]string
	// Unread counts what was left out of everything above because it could not be read.
	Unread Unread
}

// Unread counts the jobs, the connections and the accounts that could not be read. A job or a
// connection is then in no count; an account is left out of the features it uses by itself and,
// when its members cannot be read, of the roles.
type Unread struct {
	Jobs, Connections, Accounts int64
}

// UsersSeen counts the users seen since a day. *usagestore.Store is one.
type UsersSeen interface {
	UsersSeenSince(ctx context.Context, since time.Time) (int64, error)
}

// InventoryReader reads what the instance holds, across every account.
type InventoryReader struct {
	db          *husonymdb.HusonymDb
	usage       *licensegate.UsageReader
	roles       licensegate.Roles
	seen        UsersSeen
	authEnabled bool
}

func NewInventoryReader(
	db *husonymdb.HusonymDb,
	usage *licensegate.UsageReader,
	roles licensegate.Roles,
	seen UsersSeen,
	authEnabled bool,
) *InventoryReader {
	return &InventoryReader{db: db, usage: usage, roles: roles, seen: seen, authEnabled: authEnabled}
}

// Read gives the inventory of the instance. now is the moment the report is prepared: the users
// active in the last thirty days are counted back from it.
//
// A job, a connection or an account that cannot be read is left out alone, logged by its id and
// counted in Unread; a query on the whole instance that fails fails the reading.
func (r *InventoryReader) Read(ctx context.Context, now time.Time) (*Inventory, error) {
	inventory := &Inventory{}
	used, err := r.readJobsAndConnections(ctx, inventory)
	if err != nil {
		return nil, err
	}

	userDefined, err := r.db.Q.CountUserDefinedTransformersOfInstance(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to count the user-defined transformers of the instance: %w", err)
	}
	inventory.Transformers.UserDefined = int(userDefined)

	inventory.AccountOidcProviders, err = r.db.Q.CountAccountOidcProviders(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to count the identity providers of the accounts: %w", err)
	}

	if err := r.readAccounts(ctx, used, inventory); err != nil {
		return nil, err
	}
	inventory.Features = featureUses(used)
	if err := r.countUsers(ctx, now, inventory); err != nil {
		return nil, err
	}
	return inventory, nil
}

// readJobsAndConnections fills what the jobs and the connections tell, and returns the features
// the jobs use.
func (r *InventoryReader) readJobsAndConnections(ctx context.Context, inventory *Inventory) (map[license.Feature]bool, error) {
	jobRows, err := r.db.Q.ListJobsOfInstanceForUsage(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to list the jobs of the instance: %w", err)
	}
	connectionRows, err := r.db.Q.ListConnectionsOfInstance(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to list the connections of the instance: %w", err)
	}
	destinationRows, err := r.db.Q.ListJobDestinationsOfInstance(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to list the destinations of the instance: %w", err)
	}

	jobs, err := readJobs(ctx, jobRows, func(ctx context.Context, job *mgmtv1alpha1.Job) ([]license.Feature, error) {
		return licensegate.FeaturesOfJob(ctx, r.db, r.db.Db, job)
	})
	if err != nil {
		return nil, err
	}
	inventory.Jobs = jobs.jobs
	inventory.Transformers.System = jobs.system
	inventory.Transformers.UserDefinedColumns = jobs.userDefinedColumns
	inventory.Unread.Jobs = int64(len(jobs.unread))

	// The columns of a job that is left out are left out with it. The types are all the query
	// reads of them.
	columnTypeRows, err := r.db.Q.CountSourceColumnTypesOfInstance(ctx, r.db.Db, jobs.unread)
	if err != nil {
		return nil, fmt.Errorf("unable to count the column types of the instance: %w", err)
	}
	inventory.ColumnTypes = columnTypes(columnTypeRows)

	types, unread := connectionTypes(ctx, connectionRows)
	inventory.Unread.Connections = unread
	inventory.Connections = countConnections(types, jobs.sourceConnections, destinationsOf(destinationRows, jobs.unread))
	inventory.SourceTypeOfJob = sourceTypes(jobs.sourceOfJob, types)
	return jobs.features, nil
}

// destinationsOf holds the connections a job writes to. What a job that is left out writes to is
// left out with it.
func destinationsOf(rows []db_queries.ListJobDestinationsOfInstanceRow, unreadJobs []pgtype.UUID) map[string]bool {
	destinations := make(map[string]bool, len(rows))
	for _, row := range rows {
		if !slices.Contains(unreadJobs, row.JobID) {
			destinations[husonymdb.UUIDString(row.ConnectionID)] = true
		}
	}
	return destinations
}

// accountRead is what one account tells by itself, whatever its jobs do.
type accountRead struct {
	features []license.Feature
	members  []rbac.User
	held     map[rbac.User]mgmtv1alpha1.AccountRole
	// featuresErr says why the features could not be read, when the members could.
	featuresErr error
}

// readAccounts gathers what is told account by account: the features an account uses by itself,
// which are added to those its jobs use, and the roles its members hold.
func (r *InventoryReader) readAccounts(ctx context.Context, used map[license.Feature]bool, inventory *Inventory) error {
	accounts := rbac.NewAccounts(r.db.Q, r.db.Db)
	all, err := accounts.Accounts(ctx)
	if err != nil {
		return fmt.Errorf("unable to list the accounts of the instance: %w", err)
	}

	roles := map[string]int{}
	for _, account := range all {
		read, err := guarded(func() (*accountRead, error) { return r.readAccount(ctx, accounts, account) })
		cause := err
		if cause == nil {
			cause = read.featuresErr
		}
		if cause != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// The error is not logged: it can carry what a decoding quoted of a stored value.
			leftOut(ctx, "an account could not be read and is left out of the usage report", "accountId", account.String(), cause)
			inventory.Unread.Accounts++
		}
		if err != nil {
			continue
		}
		for _, feature := range read.features {
			used[feature] = true
		}
		countRoles(roles, read.members, read.held)
	}
	inventory.Users.ByRole = roleCounts(roles)
	return nil
}

// readAccount reads the members of an account, the roles they hold and the features the account
// uses by itself. Without the features the members still count; without the members nothing does.
func (r *InventoryReader) readAccount(ctx context.Context, accounts rbac.Accounts, account rbac.Account) (*accountRead, error) {
	members, err := accounts.HumanMembers(ctx, account)
	if err != nil {
		return nil, err
	}
	read := &accountRead{members: members, held: r.roles.Roles(members, account)}
	read.features, read.featuresErr = guarded(func() ([]license.Feature, error) {
		return r.usage.AccountFeatures(ctx, account.String())
	})
	return read, nil
}

func (r *InventoryReader) countUsers(ctx context.Context, now time.Time, inventory *Inventory) error {
	accounts, err := r.db.Q.CountAccounts(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to count the accounts of the instance: %w", err)
	}
	users, err := r.db.Q.CountUsersOfInstance(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to count the users of the instance: %w", err)
	}
	inventory.Users.Accounts = int(accounts)
	inventory.Users.Users = int(users)

	// Without authentication nobody signs in, and nobody is ever seen.
	if !r.authEnabled {
		return nil
	}
	seen, err := r.seen.UsersSeenSince(ctx, now.Add(-activeWindow))
	if err != nil {
		return fmt.Errorf("unable to count the users seen lately: %w", err)
	}
	active := int(seen)
	inventory.Users.Active30d = &active
	return nil
}

// connectionTypes gives, by connection id, the type of each connection as the report names it.
// A connection that cannot be read is left out, logged by its id and counted.
func connectionTypes(ctx context.Context, rows []db_queries.ListConnectionsOfInstanceRow) (types map[string]string, unread int64) {
	types = make(map[string]string, len(rows))
	for _, row := range rows {
		name, err := guarded(func() (string, error) { return connectionTypeOf(row.ConnectionConfig) })
		if err != nil {
			unread++
			// The error is not logged: one of decoding can quote a piece of what it read.
			leftOut(
				ctx,
				"a connection could not be read and is left out of the usage report",
				"connectionId",
				husonymdb.UUIDString(row.ID),
				err,
			)
			continue
		}
		types[husonymdb.UUIDString(row.ID)] = name
	}
	return types, unread
}

// connectionTypeOf reads the type of a connection out of the configuration the database stores.
// Nothing else of the configuration is kept, and its secrets are masked on the way.
func connectionTypeOf(stored []byte) (string, error) {
	config := &pg_models.ConnectionConfig{}
	if err := json.Unmarshal(stored, config); err != nil {
		return "", err
	}
	dto, err := config.ToDto(false)
	if err != nil {
		return "", err
	}
	return telemetry.ConnectionType(dtomaps.ConnectionTypeName(dto)), nil
}

// countConnections counts the connections by type and by role: source for one the source options
// of a job name, destination for one a job writes to. A connection that is both counts under
// both, and one that is neither is not counted.
func countConnections(types map[string]string, sources, destinations map[string]bool) []telemetry.ConnectionCount {
	counts := map[telemetry.ConnectionCount]int{}
	for id, name := range types {
		if sources[id] {
			counts[telemetry.ConnectionCount{Type: name, Role: telemetry.ConnectionRole("source")}]++
		}
		if destinations[id] {
			counts[telemetry.ConnectionCount{Type: name, Role: telemetry.ConnectionRole("destination")}]++
		}
	}
	connections := make([]telemetry.ConnectionCount, 0, len(counts))
	for connection, count := range counts {
		connection.Count = count
		connections = append(connections, connection)
	}
	slices.SortFunc(connections, func(a, b telemetry.ConnectionCount) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Role, b.Role))
	})
	return connections
}

// sourceTypes gives, by job id, the type of the database the job reads.
func sourceTypes(sourceOfJob, types map[string]string) map[string]string {
	sources := make(map[string]string, len(sourceOfJob))
	for job, connection := range sourceOfJob {
		if name, ok := types[connection]; ok {
			sources[job] = name
		}
	}
	return sources
}

// columnTypes sums the columns by the family of their type.
func columnTypes(rows []db_queries.CountSourceColumnTypesOfInstanceRow) []telemetry.ColumnTypeCount {
	columnsOf := map[string]int{}
	for _, row := range rows {
		columnsOf[telemetry.ColumnTypeFamily(row.DataType)] += int(row.Columns)
	}
	families := make([]telemetry.ColumnTypeCount, 0, len(columnsOf))
	for family, columns := range columnsOf {
		families = append(families, telemetry.ColumnTypeCount{Family: family, Columns: columns})
	}
	slices.SortFunc(families, func(a, b telemetry.ColumnTypeCount) int { return cmp.Compare(a.Family, b.Family) })
	return families
}

// featureUses says, of every feature a license can include, whether it is in use.
func featureUses(used map[license.Feature]bool) []telemetry.FeatureUse {
	features := license.AllFeatures()
	uses := make([]telemetry.FeatureUse, 0, len(features))
	for _, feature := range features {
		uses = append(uses, telemetry.FeatureUse{Name: string(feature), InUse: used[feature]})
	}
	return uses
}

// countRoles adds the members of an account to the counts, each under the role it holds there.
// A member who holds none was given none.
func countRoles(counts map[string]int, members []rbac.User, held map[rbac.User]mgmtv1alpha1.AccountRole) {
	for _, member := range members {
		counts[telemetry.Role(held[member])]++
	}
}

func roleCounts(counts map[string]int) []telemetry.RoleCount {
	roles := make([]telemetry.RoleCount, 0, len(counts))
	for role, count := range counts {
		roles = append(roles, telemetry.RoleCount{Role: role, Count: count})
	}
	slices.SortFunc(roles, func(a, b telemetry.RoleCount) int { return cmp.Compare(a.Role, b.Role) })
	return roles
}

// leftOut logs what is left out of the report, by its id and nothing else of it. Of the cause
// it says one thing: whether the reading panicked, which is a defect of the program to look for
// rather than a damaged row. What the cause says is never logged.
func leftOut(ctx context.Context, message, key, id string, cause error) {
	if errors.Is(cause, errPanicked) {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(ctx, message, key, id, "panicked", true)
		return
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(ctx, message, key, id)
}
