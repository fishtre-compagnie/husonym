// Package ask puts questions to the person behind the agent, through the client, and takes
// their answers back.
//
// The model is never the one who answers: a question travels to the client, which shows it to
// the person, and the answer comes back with the next call of the tool. What this package holds
// is the rule that makes an answer count — against a question it asked, about the very thing it
// asked about, and only once — so that the readers which ask do not each write it again.
package ask

import (
	"crypto/rand"
	"maps"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Questions holds the questions put and not yet answered, each under the key of what it is
// about. It is not safe for concurrent use: the reader holding it locks around it, together
// with whatever it records of the answers.
type Questions[K comparable] struct {
	// pending holds the questions by the id sent with each. One key has one question pending at
	// most: asking again replaces it, so a client that never answers leaves one entry per key,
	// not one per call.
	pending map[string]K
}

// New returns an empty set of questions.
func New[K comparable]() *Questions[K] {
	return &Questions[K]{pending: map[string]K{}}
}

// Answer looks through the answers a call carries for one to a question asked about key. It
// reports whether the person accepted, and whether they answered at all; an answer is spent
// once read.
func (q *Questions[K]) Answer(req *mcp.CallToolRequest, key K) (accepted, answered bool) {
	for id, response := range req.Params.InputResponses {
		asked, ok := q.pending[id]
		if !ok || asked != key {
			continue
		}
		delete(q.pending, id)
		answer, ok := response.(*mcp.ElicitResult)
		return ok && answer.Action == "accept", true
	}
	return false, false
}

// CanAsk says whether the client can put a question to the person.
func CanAsk(req *mcp.CallToolRequest) bool {
	capabilities := req.ClientCapabilities()
	return capabilities != nil && capabilities.Elicitation != nil
}

// Ask records a question about key and returns it, to be handed back as the input requests of
// the tool's result: the client asks the person, then calls the tool again with the answer.
// The question asks for a yes or a no, nothing typed.
func (q *Questions[K]) Ask(key K, message string) mcp.InputRequestMap {
	maps.DeleteFunc(q.pending, func(_ string, pending K) bool { return pending == key })
	id := rand.Text()
	q.pending[id] = key
	return mcp.InputRequestMap{id: &mcp.ElicitParams{
		Mode:            "form",
		Message:         message,
		RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}
