package mcp_server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// told records the progress a client is told of.
type told struct {
	mu       sync.Mutex
	progress []*mcp.ProgressNotificationParams
}

func (p *told) client() *mcp.ClientOptions {
	return &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.progress = append(p.progress, req.Params)
		},
	}
}

func (p *told) all() []*mcp.ProgressNotificationParams {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*mcp.ProgressNotificationParams{}, p.progress...)
}

// heldCall calls a tool whose first read of the job the API holds, and returns once the API
// has the call.
func heldCall(t *testing.T, session *mcp.ClientSession, jobService *fakeJobService, params *mcp.CallToolParams) <-chan error {
	t.Helper()
	// Set once the session is: on a failure the gate opens before the session closes.
	jobService.pauseJob = newGate(t)
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(t.Context(), params)
		done <- err
	}()
	select {
	case <-jobService.pauseJob.entered:
	case err := <-done:
		require.FailNow(t, "the call ended before the API had it", "%v", err)
	}
	return done
}

// A client that asks to be told of the progress of a call is told the call is still worked on,
// at intervals, for as long as it is: a client may wait longer for a call it hears from. It is
// told nothing more once the call has answered.
func Test_Progress_ACallThatLastsTellsItIsStillWorkedOn(t *testing.T) {
	t.Parallel()
	jobService := newFakeJobService()
	client := &told{}
	session := connectJobs(t, jobService, client.client())

	params := &mcp.CallToolParams{Name: "run_job", Arguments: runShop}
	params.SetProgressToken("call-1")
	done := heldCall(t, session, jobService, params)

	require.Eventually(t, func() bool { return len(client.all()) >= 3 }, 5*time.Second, 5*time.Millisecond,
		"the client was not told the call is still worked on")
	jobService.pauseJob.open()
	require.NoError(t, <-done)

	// The client hands what it is told to its handler a moment after it received it: what was
	// told before the answer is waited for, until nothing more comes.
	var progress []*mcp.ProgressNotificationParams
	require.Eventually(t, func() bool {
		before := len(client.all())
		time.Sleep(10 * progressEveryInTests)
		progress = client.all()
		return len(progress) == before
	}, 5*time.Second, time.Millisecond, "the client is still told of a call that has answered")
	for i, notification := range progress {
		require.Equal(t, "call-1", notification.ProgressToken)
		require.NotEmpty(t, notification.Message)
		if i > 0 {
			require.Greater(t, notification.Progress, progress[i-1].Progress, "the progress told does not grow")
		}
	}
}

// A client that did not ask is told nothing.
func Test_Progress_AClientThatDidNotAskIsToldNothing(t *testing.T) {
	t.Parallel()
	jobService := newFakeJobService()
	client := &told{}
	session := connectJobs(t, jobService, client.client())

	done := heldCall(t, session, jobService, &mcp.CallToolParams{Name: "run_job", Arguments: runShop})
	time.Sleep(10 * progressEveryInTests)
	jobService.pauseJob.open()
	require.NoError(t, <-done)

	require.Empty(t, client.all())
}
