package mcp_server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/connection"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
)

type describeConnectionInput struct {
	ConnectionId string `json:"connection_id" jsonschema:"the id of the connection, as list_connections gives it"`
}

type describeConnectionOutput struct {
	Id        string         `json:"id"`
	Name      string         `json:"name"`
	Category  string         `json:"category"`
	Config    map[string]any `json:"config"     jsonschema:"where the connection points and how it connects; every secret in it is masked"`
	CreatedAt string         `json:"created_at" jsonschema:"RFC 3339, in UTC"`
	UpdatedAt string         `json:"updated_at" jsonschema:"RFC 3339, in UTC"`
}

func addDescribeConnection(server *mcp.Server, reader *maskedconn.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "describe_connection",
		Description: "Describe one connection: host, port, database, user, tunnel, TLS and options. " +
			"Passwords, keys and tokens are masked; none is ever returned.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, describeConnection(reader))
}

func describeConnection(
	reader *maskedconn.Reader,
) mcp.ToolHandlerFor[describeConnectionInput, describeConnectionOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input describeConnectionInput,
	) (*mcp.CallToolResult, describeConnectionOutput, error) {
		conn, err := reader.Get(ctx, input.ConnectionId)
		if err != nil {
			return nil, describeConnectionOutput{}, fmt.Errorf(
				"unable to read connection %s: %w", input.ConnectionId, err,
			)
		}
		// The config is handed over as the API shaped it, masked: a projection of our own
		// would be one more place to forget a field.
		raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(conn.GetConnectionConfig())
		if err != nil {
			return nil, describeConnectionOutput{}, fmt.Errorf("unable to encode connection %s: %w", conn.GetId(), err)
		}
		config := map[string]any{}
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, describeConnectionOutput{}, fmt.Errorf("unable to encode connection %s: %w", conn.GetId(), err)
		}
		return nil, describeConnectionOutput{
			Id:        conn.GetId(),
			Name:      conn.GetName(),
			Category:  connection.Category(conn.GetConnectionConfig()),
			Config:    config,
			CreatedAt: conn.GetCreatedAt().AsTime().UTC().Format(time.RFC3339),
			UpdatedAt: conn.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339),
		}, nil
	}
}
