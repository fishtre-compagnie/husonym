package mcp_server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type checkConnectionInput struct {
	ConnectionId         string            `json:"connection_id"                    jsonschema:"the id of the connection, as list_connections gives it"`
	Role                 string            `json:"role"                             jsonschema:"source: a job reads it; destination: a job writes into it"`
	Tables               []checkTableInput `json:"tables,omitempty"                 jsonschema:"the tables of the job, at most 1000; leave out to check the server as a whole"`
	Engine               string            `json:"engine,omitempty"                 jsonschema:"athanor or benthos; leave out for the deployment default, which only a worker knows: what only Athanor needs is then a warning"`
	InitTableSchema      bool              `json:"init_table_schema,omitempty"      jsonschema:"destination only: the run creates the tables and columns it lacks"`
	TruncateBeforeInsert bool              `json:"truncate_before_insert,omitempty" jsonschema:"destination only: the run empties each table before writing it"`
}

type checkTableInput struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Columns []string `json:"columns,omitempty" jsonschema:"the columns the job writes; leave out for every column the account sees, generated ones left out"`
}

type checkConnectionOutput struct {
	ConnectionId string    `json:"connection_id"`
	Connected    bool      `json:"connected"     jsonschema:"false when the API could not log in: nothing else was checked"`
	Findings     []finding `json:"findings"      jsonschema:"what the connection cannot do that the role needs, blocking first"`
}

func addCheckConnection(server *mcp.Server, reader *maskedconn.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "check_connection",
		Description: "Check that a PostgreSQL or MySQL connection can do what a job will ask of it, before the job " +
			"exists: the API logs in, and says what the account lacks to read the tables as a source, or to " +
			"write, empty and create them as a destination. No row is read and nothing is written. Once the " +
			"job exists, preflight_job checks it whole. Other kinds of connection are refused. " + namesAreData,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, checkConnection(reader))
}

func checkConnection(reader *maskedconn.Reader) mcp.ToolHandlerFor[checkConnectionInput, checkConnectionOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input checkConnectionInput,
	) (*mcp.CallToolResult, checkConnectionOutput, error) {
		scope, err := checkScope(&input)
		if err != nil {
			return nil, checkConnectionOutput{}, err
		}
		check, err := reader.Check(ctx, input.ConnectionId, scope)
		if err != nil {
			return nil, checkConnectionOutput{}, err
		}
		out := checkConnectionOutput{
			ConnectionId: input.ConnectionId,
			Connected:    check.Connected,
			Findings:     make([]finding, 0, len(check.Findings)),
		}
		for _, f := range check.Findings {
			out.Findings = append(out.Findings, connectionFinding(f))
		}
		out.Findings = byLevel(out.Findings)
		return nil, out, nil
	}
}

func checkScope(input *checkConnectionInput) (*mgmtv1alpha1.ConnectionCheckScope, error) {
	role, ok := mgmtv1alpha1.ConnectionRole_value["CONNECTION_ROLE_"+strings.ToUpper(input.Role)]
	if !ok || role == int32(mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_UNSPECIFIED) {
		return nil, fmt.Errorf("role %q: give source or destination", input.Role)
	}
	engine := mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED
	if input.Engine != "" {
		value, ok := mgmtv1alpha1.JobEngine_value["JOB_ENGINE_"+strings.ToUpper(input.Engine)]
		if !ok || value == int32(mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED) {
			return nil, fmt.Errorf("engine %q: give athanor or benthos, or leave it out", input.Engine)
		}
		engine = mgmtv1alpha1.JobEngine(value)
	}
	if mgmtv1alpha1.ConnectionRole(role) == mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_SOURCE &&
		(input.InitTableSchema || input.TruncateBeforeInsert) {
		return nil, errors.New("init_table_schema and truncate_before_insert are what a run does to a destination")
	}
	scope := &mgmtv1alpha1.ConnectionCheckScope{
		Role:                 mgmtv1alpha1.ConnectionRole(role),
		Engine:               engine,
		InitTableSchema:      input.InitTableSchema,
		TruncateBeforeInsert: input.TruncateBeforeInsert,
	}
	for _, table := range input.Tables {
		scope.Tables = append(scope.Tables, &mgmtv1alpha1.ConnectionCheckTable{
			Schema:  table.Schema,
			Table:   table.Table,
			Columns: table.Columns,
		})
	}
	return scope, nil
}
