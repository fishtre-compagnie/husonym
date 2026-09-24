// Package rowvalues reads real values from a connection's rows, and only with the consent of
// the person behind the agent.
//
// Every value read here goes to the model, and through it to whoever serves the model — which
// is what Husonym exists to prevent. Whether that is acceptable depends on something the
// server cannot see: a model running on the person's machine, cut off from the network, is not
// a model hosted elsewhere. So the server does not decide. It asks the person, through the
// client, before the first read on a connection, and remembers the answer for that connection
// until the session ends.
//
// The question is asked here, inside the reader, rather than left to the tools: there is no
// way to call Preview that skips it. A client that cannot put a question to the person gets a
// refusal, never a read without consent — the model itself is never the one who answers.
package rowvalues

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// valuesClient is the part of the connection data service this package may call.
// reader_test.go pins the list, so that widening it is a decision someone takes.
type valuesClient interface {
	PreviewColumnTransformer(
		context.Context,
		*connect.Request[mgmtv1alpha1.PreviewColumnTransformerRequest],
	) (*connect.Response[mgmtv1alpha1.PreviewColumnTransformerResponse], error)
}

// catalogClient is the part of the transformer service this package may call: the default
// configuration of a system transformer, which carries no value.
type catalogClient interface {
	GetSystemTransformerBySource(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetSystemTransformerBySourceRequest],
	) (*connect.Response[mgmtv1alpha1.GetSystemTransformerBySourceResponse], error)
}

// ErrDeclined is returned when the person declined, or dismissed the question.
var ErrDeclined = errors.New("the person declined to send this connection's values to the model")

// ErrCannotAsk is returned when the client cannot put a question to the person.
var ErrCannotAsk = errors.New(
	"this client cannot ask the person for consent, and values are never read without it: " +
		"preview_column needs a client that supports elicitation",
)

// consentKey is what a consent covers: one connection, for one session.
type consentKey struct {
	session    *mcp.ServerSession
	connection string
}

// Reader previews a transformer on real values, once the person has agreed.
type Reader struct {
	values  valuesClient
	catalog catalogClient

	mu sync.Mutex
	// granted holds the consents given. It lives as long as the server: over stdio that is one
	// session, so nothing outlives the session it was given in.
	granted map[consentKey]bool
	// asked holds the questions put and not yet answered, by the id sent with each. An answer
	// counts only against a question this reader asked, and only once.
	asked map[string]consentKey
}

// New builds a Reader on its own clients, which are never handed out.
func New(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *Reader {
	return &Reader{
		values:  mgmtv1alpha1connect.NewConnectionDataServiceClient(httpClient, baseURL, opts...),
		catalog: mgmtv1alpha1connect.NewTransformersServiceClient(httpClient, baseURL, opts...),
		granted: map[consentKey]bool{},
		asked:   map[string]consentKey{},
	}
}

// Column names the column to preview, and the connection it is read from.
type Column struct {
	ConnectionId   string
	ConnectionName string
	Schema         string
	Table          string
	Column         string
}

// Preview shows what a system transformer makes of real values of a column.
//
// Before the person has agreed for this connection, it reads nothing and returns the question
// to put to them instead, as input requests for the tool's result; the client asks, then calls
// the tool again with the answer.
func (r *Reader) Preview(
	ctx context.Context,
	req *mcp.CallToolRequest,
	column *Column,
	source mgmtv1alpha1.TransformerSource,
	limit uint32,
) (*mgmtv1alpha1.PreviewColumnTransformerResponse, mcp.InputRequestMap, error) {
	questions, err := r.consent(req, column)
	if err != nil || questions != nil {
		return nil, questions, err
	}

	transformer, err := r.catalog.GetSystemTransformerBySource(ctx, connect.NewRequest(
		&mgmtv1alpha1.GetSystemTransformerBySourceRequest{Source: source},
	))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to find the transformer: %w", err)
	}
	res, err := r.values.PreviewColumnTransformer(ctx, connect.NewRequest(&mgmtv1alpha1.PreviewColumnTransformerRequest{
		ConnectionId: column.ConnectionId,
		Schema:       column.Schema,
		Table:        column.Table,
		Column:       column.Column,
		Transformer:  transformer.Msg.GetTransformer().GetConfig(),
		Limit:        limit,
	}))
	if err != nil {
		return nil, nil, err
	}
	return res.Msg, nil, nil
}

// consent says whether values of this connection may be read in this session. When the person
// has not been asked yet, it returns the question to ask; when they answered, it records the
// answer.
func (r *Reader) consent(req *mcp.CallToolRequest, column *Column) (mcp.InputRequestMap, error) {
	key := consentKey{session: req.Session, connection: column.ConnectionId}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.granted[key] {
		return nil, nil
	}

	for id, response := range req.Params.InputResponses {
		asked, ok := r.asked[id]
		if !ok || asked != key {
			continue
		}
		delete(r.asked, id)
		if answer, ok := response.(*mcp.ElicitResult); ok && answer.Action == "accept" {
			r.granted[key] = true
			return nil, nil
		}
		return nil, ErrDeclined
	}

	if capabilities := req.ClientCapabilities(); capabilities == nil || capabilities.Elicitation == nil {
		return nil, ErrCannotAsk
	}
	id := rand.Text()
	r.asked[id] = key
	return mcp.InputRequestMap{id: &mcp.ElicitParams{
		Mode: "form",
		Message: fmt.Sprintf(
			"The agent asks to preview a transformer on %s.%s.%s, from the connection %q. "+
				"Real values of that column will be sent to the model, and to whoever serves it "+
				"if it is not running on this machine. Allow values of this connection to be read "+
				"for the rest of this session?",
			column.Schema, column.Table, column.Column, column.ConnectionName,
		),
		RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}}, nil
}
