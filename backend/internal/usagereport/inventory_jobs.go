package usagereport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
	"github.com/jackc/pgx/v5/pgtype"
)

// jobsRead is what the jobs of the instance tell: the counts of the report, the features they
// use, and the connections their sources name, by id, for the counts of connections.
type jobsRead struct {
	jobs telemetry.Jobs
	// system counts the columns each system transformer runs on, sorted by name.
	system             []telemetry.TransformerColumns
	userDefinedColumns int
	// features are the licensed features a job uses, scheduling included.
	features map[license.Feature]bool
	// sourceOfJob gives, by job id, the database the job reads. A job that reads none is not
	// in it.
	sourceOfJob map[string]string
	// sourceConnections holds every connection the source options of a job name.
	sourceConnections map[string]bool
	// unread are the jobs left out, which are in none of the above.
	unread []pgtype.UUID
}

// jobFeatures gives the licensed features a job uses beyond its schedule, from its definition and
// from what only the database knows of it. licensegate.FeaturesOfJob is one.
type jobFeatures func(ctx context.Context, job *mgmtv1alpha1.Job) ([]license.Feature, error)

// jobFacts is what one job adds to the counts.
type jobFacts struct {
	kind      telemetry.JobKindCount
	tables    int
	columns   int
	subset    bool
	scheduled bool
	// transformers holds, for each column, the system transformers it runs, each once.
	transformers       [][]string
	userDefinedColumns int
	features           []license.Feature
	source             string
	named              []string
}

var (
	// errJobIncomplete is a stored job without source options, with a mapping that has no
	// transformer, or with a null where the product stores an object. The product never stores
	// one, and reading one as it reads a job would panic.
	errJobIncomplete = errors.New("a job has no source options, a mapping without a transformer, or a null in a list")
	// errPanicked is a reading that panicked. What the panic said is dropped: it can quote what
	// was being read.
	errPanicked = errors.New("the reading panicked")
)

// guarded makes a reading whose panic is an error like any other, so that what is damaged in a
// way nobody foresaw is left out alone too.
func guarded[T any](read func() (T, error)) (value T, err error) {
	defer func() {
		if recover() != nil {
			var none T
			value, err = none, errPanicked
		}
	}()
	return read()
}

// readJobs counts the jobs. A job that cannot be read is left out of every count and logged by
// its id: one such job must not hide the others. It fails only when the context is done.
func readJobs(ctx context.Context, rows []db_queries.ListJobsOfInstanceForUsageRow, features jobFeatures) (*jobsRead, error) {
	read := &jobsRead{
		features:          map[license.Feature]bool{},
		sourceOfJob:       map[string]string{},
		sourceConnections: map[string]bool{},
		// Never nil: the query that leaves their columns out takes it as a list.
		unread: []pgtype.UUID{},
	}
	kinds := map[telemetry.JobKindCount]int{}
	columnsOf := map[string]int{}

	for i := range rows {
		row := &rows[i]
		facts, err := guarded(func() (*jobFacts, error) { return factsOfJob(ctx, row, features) })
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// The error is not logged: one of decoding can quote a piece of what it read.
			leftOut(ctx, "a job could not be read and is left out of the usage report", "jobId", husonymdb.UUIDString(row.ID), err)
			read.unread = append(read.unread, row.ID)
			continue
		}

		kinds[facts.kind]++
		read.jobs.Tables += facts.tables
		read.jobs.Columns += facts.columns
		if facts.subset {
			read.jobs.WithSubset++
		}
		for _, names := range facts.transformers {
			for _, name := range names {
				columnsOf[name]++
			}
		}
		read.userDefinedColumns += facts.userDefinedColumns
		for _, feature := range facts.features {
			read.features[feature] = true
		}
		if facts.scheduled {
			read.features[license.FeatureScheduling] = true
		}
		if facts.source != "" {
			read.sourceOfJob[husonymdb.UUIDString(row.ID)] = facts.source
		}
		for _, connection := range facts.named {
			read.sourceConnections[connection] = true
		}
	}

	for kind, count := range kinds {
		kind.Count = count
		read.jobs.ByKind = append(read.jobs.ByKind, kind)
	}
	slices.SortFunc(read.jobs.ByKind, func(a, b telemetry.JobKindCount) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		// Not scheduled before scheduled, as the report orders them.
		switch {
		case a.Scheduled == b.Scheduled:
			return 0
		case b.Scheduled:
			return -1
		}
		return 1
	})
	for name, columns := range columnsOf {
		read.system = append(read.system, telemetry.TransformerColumns{Name: name, Columns: columns})
	}
	slices.SortFunc(read.system, func(a, b telemetry.TransformerColumns) int { return cmp.Compare(a.Name, b.Name) })
	return read, nil
}

// factsOfJob reads one job and gives what it adds to the counts, or an error and nothing.
func factsOfJob(ctx context.Context, row *db_queries.ListJobsOfInstanceForUsageRow, features jobFeatures) (*jobFacts, error) {
	stored, job, err := decodeJob(row)
	if err != nil {
		return nil, err
	}
	used, err := features(ctx, job)
	if err != nil {
		return nil, err
	}

	cron := row.CronSchedule.String
	facts := &jobFacts{
		scheduled: cron != "" && cron != job_util.UnscheduledCron,
		columns:   len(job.GetMappings()),
		subset:    licensegate.UsesSubsetting(job),
		features:  used,
	}
	facts.kind = telemetry.JobKindCount{
		Kind:      telemetry.JobKind(string(usagestore.KindOfJob(stored))),
		Scheduled: facts.scheduled,
	}

	type table struct{ schema, name string }
	tables := map[table]struct{}{}
	for _, mapping := range job.GetMappings() {
		tables[table{mapping.GetSchema(), mapping.GetTable()}] = struct{}{}
		names, userDefined := transformersOf(mapping.GetTransformer().GetConfig())
		facts.transformers = append(facts.transformers, names)
		if userDefined {
			facts.userDefinedColumns++
		}
	}
	facts.tables = len(tables)
	facts.source, facts.named = sourceConnectionsOf(stored.ConnectionOptions)
	return facts, nil
}

// decodeJob reads a job out of the JSON the database stores, through the models the product
// writes it with, and as the job service reads it.
func decodeJob(row *db_queries.ListJobsOfInstanceForUsageRow) (*db_queries.HusonymApiJob, *mgmtv1alpha1.Job, error) {
	stored := &db_queries.HusonymApiJob{
		ID:            row.ID,
		AccountID:     row.AccountID,
		JobtypeConfig: row.JobtypeConfig,
		CronSchedule:  row.CronSchedule,
	}
	// A type that cannot be read does not hide the job: it is read as a job with no type, which
	// is a synchronization, as usagestore.KindOfJob and the license have it.
	if json.Unmarshal(row.JobtypeConfig, &mgmtv1alpha1.JobTypeConfig{}) != nil {
		stored.JobtypeConfig = nil
	}
	if len(row.ConnectionOptions) > 0 {
		if err := json.Unmarshal(row.ConnectionOptions, &stored.ConnectionOptions); err != nil {
			return nil, nil, err
		}
	}
	if len(row.Mappings) > 0 {
		if err := json.Unmarshal(row.Mappings, &stored.Mappings); err != nil {
			return nil, nil, err
		}
	}
	if stored.ConnectionOptions == nil ||
		holdsNull(reflect.ValueOf(stored.ConnectionOptions)) || holdsNull(reflect.ValueOf(stored.Mappings)) {
		return nil, nil, errJobIncomplete
	}
	for _, mapping := range stored.Mappings {
		if mapping.JobMappingTransformer == nil {
			return nil, nil, errJobIncomplete
		}
	}
	job, err := dtomaps.ToJobDto(stored, nil)
	if err != nil {
		return nil, nil, err
	}
	return stored, job, nil
}

// holdsNull tells whether a list, anywhere in what was decoded, holds a null where the models
// expect an object: the schemas of a source, the tables of a schema, the mappings. The models
// read each element without asking, and would panic on it.
func holdsNull(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Pointer:
		return !value.IsNil() && holdsNull(value.Elem())
	case reflect.Struct:
		for i := range value.NumField() {
			if holdsNull(value.Field(i)) {
				return true
			}
		}
	case reflect.Slice:
		for i := range value.Len() {
			element := value.Index(i)
			if element.Kind() == reflect.Pointer && element.IsNil() {
				return true
			}
			if holdsNull(element) {
				return true
			}
		}
	}
	return false
}

// transformersOf gives what the transformer of a column runs: the names of the system
// transformers, each once and sorted, and whether one of them is defined by the user. What a PII
// text hands its findings to runs on the column as well. Of a user-defined transformer only the
// fact is kept; a configuration that sets no transformer gives nothing.
func transformersOf(config *mgmtv1alpha1.TransformerConfig) (system []string, userDefined bool) {
	for _, run := range job_util.TransformerConfigsRun(config) {
		if source, ok := catalog.SourceOf(run); ok {
			system = append(system, telemetry.TransformerName(source))
			continue
		}
		if run.GetUserDefinedTransformerConfig() != nil {
			userDefined = true
		}
	}
	slices.Sort(system)
	return slices.Compact(system), userDefined
}

// sourceConnectionsOf gives the database a job reads, and every connection its source options
// name. A generation reads the database it takes the shape of its foreign keys from, when it has
// one, and no database otherwise; the model of a generation by a model is a connection its
// source names, and not a database. Either is empty for a job that names none.
func sourceConnectionsOf(options *pg_models.JobSourceOptions) (database string, named []string) {
	switch {
	case options.PostgresOptions != nil:
		database = options.PostgresOptions.ConnectionId
	case options.MysqlOptions != nil:
		database = options.MysqlOptions.ConnectionId
	case options.MssqlOptions != nil:
		database = options.MssqlOptions.ConnectionId
	case options.MongoDbOptions != nil:
		database = options.MongoDbOptions.ConnectionId
	case options.DynamoDBOptions != nil:
		database = options.DynamoDBOptions.ConnectionId
	case options.GenerateOptions != nil:
		database = valueOf(options.GenerateOptions.FkSourceConnectionId)
	case options.AiGenerateOptions != nil:
		database = valueOf(options.AiGenerateOptions.FkSourceConnectionId)
		if model := options.AiGenerateOptions.AiConnectionId; model != "" {
			named = append(named, model)
		}
	}
	if database != "" {
		named = append(named, database)
	}
	return database, named
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
