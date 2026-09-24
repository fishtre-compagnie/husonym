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
// Values come two ways: a transformer previewed on a column, and the message of a run's
// failure, which can quote the value the run failed on. Both are read here and nowhere else.
//
// The question is asked here, inside the reader, rather than left to the tools: there is no
// way to read that skips it, and the reader finds for itself which connection a read touches.
// A client that cannot put a question to the person gets a refusal, never a read without
// consent — the model itself is never the one who answers.
package rowvalues

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/ask"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
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

// runClient is the part of the job service this package may call: what a run failed on.
type runClient interface {
	GetJobRun(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRunRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobRunResponse], error)
	GetJobRunEvents(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRunEventsRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobRunEventsResponse], error)
}

// ErrDeclined is returned when the person declined, or dismissed the question.
var ErrDeclined = errors.New("the person declined to send this connection's values to the model")

// ErrCannotAsk is returned when the client cannot put a question to the person.
var ErrCannotAsk = errors.New(
	"this client cannot ask the person for consent, and values are never read without it: " +
		"preview_column and get_run_failure need a client that supports elicitation",
)

// consentKey is what a consent covers: one connection, for one session.
type consentKey struct {
	session    *mcp.ServerSession
	connection string
}

// Reader reads values from rows, once the person has agreed for the connection.
type Reader struct {
	values      valuesClient
	runs        runClient
	connections *maskedconn.Reader
	jobs        *jobs.Reader
	accountId   string

	mu sync.Mutex
	// granted holds the consents given. It lives as long as the server: over stdio that is one
	// session, so nothing outlives the session it was given in.
	granted   map[consentKey]bool
	questions *ask.Questions[consentKey]
}

// New builds a Reader on its own clients, which are never handed out. It names connections to
// the person through connections, and finds the job of a run through jobs.
func New(
	httpClient connect.HTTPClient,
	baseURL string,
	accountId string,
	connections *maskedconn.Reader,
	jobReader *jobs.Reader,
	opts ...connect.ClientOption,
) *Reader {
	return &Reader{
		values:      mgmtv1alpha1connect.NewConnectionDataServiceClient(httpClient, baseURL, opts...),
		runs:        mgmtv1alpha1connect.NewJobServiceClient(httpClient, baseURL, opts...),
		connections: connections,
		jobs:        jobReader,
		accountId:   accountId,
		granted:     map[consentKey]bool{},
		questions:   ask.New[consentKey](),
	}
}

// Column names the column to preview, and the connection it is read from.
type Column struct {
	ConnectionId string
	Schema       string
	Table        string
	Column       string
}

// Preview shows what a transformer, configured as given, makes of real values of a column.
//
// Before the person has agreed for this connection, it reads nothing and returns the question
// to put to them instead, as input requests for the tool's result; the client asks, then calls
// the tool again with the answer.
func (r *Reader) Preview(
	ctx context.Context,
	req *mcp.CallToolRequest,
	column *Column,
	transformer *mgmtv1alpha1.TransformerConfig,
	limit uint32,
) (*mgmtv1alpha1.PreviewColumnTransformerResponse, mcp.InputRequestMap, error) {
	questions, err := r.consent(ctx, req, column.ConnectionId, func(name string) string {
		return fmt.Sprintf(
			"The agent asks to preview a transformer on the column %q of the table %q, from the connection %q. "+
				"Real values of that column will be sent to the model, and to whoever serves it "+
				"if it is not running on this machine. Allow values of this connection to be read "+
				"for the rest of this session?",
			column.Column, column.Schema+"."+column.Table, name,
		)
	})
	if err != nil || questions != nil {
		return nil, questions, err
	}

	res, err := r.values.PreviewColumnTransformer(ctx, connect.NewRequest(&mgmtv1alpha1.PreviewColumnTransformerRequest{
		ConnectionId: column.ConnectionId,
		Schema:       column.Schema,
		Table:        column.Table,
		Column:       column.Column,
		Transformer:  transformer,
		Limit:        limit,
	}))
	if err != nil {
		return nil, nil, err
	}
	return res.Msg, nil, nil
}

// Failure is what a run failed on, messages included.
type Failure struct {
	Run    *mgmtv1alpha1.JobRun
	Events []*mgmtv1alpha1.JobRunEvent
}

// Failure reads the messages of a run's failures. They can quote values of the rows the run
// read from its job's source, so it asks as Preview does, about that connection.
func (r *Reader) Failure(
	ctx context.Context,
	req *mcp.CallToolRequest,
	runId string,
) (*Failure, mcp.InputRequestMap, error) {
	// The run is read without its messages first, to find the connection to ask about.
	run, err := r.jobs.GetRun(ctx, runId)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read run %s: %w", runId, err)
	}
	scrubbed, err := r.jobs.Events(ctx, runId)
	if err != nil {
		return nil, nil, err
	}
	// A run with nothing failed has no message to read, and no reason to ask.
	if !jobs.Failed(run, scrubbed) {
		return &Failure{Run: run, Events: scrubbed}, nil, nil
	}
	job, err := r.jobs.Get(ctx, run.GetJobId())
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read job %s: %w", run.GetJobId(), err)
	}
	source := jobs.SourceConnectionId(job)
	if source == "" {
		return nil, nil, fmt.Errorf("the job %q reads from neither PostgreSQL nor MySQL: its failures are not read here", job.GetName())
	}

	questions, err := r.consent(ctx, req, source, func(name string) string {
		return fmt.Sprintf(
			"The agent asks to read why run %s of the job %q failed. A failure message can quote "+
				"values of the rows it failed on, read from the connection %q or met in the "+
				"destinations; they will be sent to the model, and to whoever serves it if it is "+
				"not running on this machine. Allow values of this connection to be read for the "+
				"rest of this session?",
			runId, job.GetName(), name,
		)
	})
	if err != nil || questions != nil {
		return nil, questions, err
	}

	withMessages, err := r.runs.GetJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunRequest{
		JobRunId: runId, AccountId: r.accountId,
	}))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read run %s: %w", runId, err)
	}
	events, err := r.runs.GetJobRunEvents(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunEventsRequest{
		JobRunId: runId, AccountId: r.accountId,
	}))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read the events of run %s: %w", runId, err)
	}
	return &Failure{Run: withMessages.Msg.GetJobRun(), Events: events.Msg.GetEvents()}, nil, nil
}

// consent says whether values of a connection may be read in this session. When the person has
// not been asked yet, it returns the question to ask, worded by message around the name of the
// connection; when they answered, it records the answer.
func (r *Reader) consent(
	ctx context.Context,
	req *mcp.CallToolRequest,
	connectionId string,
	message func(name string) string,
) (mcp.InputRequestMap, error) {
	key := consentKey{session: req.Session, connection: connectionId}

	r.mu.Lock()
	granted := r.granted[key]
	accepted, answered := false, false
	if !granted {
		accepted, answered = r.questions.Answer(req, key)
		if accepted {
			r.granted[key] = true
		}
	}
	r.mu.Unlock()
	switch {
	case granted, accepted:
		return nil, nil
	case answered:
		return nil, ErrDeclined
	case !ask.CanAsk(req):
		return nil, ErrCannotAsk
	}

	// The person is asked about a connection they know by its name, not by its id.
	conn, err := r.connections.Get(ctx, connectionId)
	if err != nil {
		return nil, fmt.Errorf("unable to read connection %s: %w", connectionId, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.questions.Ask(key, message(conn.GetName())), nil
}
