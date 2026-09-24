package mcp_server

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/connection"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listConnectionsInput struct{}

type listConnectionsOutput struct {
	Connections []connectionSummary `json:"connections"`
}

type connectionSummary struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Category  string `json:"category"   jsonschema:"the kind of system the connection points at, such as PostgreSQL or MySQL"`
	CreatedAt string `json:"created_at" jsonschema:"RFC 3339, in UTC"`
	UpdatedAt string `json:"updated_at" jsonschema:"RFC 3339, in UTC"`
}

func addListConnections(server *mcp.Server, reader *maskedconn.Reader, accountId string) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_connections",
		Description: "List the connections of the account: the databases and stores Husonym reads from " +
			"and writes to. Gives each one's id, name and category, never its credentials.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, listConnections(reader, accountId))
}

func listConnections(
	reader *maskedconn.Reader,
	accountId string,
) mcp.ToolHandlerFor[listConnectionsInput, listConnectionsOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		_ listConnectionsInput,
	) (*mcp.CallToolResult, listConnectionsOutput, error) {
		connections, err := reader.List(ctx, accountId)
		if err != nil {
			return nil, listConnectionsOutput{}, fmt.Errorf(
				"unable to list the connections of account %s: %w", accountId, err,
			)
		}
		return nil, listConnectionsOutput{Connections: summarize(connections)}, nil
	}
}

// summarize keeps what tells connections apart, in a stable order, so that the same account
// always reads the same.
func summarize(connections []*mgmtv1alpha1.Connection) []connectionSummary {
	summaries := make([]connectionSummary, 0, len(connections))
	for _, conn := range connections {
		summaries = append(summaries, connectionSummary{
			Id:        conn.GetId(),
			Name:      conn.GetName(),
			Category:  connection.Category(conn.GetConnectionConfig()),
			CreatedAt: conn.GetCreatedAt().AsTime().UTC().Format(time.RFC3339),
			UpdatedAt: conn.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339),
		})
	}
	slices.SortFunc(summaries, func(a, b connectionSummary) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Id, b.Id))
	})
	return summaries
}
