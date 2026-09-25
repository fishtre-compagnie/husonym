package benthosbuilder_internal

import (
	"context"
	"fmt"
	"log/slog"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/metrics"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	bb_shared "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/shared"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	tablesync_shared "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/shared"
)

// Determines SQL driver from connection type
func GetSqlDriverByConnectionType(connectionType bb_shared.ConnectionType) (string, error) {
	switch connectionType {
	case bb_shared.ConnectionTypePostgres:
		return sqlmanager_shared.PostgresDriver, nil
	case bb_shared.ConnectionTypeMysql:
		return sqlmanager_shared.MysqlDriver, nil
	case bb_shared.ConnectionTypeMssql:
		return sqlmanager_shared.MssqlDriver, nil
	default:
		return "", fmt.Errorf("unsupported SQL connection type: %s", connectionType)
	}
}

// JobType represents the type of job
type JobType string

const (
	JobTypeSync       JobType = "sync"
	JobTypeGenerate   JobType = "generate"
	JobTypeAIGenerate JobType = "ai-generate"
)

// Determines type of job from Job
func GetJobType(job *mgmtv1alpha1.Job) JobType {
	switch job.GetSource().GetOptions().GetConfig().(type) {
	case *mgmtv1alpha1.JobSourceOptions_Postgres,
		*mgmtv1alpha1.JobSourceOptions_Mysql,
		*mgmtv1alpha1.JobSourceOptions_Mssql,
		*mgmtv1alpha1.JobSourceOptions_Mongodb,
		*mgmtv1alpha1.JobSourceOptions_Dynamodb,
		*mgmtv1alpha1.JobSourceOptions_AwsS3:
		return JobTypeSync
	case *mgmtv1alpha1.JobSourceOptions_Generate:
		return JobTypeGenerate
	case *mgmtv1alpha1.JobSourceOptions_AiGenerate:
		return JobTypeAIGenerate
	default:
		return ""
	}
}

// Handles both source (input) and destination (output) configurations for different
// connection types (postgres, mysql...) and job types (e.g., sync, generate...).
type BenthosBuilder interface {
	// BuildSourceConfigs generates Benthos source configurations for reading and processing data.
	// Returns a config for each schema.table in job mappings
	BuildSourceConfigs(ctx context.Context, params *SourceParams) ([]*BenthosSourceConfig, error)
	// BuildDestinationConfig creates a Benthos destination configuration for writing processed data.
	// Returns single config for a schema.table configuration
	BuildDestinationConfig(
		ctx context.Context,
		params *DestinationParams,
	) (*BenthosDestinationConfig, error)
}

// SourceParams contains all parameters needed to build a source benthos configuration
type SourceParams struct {
	Job              *mgmtv1alpha1.Job
	JobRunId         string
	SourceConnection *mgmtv1alpha1.Connection
	Logger           *slog.Logger

	// MappingChanges is an output, filled by the SQL builder: how the job's mappings differ from
	// the source it read. The caller writes them to the job, so that the job keeps mirroring its
	// source instead of each run re-deciding the same columns. It lives here rather than in the
	// return value because every other builder would have to return an empty one.
	MappingChanges MappingChanges

	// HasConsistencyKey says whether the deployment can derive a key for deterministic
	// pseudonymization. The strategy for new columns reads it: it does not choose for a
	// column an option that needs a key the run will not have.
	HasConsistencyKey bool

	// UsesAthanor says which engine runs the job: what the run meets depends on it.
	UsesAthanor bool
	// Findings is an output, filled by the SQL builder: what the plan of the tables tells of
	// the run (see internal/preflight).
	Findings []*preflight.Finding
}

// MappingChanges is what a run changes in its job's mappings.
type MappingChanges struct {
	// Mappings for the columns the source has and the job did not map, as the job's strategy
	// for new columns chose them
	Added []*mgmtv1alpha1.JobMapping
	// The job's mappings whose column the source no longer has
	Removed []*mgmtv1alpha1.JobMapping
	// Every column of the tables the job syncs, with its type as the source reports it
	Columns []*mgmtv1alpha1.JobSourceColumn
	// Whether the strategy asks for the changes to be reviewed (AutoMap & Review)
	RecordChanges bool
}

type ReferenceKey struct {
	Table  string
	Column string
}

// DestinationParams contains all parameters needed to build a destination benthos configuration
type DestinationParams struct {
	SourceConfig    *BenthosSourceConfig
	Job             *mgmtv1alpha1.Job
	JobRunId        string
	DestinationOpts *mgmtv1alpha1.JobDestinationOptions
	DestConnection  *mgmtv1alpha1.Connection
	Logger          *slog.Logger

	// UsesAthanor says which engine runs the job: what the run meets depends on it.
	UsesAthanor bool
	// Findings is an output, filled by the SQL builder: what the destination is to receive
	// of the table and may refuse (see internal/preflight).
	Findings []*preflight.Finding
}

// BenthosSourceConfig represents a Benthos source configuration
type BenthosSourceConfig struct {
	Config                  *husonym_benthos.BenthosConfig
	Name                    string
	DependsOn               []*runconfigs.DependsOn
	RunType                 runconfigs.RunType
	TableSchema             string
	TableName               string
	Columns                 []string
	RedisDependsOn          map[string][]string
	ColumnDefaultProperties map[string]*husonym_benthos.ColumnDefaultProperties
	Processors              []*husonym_benthos.ProcessorConfig
	BenthosDsns             []*bb_shared.BenthosDsn
	RedisConfig             []*bb_shared.BenthosRedisConfig
	PrimaryKeys             []string
	Metriclabels            metrics.MetricLabels
	ColumnIdentityCursors   map[string]*tablesync_shared.IdentityCursor
	// ForeignKeys are the foreign keys of the table, for the engine-neutral plan.
	ForeignKeys []*tableplan.ForeignKey
	// GeneratedColumns are the columns the destination computes itself.
	GeneratedColumns []string
	// PublishedKeys are the transformed columns other tables reference.
	PublishedKeys []*tableplan.PublishedKey
}

// BenthosDestinationConfig represents a Benthos destination configuration
type BenthosDestinationConfig struct {
	Outputs     []husonym_benthos.Outputs
	BenthosDsns []*bb_shared.BenthosDsn
}
