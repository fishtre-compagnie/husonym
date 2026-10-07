package licensegate

import (
	"encoding/json"
	"slices"
	"strings"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// Source is a database that a synchronization job reads, as the license counts it. Database is
// empty except for MySQL and MongoDB, where one connection can serve several databases.
type Source struct {
	AccountId    string
	ConnectionId string
	Database     string
}

// SourcesOf lists the distinct sources the given jobs read, sorted by account, connection and
// database.
//
// Only synchronization jobs count: a job that detects PII reads a sample of a source it does not
// move, and a generation job reads nothing (its optional foreign key source is only read for the
// shape of a schema). A connection seen without a database by one job and with one by another
// is two entries: the first job does not say which database it reads.
func SourcesOf(jobs []db_queries.ListJobSourcesOfInstanceRow) []Source {
	seen := map[Source]struct{}{}
	for _, job := range jobs {
		if !isSynchronization(job.JobtypeConfig) {
			continue
		}
		account := husonymdb.UUIDString(job.AccountID)
		for _, source := range sourcesOfJob(job.ConnectionOptions, job.Schemas) {
			source.AccountId = account
			seen[source] = struct{}{}
		}
	}

	sources := make([]Source, 0, len(seen))
	for source := range seen {
		sources = append(sources, source)
	}
	slices.SortFunc(sources, func(a, b Source) int {
		if c := strings.Compare(a.AccountId, b.AccountId); c != 0 {
			return c
		}
		if c := strings.Compare(a.ConnectionId, b.ConnectionId); c != 0 {
			return c
		}
		return strings.Compare(a.Database, b.Database)
	})
	return sources
}

// isSynchronization tells whether a job of this stored type moves data from its source. A job
// whose type is missing or cannot be read is a synchronization job, the default type: one that
// cannot be classified must count rather than hide.
func isSynchronization(jobtypeConfig []byte) bool {
	config := &mgmtv1alpha1.JobTypeConfig{}
	if err := json.Unmarshal(jobtypeConfig, config); err != nil {
		return true
	}
	return config.GetPiiDetect() == nil
}

// sourcesOfJob gives the sources of one job, without their account.
func sourcesOfJob(options *pg_models.JobSourceOptions, schemas []string) []Source {
	if options == nil {
		return nil
	}
	// A new engine in JobSourceOptions must be added here, and to the engines of the query that
	// give schemas if it has databases: the test on the fields of the struct fails until it is.
	switch {
	case options.PostgresOptions != nil:
		return perConnection(options.PostgresOptions.ConnectionId)
	case options.MssqlOptions != nil:
		return perConnection(options.MssqlOptions.ConnectionId)
	case options.DynamoDBOptions != nil:
		return perConnection(options.DynamoDBOptions.ConnectionId)
	case options.MysqlOptions != nil:
		return perSchema(options.MysqlOptions.ConnectionId, schemas)
	case options.MongoDbOptions != nil:
		return perSchema(options.MongoDbOptions.ConnectionId, schemas)
	}
	// Generation jobs, and jobs with no source options.
	return nil
}

func perConnection(connectionId string) []Source {
	if connectionId == "" {
		return nil
	}
	return []Source{{ConnectionId: connectionId}}
}

// perSchema gives one source per distinct schema the job maps, or the connection alone when it
// maps none. For MongoDB a mapping's schema is the database name. The query hands the schemas
// over already distinct and without empty names.
func perSchema(connectionId string, schemas []string) []Source {
	if connectionId == "" {
		return nil
	}
	if len(schemas) == 0 {
		return perConnection(connectionId)
	}
	sources := make([]Source, 0, len(schemas))
	for _, schema := range schemas {
		sources = append(sources, Source{ConnectionId: connectionId, Database: schema})
	}
	return sources
}
