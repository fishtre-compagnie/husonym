// Package usagereport prepares the usage report of the instance from what its database holds.
//
// Everything it reads that a customer entered is dropped or counted. A text reaches a block of
// the report only through a function of the telemetry package, which answers with a member of a
// closed list.
package usagereport

import (
	"cmp"
	"context"
	"encoding/json"
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
	// A job that reads no connection, or whose connection is gone or cannot be read, is not in it.
	SourceTypeOfJob map[string]string
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
// A job, a connection or an account that cannot be read is left out and logged by its id; a
// query that fails fails the reading.
func (r *InventoryReader) Read(ctx context.Context, now time.Time) (*Inventory, error) {
	inventory := &Inventory{}
	if err := r.readJobsAndConnections(ctx, inventory); err != nil {
		return nil, err
	}

	columnTypeRows, err := r.db.Q.CountSourceColumnTypesOfInstance(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to count the column types of the instance: %w", err)
	}
	inventory.ColumnTypes = columnTypes(columnTypeRows)

	userDefined, err := r.db.Q.CountUserDefinedTransformersOfInstance(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to count the user-defined transformers of the instance: %w", err)
	}
	inventory.Transformers.UserDefined = int(userDefined)

	inventory.AccountOidcProviders, err = r.db.Q.CountAccountOidcProviders(ctx, r.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to count the identity providers of the accounts: %w", err)
	}

	if err := r.readAccounts(ctx, inventory); err != nil {
		return nil, err
	}
	if err := r.countUsers(ctx, now, inventory); err != nil {
		return nil, err
	}
	return inventory, nil
}

func (r *InventoryReader) readJobsAndConnections(ctx context.Context, inventory *Inventory) error {
	jobRows, err := r.db.Q.ListJobsOfInstanceForUsage(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to list the jobs of the instance: %w", err)
	}
	connectionRows, err := r.db.Q.ListConnectionsOfInstance(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to list the connections of the instance: %w", err)
	}
	destinationRows, err := r.db.Q.ListJobDestinationsOfInstance(ctx, r.db.Db)
	if err != nil {
		return fmt.Errorf("unable to list the destinations of the instance: %w", err)
	}

	jobs := readJobs(ctx, jobRows)
	inventory.Jobs = jobs.jobs
	inventory.Transformers.System = jobs.system
	inventory.Transformers.UserDefinedColumns = jobs.userDefinedColumns

	destinations := make(map[string]bool, len(destinationRows))
	for _, row := range destinationRows {
		destinations[husonymdb.UUIDString(row.ConnectionID)] = true
	}
	types := connectionTypes(ctx, connectionRows)
	inventory.Connections = countConnections(types, jobs.sourceConnections, destinations)
	inventory.SourceTypeOfJob = sourceTypes(jobs.sourceOfJob, types)
	return nil
}

// readAccounts gathers what is told account by account: the features in use, in any of them,
// and the roles their members hold.
func (r *InventoryReader) readAccounts(ctx context.Context, inventory *Inventory) error {
	accounts := rbac.NewAccounts(r.db.Q, r.db.Db)
	all, err := accounts.Accounts(ctx)
	if err != nil {
		return fmt.Errorf("unable to list the accounts of the instance: %w", err)
	}

	used := map[license.Feature]bool{}
	roles := map[string]int{}
	for _, account := range all {
		// Neither error is logged: both can carry what a decoding quoted of a stored value.
		usage, err := r.usage.Of(ctx, account.String())
		if err != nil {
			leftOut(ctx, "what an account uses could not be read and is left out of the usage report", "accountId", account.String())
		} else {
			for _, feature := range usage.FeaturesInUse {
				used[feature] = true
			}
		}

		members, err := accounts.HumanMembers(ctx, account)
		if err != nil {
			leftOut(ctx, "the members of an account could not be read and are left out of the usage report", "accountId", account.String())
			continue
		}
		countRoles(roles, members, r.roles.Roles(members, account))
	}
	inventory.Features = featureUses(used)
	inventory.Users.ByRole = roleCounts(roles)
	return nil
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
// A connection that cannot be read is left out and logged by its id.
func connectionTypes(ctx context.Context, rows []db_queries.ListConnectionsOfInstanceRow) map[string]string {
	types := make(map[string]string, len(rows))
	for _, row := range rows {
		name, err := connectionTypeOf(row.ConnectionConfig)
		if err != nil {
			// The error is not logged: one of decoding can quote a piece of what it read.
			leftOut(ctx, "a connection could not be read and is left out of the usage report", "connectionId", husonymdb.UUIDString(row.ID))
			continue
		}
		types[husonymdb.UUIDString(row.ID)] = name
	}
	return types
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

// sourceTypes gives, by job id, the type of the connection the job reads.
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

// leftOut logs what is left out of the report, by its id and nothing else of it.
func leftOut(ctx context.Context, message, key, id string) {
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(ctx, message, key, id)
}
