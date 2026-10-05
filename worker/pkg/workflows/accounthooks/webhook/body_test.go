package webhook

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"github.com/stretchr/testify/require"
)

var (
	goldenRun = runevents.Run{AccountID: "acc-replay", JobID: "job-replay", RunID: "datasync-before"}
	goldenAt  = time.Date(2026, time.October, 3, 7, 51, 53, 58540394, time.UTC)
)

const (
	goldenSecret        = "test-secret"
	goldenSucceededBody = `{"event_name":"ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED","event_data":{"name":3,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunSucceeded":{"jobId":"job-replay","jobRunId":"datasync-before"}}}`
	goldenCreatedBody   = `{"event_name":"ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED","event_data":{"name":1,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunCreated":{"jobId":"job-replay","jobRunId":"datasync-before"}}}`
	goldenFailedBody    = `{"event_name":"ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED","event_data":{"name":2,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunFailed":{"jobId":"job-replay","jobRunId":"datasync-before"}}}`
)

// The body of a webhook is a contract with its receivers: these are its exact bytes.
func Test_Body_IsTheExactDocumentOfTheEvent(t *testing.T) {
	tests := []struct {
		name  string
		event *runevents.Event
		want  string
	}{
		{"succeeded", goldenRun.Succeeded(goldenAt), goldenSucceededBody},
		{"created", goldenRun.Created(goldenAt), goldenCreatedBody},
		{"failed", goldenRun.Failed(goldenAt), goldenFailedBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bodyOf(tt.event)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func Test_Body_EscapesAsGoDoes(t *testing.T) {
	run := runevents.Run{AccountID: "a<b>&c", JobID: "", RunID: ""}
	got, err := bodyOf(run.Created(goldenAt))
	require.NoError(t, err)
	// The three characters are written as their six-character escapes, a backslash first.
	escaped := "a" + `\` + "u003cb" + `\` + "u003e" + `\` + "u0026c"
	require.Equal(
		t,
		`{"event_name":"ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED","event_data":{"name":1,"accountId":"`+escaped+`","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunCreated":{"jobId":"","jobRunId":""}}}`,
		string(got),
	)
}

func Test_Body_RefusesAnEventThatHasNoKind(t *testing.T) {
	_, err := bodyOf(&runevents.Event{})
	require.Error(t, err)
}
