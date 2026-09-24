package mcp_server

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// person answers the questions the server puts, and remembers them.
type person struct {
	answer string

	mu        sync.Mutex
	questions []string
}

func (p *person) client() *mcp.ClientOptions {
	return &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			p.mu.Lock()
			p.questions = append(p.questions, req.Params.Message)
			p.mu.Unlock()
			return &mcp.ElicitResult{Action: p.answer}, nil
		},
	}
}

func (p *person) asked() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.questions)
}

var previewEmail = map[string]any{
	"connection_id": connectionId,
	"table":         "public.users",
	"column":        "email",
	"transformer":   "generate_email",
}

func Test_PreviewColumn(t *testing.T) {
	t.Parallel()

	// Since 2026-07-28 the question travels in the tool's result and the client calls again;
	// before, the server puts it to the client itself. The SDK carries both, the rule is one.
	for _, protocolVersion := range []string{"", "2025-11-25"} {
		t.Run("protocol "+cmp.Or(protocolVersion, "latest"), func(t *testing.T) {
			t.Parallel()
			testConsent(t, protocolVersion)
		})
	}

	t.Run("takes no answer to a question it did not ask", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{}
		session := connectClient(t, &fakeConnectionService{}, data)

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:           "preview_column",
			Arguments:      previewEmail,
			InputResponses: mcp.InputResponseMap{"made-up": &mcp.ElicitResult{Action: "accept"}},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		require.Empty(t, data.previewRequests())
	})

	t.Run("refuses a transformer that is not a system one", func(t *testing.T) {
		t.Parallel()
		somebody := &person{answer: "accept"}
		session := connectClientWith(t, &fakeConnectionService{}, &fakeDataService{}, somebody.client(), "")

		message := callToolError(t, session, "preview_column", map[string]any{
			"connection_id": connectionId, "table": "public.users", "column": "email", "transformer": "shred",
		})
		require.Contains(t, message, `"shred"`)
		require.Empty(t, somebody.asked(), "nothing to ask about before the call makes sense")
	})

	t.Run("refuses a column that is not there, before asking anything", func(t *testing.T) {
		t.Parallel()
		somebody := &person{answer: "accept"}
		session := connectClientWith(t, &fakeConnectionService{}, &fakeDataService{}, somebody.client(), "")

		message := callToolError(t, session, "preview_column", map[string]any{
			"connection_id": connectionId, "table": "public.users", "column": "nope", "transformer": "generate_email",
		})
		require.Contains(t, message, "no column nope in public.users")
		require.Empty(t, somebody.asked())
	})
}

func testConsent(t *testing.T, protocolVersion string) {
	t.Run("asks the person once for the connection, then shows the values", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{}
		somebody := &person{answer: "accept"}
		session := connectClientWith(t, &fakeConnectionService{}, data, somebody.client(), protocolVersion)

		res := callTool(t, session, "preview_column", previewEmail)
		callTool(t, session, "preview_column", previewEmail)

		questions := somebody.asked()
		require.Len(t, questions, 1, "the consent covers the connection for the session")
		require.Contains(t, questions[0], "public.users.email")
		require.Contains(t, questions[0], `"production"`)
		require.Contains(t, questions[0], "sent to the model")

		previews := data.previewRequests()
		require.Len(t, previews, 2)
		require.Equal(t, "public", previews[0].GetSchema())
		require.Equal(t, "users", previews[0].GetTable())
		require.Equal(t, "email", previews[0].GetColumn())
		require.NotNil(t, previews[0].GetTransformer().GetGenerateEmailConfig(), "the default config of generate_email")

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"values": [
				{"input": "jean.dupont@example.com", "output": "kx81@anon.test"},
				{"input": null, "output": null}
			],
			"distinct_inputs": 1,
			"distinct_outputs": 1
		}`, string(structured))
	})

	t.Run("reads nothing when the person declines", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{}
		somebody := &person{answer: "decline"}
		session := connectClientWith(t, &fakeConnectionService{}, data, somebody.client(), protocolVersion)

		message := callToolError(t, session, "preview_column", previewEmail)
		require.Contains(t, message, "declined")
		require.Empty(t, data.previewRequests())

		// A refusal is not remembered as consent: the next call asks again.
		callToolError(t, session, "preview_column", previewEmail)
		require.Len(t, somebody.asked(), 2)
		require.Empty(t, data.previewRequests())
	})

	t.Run("refuses a client that cannot ask the person", func(t *testing.T) {
		t.Parallel()
		data := &fakeDataService{}
		session := connectClientWith(t, &fakeConnectionService{}, data, nil, protocolVersion)

		message := callToolError(t, session, "preview_column", previewEmail)
		require.Contains(t, message, "cannot ask the person")
		require.Empty(t, data.previewRequests())
	})
}

// The values of one connection are not consented to by agreeing for another.
func Test_PreviewColumn_ConsentIsPerConnection(t *testing.T) {
	t.Parallel()
	somebody := &person{answer: "accept"}
	session := connectClientWith(t, &fakeConnectionService{}, &fakeDataService{}, somebody.client(), "")

	callTool(t, session, "preview_column", previewEmail)
	other := maps.Clone(previewEmail)
	other["connection_id"] = "7f2c1e4a-0000-4000-8000-0000000000c2"
	callTool(t, session, "preview_column", other)

	require.Len(t, somebody.asked(), 2)
}
