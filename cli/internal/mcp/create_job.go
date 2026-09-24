package mcp_server

import (
	"context"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type createJobInput struct {
	Name               string             `json:"name"                  jsonschema:"unique in the account: 3 to 100 lowercase letters, digits and dashes"`
	SourceConnectionId string             `json:"source_connection_id"  jsonschema:"the connection to read from, PostgreSQL or MySQL, as list_connections gives it"`
	Destinations       []destinationInput `json:"destinations"          jsonschema:"the connections to write into, PostgreSQL or MySQL"`
	Mappings           []mappingInput     `json:"mappings"              jsonschema:"one per column of every table the job reads, passthrough included"`
	NewColumns         string             `json:"new_columns,omitempty" jsonschema:"what a run does with a column that appears later in a mapped table: halt (the default) stops the run; auto_map maps it as the PII detection suggests and records the change for review — and copies it as it is when the detection suggests nothing or the column is in a key"`
}

type destinationInput struct {
	ConnectionId         string `json:"connection_id"`
	TruncateBeforeInsert bool   `json:"truncate_before_insert,omitempty" jsonschema:"empty each table before writing into it; the person is told when asked to run the job"`
}

type createJobOutput struct {
	JobId   string   `json:"job_id"`
	Name    string   `json:"name"`
	Tables  []string `json:"tables"  jsonschema:"the tables the job reads, as schema.table"`
	Columns int      `json:"columns" jsonschema:"how many columns are mapped"`
}

func addCreateJob(server *mcp.Server, connections *maskedconn.Reader, data *novalues.Reader, jobReader *jobs.Reader) {
	openWorld, destructive := false, false
	mcp.AddTool(server, &mcp.Tool{
		Name: "create_job",
		Description: "Create a job that copies tables from a source to destinations, each column through the " +
			"transformer given. Every column of every table read takes a mapping, passthrough included; " +
			"a column the database fills itself (generated, identity) takes generate_default, or " +
			"passthrough for an identity. " +
			"The job has no schedule and does not run: run_job runs it, once the person agrees.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &openWorld},
	}, createJob(connections, data, jobReader))
}

func createJob(
	connections *maskedconn.Reader,
	data *novalues.Reader,
	jobReader *jobs.Reader,
) mcp.ToolHandlerFor[createJobInput, createJobOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input createJobInput,
	) (*mcp.CallToolResult, createJobOutput, error) {
		source, sourceConn, err := sqlKindOf(ctx, connections, input.SourceConnectionId)
		if err != nil {
			return nil, createJobOutput{}, err
		}
		newColumns, err := newColumnStrategy(input.NewColumns)
		if err != nil {
			return nil, createJobOutput{}, err
		}
		destinations, err := buildDestinations(ctx, connections, sourceConn, input.Destinations)
		if err != nil {
			return nil, createJobOutput{}, err
		}

		columns, err := data.Columns(ctx, input.SourceConnectionId)
		if err != nil {
			return nil, createJobOutput{}, fmt.Errorf(
				"unable to read the schema of connection %s: %w", input.SourceConnectionId, err,
			)
		}
		mappings, err := buildMappings(ctx, data, columns, input.Mappings)
		if err != nil {
			return nil, createJobOutput{}, err
		}
		if err := checkComplete(mappings, columns); err != nil {
			return nil, createJobOutput{}, err
		}
		jobSource := &mgmtv1alpha1.JobSource{Options: sourceOptions(source, input.SourceConnectionId, newColumns)}
		if err := checkMappings(
			ctx, data, input.SourceConnectionId, jobSource, mappings, nil, nil,
		); err != nil {
			return nil, createJobOutput{}, err
		}

		job, err := jobReader.Create(ctx, &mgmtv1alpha1.CreateJobRequest{
			JobName:      input.Name,
			Mappings:     mappings,
			Source:       jobSource,
			Destinations: destinations,
			JobType: &mgmtv1alpha1.JobTypeConfig{
				JobType: &mgmtv1alpha1.JobTypeConfig_Sync{Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{}},
			},
		})
		if err != nil {
			return nil, createJobOutput{}, fmt.Errorf("unable to create the job: %w", err)
		}
		return nil, createJobOutput{
			JobId:   job.GetId(),
			Name:    job.GetName(),
			Tables:  mappedTables(mappings),
			Columns: len(mappings),
		}, nil
	}
}

// sqlKind is the kind of database a job can be configured on here.
type sqlKind int

const (
	postgres sqlKind = iota + 1
	mysql
)

// sqlKindOf reads a connection and says whether it is PostgreSQL or MySQL, the two a job is
// configured on here.
func sqlKindOf(
	ctx context.Context,
	connections *maskedconn.Reader,
	connectionId string,
) (sqlKind, *mgmtv1alpha1.Connection, error) {
	conn, err := connections.Get(ctx, connectionId)
	if err != nil {
		return 0, nil, fmt.Errorf("unable to read connection %s: %w", connectionId, err)
	}
	switch {
	case conn.GetConnectionConfig().GetPgConfig() != nil:
		return postgres, conn, nil
	case conn.GetConnectionConfig().GetMysqlConfig() != nil:
		return mysql, conn, nil
	default:
		return 0, nil, fmt.Errorf(
			"the connection %q is neither PostgreSQL nor MySQL: jobs are configured here on those two only",
			conn.GetName(),
		)
	}
}

func buildDestinations(
	ctx context.Context,
	connections *maskedconn.Reader,
	source *mgmtv1alpha1.Connection,
	inputs []destinationInput,
) ([]*mgmtv1alpha1.CreateJobDestination, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no destination given: name at least one connection to write into")
	}
	sourceId := source.GetId()
	seen := map[string]bool{sourceId: true}
	destinations := make([]*mgmtv1alpha1.CreateJobDestination, 0, len(inputs))
	for _, input := range inputs {
		if seen[input.ConnectionId] {
			if input.ConnectionId == sourceId {
				return nil, fmt.Errorf("the source cannot be a destination: a job would write over what it reads")
			}
			return nil, fmt.Errorf("the destination %s is given twice", input.ConnectionId)
		}
		seen[input.ConnectionId] = true
		kind, conn, err := sqlKindOf(ctx, connections, input.ConnectionId)
		if err != nil {
			return nil, err
		}
		if database := databaseOf(conn.GetConnectionConfig()); database != "" && database == databaseOf(source.GetConnectionConfig()) {
			return nil, fmt.Errorf(
				"the destination %q points at the database the source %q reads: a job would write over what it reads",
				conn.GetName(), source.GetName(),
			)
		}
		destinations = append(destinations, &mgmtv1alpha1.CreateJobDestination{
			ConnectionId: input.ConnectionId,
			Options:      destinationOptions(kind, input.TruncateBeforeInsert),
		})
	}
	return destinations, nil
}

func destinationOptions(kind sqlKind, truncate bool) *mgmtv1alpha1.JobDestinationOptions {
	if kind == mysql {
		return &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_MysqlOptions{
			MysqlOptions: &mgmtv1alpha1.MysqlDestinationConnectionOptions{
				TruncateTable: &mgmtv1alpha1.MysqlTruncateTableConfig{TruncateBeforeInsert: truncate},
			},
		}}
	}
	return &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
		PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{
			TruncateTable: &mgmtv1alpha1.PostgresTruncateTableConfig{TruncateBeforeInsert: truncate},
		},
	}}
}

// databaseOf names the database a SQL connection points at, credentials left out, or "" when
// its form does not say. Two connections to one database under different users or names come
// out the same; one database reached by two host names does not, and passes.
func databaseOf(config *mgmtv1alpha1.ConnectionConfig) string {
	pg, my := config.GetPgConfig(), config.GetMysqlConfig()
	switch {
	case pg.GetConnection() != nil:
		c := pg.GetConnection()
		return fmt.Sprintf("postgres %s:%d/%s", strings.ToLower(c.GetHost()), c.GetPort(), c.GetName())
	case pg.GetUrlFromEnv() != "":
		return "postgres env " + pg.GetUrlFromEnv()
	case pg.GetUrl() != "":
		return "postgres " + pg.GetUrl()
	case my.GetConnection() != nil:
		c := my.GetConnection()
		return fmt.Sprintf("mysql %s:%d/%s", strings.ToLower(c.GetHost()), c.GetPort(), c.GetName())
	case my.GetUrlFromEnv() != "":
		return "mysql env " + my.GetUrlFromEnv()
	case my.GetUrl() != "":
		return "mysql " + my.GetUrl()
	default:
		return ""
	}
}

// newColumnStrategy reads what a run does with a column that appears: halt or auto_map. The
// passthrough strategy is not offered; auto_map itself copies a column as it is when the
// detection suggests nothing or the column is in a key, and the person is told so when asked
// to run the job.
func newColumnStrategy(label string) (bool, error) {
	switch label {
	case "", "halt":
		return false, nil
	case "auto_map":
		return true, nil
	default:
		return false, fmt.Errorf("new_columns is halt or auto_map, not %q", label)
	}
}

// sourceOptions reads from a connection, carrying on past a column that disappears — the UI's
// default: halting would stop every run on a mapping no tool here can remove — and halting on
// one that appears unless autoMap.
func sourceOptions(kind sqlKind, connectionId string, autoMap bool) *mgmtv1alpha1.JobSourceOptions {
	if kind == mysql {
		added := &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy{
			Strategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob_{
				HaltJob: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob{},
			},
		}
		if autoMap {
			added.Strategy = &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap_{
				AutoMap: &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap{},
			}
		}
		return &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
			Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
				ConnectionId:              connectionId,
				NewColumnAdditionStrategy: added,
				ColumnRemovalStrategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_ColumnRemovalStrategy{
					Strategy: &mgmtv1alpha1.MysqlSourceConnectionOptions_ColumnRemovalStrategy_ContinueJob_{
						ContinueJob: &mgmtv1alpha1.MysqlSourceConnectionOptions_ColumnRemovalStrategy_ContinueJob{},
					},
				},
			},
		}}
	}
	added := &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{
		Strategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob_{
			HaltJob: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_HaltJob{},
		},
	}
	if autoMap {
		added.Strategy = &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap_{
			AutoMap: &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy_AutoMap{},
		}
	}
	return &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId:              connectionId,
			NewColumnAdditionStrategy: added,
			ColumnRemovalStrategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_ColumnRemovalStrategy{
				Strategy: &mgmtv1alpha1.PostgresSourceConnectionOptions_ColumnRemovalStrategy_ContinueJob_{
					ContinueJob: &mgmtv1alpha1.PostgresSourceConnectionOptions_ColumnRemovalStrategy_ContinueJob{},
				},
			},
		},
	}}
}
