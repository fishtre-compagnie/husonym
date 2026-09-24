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

// byHand is a client that declares it can ask, but answers by hand: the tests send the answers
// a confused or hostile client could send.
func byHand() *mcp.ClientOptions {
	options := (&person{answer: "accept"}).client()
	options.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	return options
}

// accept answers yes to the question id.
func accept(id string) mcp.InputResponseMap {
	return mcp.InputResponseMap{id: &mcp.ElicitResult{Action: "accept"}}
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
		require.Contains(t, questions[0], `the column "email" of the table "public.users"`)
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

// questionOf calls a tool as a client that fulfils nothing itself, and returns the id of the
// question the server puts — the id a forged answer would have to name.
func questionOf(
	t *testing.T,
	session *mcp.ClientSession,
	tool string,
	args map[string]any,
	responses mcp.InputResponseMap,
) string {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: tool, Arguments: args, InputResponses: responses,
	})
	require.NoError(t, err)
	require.True(t, res.NeedsInput(), "expected a question, got %v", res.Content)
	require.Len(t, res.InputRequests, 1)
	for id := range res.InputRequests {
		return id
	}
	return ""
}

// A client that declares it can ask, but answers by hand: the answers below are what a
// confused or hostile client could send, and none of them may open a read.
func Test_PreviewColumn_AnswersAreBoundToTheirQuestion(t *testing.T) {
	t.Parallel()
	answeringByHand := func(t *testing.T) (*mcp.ClientSession, *fakeDataService) {
		data := &fakeDataService{}
		return connectClientWith(t, &fakeConnectionService{}, data, byHand(), ""), data
	}
	other := maps.Clone(previewEmail)
	other["connection_id"] = "7f2c1e4a-0000-4000-8000-0000000000c2"

	t.Run("an answer about one connection opens no other", func(t *testing.T) {
		t.Parallel()
		session, data := answeringByHand(t)
		asked := questionOf(t, session, "preview_column", previewEmail, nil)

		questionOf(t, session, "preview_column", other, accept(asked))
		require.Empty(t, data.previewRequests())
	})

	t.Run("an answer counts once", func(t *testing.T) {
		t.Parallel()
		session, data := answeringByHand(t)
		asked := questionOf(t, session, "preview_column", previewEmail, nil)

		_, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "preview_column", Arguments: previewEmail,
			InputResponses: mcp.InputResponseMap{asked: &mcp.ElicitResult{Action: "decline"}},
		})
		require.NoError(t, err)
		// The same id, now accepting: it was spent on the decline.
		questionOf(t, session, "preview_column", previewEmail, accept(asked))
		require.Empty(t, data.previewRequests())
	})

	t.Run("a question asked again replaces the one pending", func(t *testing.T) {
		t.Parallel()
		session, data := answeringByHand(t)
		first := questionOf(t, session, "preview_column", previewEmail, nil)
		second := questionOf(t, session, "preview_column", previewEmail, nil)
		require.NotEqual(t, first, second)

		questionOf(t, session, "preview_column", previewEmail, accept(first))
		require.Empty(t, data.previewRequests(), "the first question no longer stands")
	})

	t.Run("the answer to the question asked opens the read", func(t *testing.T) {
		t.Parallel()
		session, data := answeringByHand(t)
		asked := questionOf(t, session, "preview_column", previewEmail, nil)

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "preview_column", Arguments: previewEmail, InputResponses: accept(asked),
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.Len(t, data.previewRequests(), 1)
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
