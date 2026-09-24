package jobs

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/connection"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/ask"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/proto"
)

// confirmation is what one yes covers: one change of one job, as it stood when the question
// was put, in one session. A job changed since, however little, or a different change, needs
// a new question.
type confirmation struct {
	session *mcp.ServerSession
	job     string
	// stood is the job and its hooks, encoded: the API does not move a job's updated_at on
	// every change, so the whole of it is compared.
	stood  string
	change string
}

// snapshot is a job as a run would read it, hooks included.
type snapshot struct {
	job   *mgmtv1alpha1.Job
	hooks []*mgmtv1alpha1.JobHook
}

// snapshotOf reads the hooks of a job, and puts them with it.
func (r *Reader) snapshotOf(ctx context.Context, job *mgmtv1alpha1.Job) (*snapshot, error) {
	res, err := r.client.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: job.GetId()}))
	if err != nil {
		return nil, fmt.Errorf("unable to read the hooks of job %s: %w", job.GetId(), err)
	}
	hooks := slices.Clone(res.Msg.GetHooks())
	slices.SortFunc(hooks, func(a, b *mgmtv1alpha1.JobHook) int { return cmp.Compare(a.GetId(), b.GetId()) })
	return &snapshot{job: job, hooks: hooks}, nil
}

// encoded gives the snapshot as bytes that are the same for the same job and hooks.
func (s *snapshot) encoded() (string, error) {
	deterministic := proto.MarshalOptions{Deterministic: true}
	job, err := deterministic.Marshal(s.job)
	if err != nil {
		return "", err
	}
	var encoded strings.Builder
	encoded.Write(job)
	for _, hook := range s.hooks {
		raw, err := deterministic.Marshal(hook)
		if err != nil {
			return "", err
		}
		encoded.WriteString("\x00")
		encoded.Write(raw)
	}
	return encoded.String(), nil
}

// confirm says whether the person agreed to change on job, as it stands with its hooks. When
// they have not been asked, it returns the question to ask, worded by message; their answer
// is spent once read.
func (r *Reader) confirm(
	ctx context.Context,
	req *mcp.CallToolRequest,
	job *mgmtv1alpha1.Job,
	change string,
	message func(*snapshot) (string, error),
) (mcp.InputRequestMap, error) {
	snap, err := r.snapshotOf(ctx, job)
	if err != nil {
		return nil, err
	}
	stood, err := snap.encoded()
	if err != nil {
		return nil, err
	}
	key := confirmation{session: req.Session, job: job.GetId(), stood: stood, change: change}

	r.mu.Lock()
	accepted, answered := r.questions.Answer(req, key)
	r.mu.Unlock()
	switch {
	case accepted:
		return nil, nil
	case answered:
		return nil, ErrDeclined
	case !ask.CanAsk(req):
		return nil, ErrCannotAsk
	}

	text, err := message(snap)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.questions.Ask(key, text), nil
}

// describe says, for the person, what a run of the job with mappings reads, runs and writes:
// all of what they agree to, not the part the agent changed.
func (r *Reader) describe(ctx context.Context, snap *snapshot, mappings []*mgmtv1alpha1.JobMapping) (string, error) {
	job := snap.job
	source, err := r.connectionName(ctx, SourceConnectionId(job))
	if err != nil {
		return "", err
	}

	tables := map[string]bool{}
	var passthrough, code, userDefined int
	for _, mapping := range mappings {
		tables[mapping.GetSchema()+"."+mapping.GetTable()] = true
		config := mapping.GetTransformer().GetConfig()
		switch {
		case config.GetPassthroughConfig() != nil:
			passthrough++
		case config.GetUserDefinedTransformerConfig() != nil:
			userDefined++
		case novalues.RunsCode(config):
			code++
		}
	}
	var sentences []string
	reads := fmt.Sprintf("reads %s of %s from %s, %d of them copied as they are (passthrough)",
		count(len(mappings), "column"), count(len(tables), "table"), source, passthrough)
	if code > 0 {
		reads += fmt.Sprintf(", %d through JavaScript written in the job", code)
	}
	if userDefined > 0 {
		reads += fmt.Sprintf(", %d through transformers defined in the account", userDefined)
	}
	sentences = append(sentences, reads+".")
	if autoMaps(job) {
		sentences = append(sentences, "A column that appears in these tables is mapped as the PII detection "+
			"suggests, and copied as it is when the detection suggests nothing or the column is in a key.")
	}

	destinations := make([]string, 0, len(job.GetDestinations()))
	for _, destination := range job.GetDestinations() {
		name, err := r.connectionName(ctx, destination.GetConnectionId())
		if err != nil {
			return "", err
		}
		options := destination.GetOptions()
		var does []string
		if initsSchema(options) {
			does = append(does, "the tables it lacks are created")
		}
		switch {
		case Truncates(options) && cascades(options):
			does = append(does, "its tables are emptied first, and the tables referencing them too")
		case Truncates(options):
			does = append(does, "its tables are emptied first")
		}
		if len(does) > 0 {
			name += " (" + strings.Join(does, "; ") + ")"
		}
		destinations = append(destinations, name)
	}
	sentences = append(sentences, "It writes into "+strings.Join(destinations, ", ")+".")

	var hooks []string
	for _, hook := range snap.hooks {
		if !hook.GetEnabled() {
			continue
		}
		sql := hook.GetConfig().GetSql()
		when := "after the sync"
		if sql.GetTiming().GetPreSync() != nil {
			when = "before the sync"
		}
		on, err := r.connectionName(ctx, sql.GetConnectionId())
		if err != nil {
			return "", err
		}
		hooks = append(hooks, fmt.Sprintf("%q %s on %s", hook.GetName(), when, on))
	}
	if len(hooks) > 0 {
		sentences = append(sentences, fmt.Sprintf("It also runs the SQL of %s: %s.",
			count(len(hooks), "hook"), strings.Join(hooks, ", ")))
	}
	sentences = append(sentences, "This writes into those databases, and cannot be undone from here.")
	return strings.Join(sentences, " "), nil
}

// count says how many of a noun, singular or plural.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// connectionName names a connection the way the person knows it, with its kind.
func (r *Reader) connectionName(ctx context.Context, connectionId string) (string, error) {
	conn, err := r.connections.Get(ctx, connectionId)
	if err != nil {
		return "", fmt.Errorf("unable to read connection %s: %w", connectionId, err)
	}
	return fmt.Sprintf("%q (%s)", conn.GetName(), connection.Category(conn.GetConnectionConfig())), nil
}
