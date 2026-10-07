package usagereport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
)

// jobsRead is what the jobs of the instance tell: the counts of the report, and the connections
// their sources name, by id, for the counts of connections.
type jobsRead struct {
	jobs telemetry.Jobs
	// system counts the columns each system transformer runs on, sorted by name.
	system             []telemetry.TransformerColumns
	userDefinedColumns int
	// sourceOfJob gives, by job id, the connection the job reads. A job that reads none is not
	// in it.
	sourceOfJob map[string]string
	// sourceConnections holds every connection the source options of a job name.
	sourceConnections map[string]bool
}

// errJobIncomplete is a stored job without source options, or with a mapping that is null or has
// no transformer. The product never stores one, and reading one as it reads a job would panic.
var errJobIncomplete = errors.New("a job has no source options, or a mapping that is null or has no transformer")

// readJobs counts the jobs. A job that cannot be read is left out of every count and logged by
// its id: one such job must not hide the others.
func readJobs(ctx context.Context, rows []db_queries.ListJobsOfInstanceForUsageRow) *jobsRead {
	read := &jobsRead{sourceOfJob: map[string]string{}, sourceConnections: map[string]bool{}}
	kinds := map[telemetry.JobKindCount]int{}
	columnsOf := map[string]int{}

	for i := range rows {
		row := &rows[i]
		stored, job, err := decodeJob(row)
		if err != nil {
			// The error is not logged: one of decoding can quote a piece of what it read.
			leftOut(ctx, "a job could not be read and is left out of the usage report", "jobId", husonymdb.UUIDString(row.ID))
			continue
		}

		cron := row.CronSchedule.String
		kinds[telemetry.JobKindCount{
			Kind:      telemetry.JobKind(string(usagestore.KindOfJob(stored))),
			Scheduled: cron != "" && cron != job_util.UnscheduledCron,
		}]++

		type table struct{ schema, name string }
		tables := map[table]struct{}{}
		for _, mapping := range job.GetMappings() {
			tables[table{mapping.GetSchema(), mapping.GetTable()}] = struct{}{}
			names, userDefined := transformersOf(mapping.GetTransformer().GetConfig())
			for name := range names {
				columnsOf[name]++
			}
			if userDefined {
				read.userDefinedColumns++
			}
		}
		read.jobs.Tables += len(tables)
		read.jobs.Columns += len(job.GetMappings())
		if licensegate.UsesSubsetting(job) {
			read.jobs.WithSubset++
		}

		id := husonymdb.UUIDString(row.ID)
		source, named := sourceConnectionsOf(stored.ConnectionOptions)
		if source != "" {
			read.sourceOfJob[id] = source
		}
		for _, connection := range named {
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
	return read
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
	if len(row.ConnectionOptions) > 0 {
		if err := json.Unmarshal(row.ConnectionOptions, &stored.ConnectionOptions); err != nil {
			return nil, nil, err
		}
	}
	if stored.ConnectionOptions == nil {
		return nil, nil, errJobIncomplete
	}
	if len(row.Mappings) > 0 {
		if err := json.Unmarshal(row.Mappings, &stored.Mappings); err != nil {
			return nil, nil, err
		}
	}
	for _, mapping := range stored.Mappings {
		if mapping == nil || mapping.JobMappingTransformer == nil {
			return nil, nil, errJobIncomplete
		}
	}
	job, err := dtomaps.ToJobDto(stored, nil)
	if err != nil {
		return nil, nil, err
	}
	return stored, job, nil
}

// transformersOf gives what the transformer of a column runs: the names of the system
// transformers, each once, and whether one of them is defined by the user. What a PII text
// hands its findings to runs on the column as well. Of a user-defined transformer only the fact
// is kept; a configuration that sets no transformer gives nothing.
func transformersOf(config *mgmtv1alpha1.TransformerConfig) (system map[string]struct{}, userDefined bool) {
	system = map[string]struct{}{}
	for _, run := range job_util.TransformerConfigsRun(config) {
		if source, ok := catalog.SourceOf(run); ok {
			system[telemetry.TransformerName(source)] = struct{}{}
			continue
		}
		if run.GetUserDefinedTransformerConfig() != nil {
			userDefined = true
		}
	}
	return system, userDefined
}

// sourceConnectionsOf gives the connection a job reads, and every connection its source options
// name. A generation reads the database it takes the shape of its foreign keys from, when it has
// one; a generation by a model names the model too, which is what it reads when it has no such
// database. Either is empty for a job that names none.
func sourceConnectionsOf(options *pg_models.JobSourceOptions) (source string, named []string) {
	switch {
	case options.PostgresOptions != nil:
		source = options.PostgresOptions.ConnectionId
	case options.MysqlOptions != nil:
		source = options.MysqlOptions.ConnectionId
	case options.MssqlOptions != nil:
		source = options.MssqlOptions.ConnectionId
	case options.MongoDbOptions != nil:
		source = options.MongoDbOptions.ConnectionId
	case options.DynamoDBOptions != nil:
		source = options.DynamoDBOptions.ConnectionId
	case options.GenerateOptions != nil:
		source = valueOf(options.GenerateOptions.FkSourceConnectionId)
	case options.AiGenerateOptions != nil:
		model := options.AiGenerateOptions.AiConnectionId
		source = cmp.Or(valueOf(options.AiGenerateOptions.FkSourceConnectionId), model)
		if model != "" && model != source {
			named = append(named, model)
		}
	}
	if source != "" {
		named = append(named, source)
	}
	return source, named
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
