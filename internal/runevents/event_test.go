package runevents

import (
	"encoding/json"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The numbers are stored in workflow histories, in the database and sent to webhooks.
func Test_Run_Constructors_Kinds(t *testing.T) {
	run := Run{AccountID: "acc", JobID: "job", RunID: "run"}
	at := time.Date(2026, time.October, 3, 7, 51, 52, 0, time.UTC)

	tests := []struct {
		name   string
		event  *Event
		number int32
		text   string
	}{
		{"created", run.Created(at), 1, "ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED"},
		{"failed", run.Failed(at), 2, "ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED"},
		{"succeeded", run.Succeeded(at), 3, "ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, mgmtv1alpha1.AccountHookEvent(tt.number), tt.event.Kind())
			require.Equal(t, tt.text, tt.event.Kind().String())
			require.Equal(t, "acc", tt.event.AccountID())
		})
	}
}

func Test_Run_Constructors_StampUTC(t *testing.T) {
	paris := time.FixedZone("UTC+2", 2*60*60)
	at := time.Date(2026, time.October, 3, 9, 51, 52, 716809604, paris)

	encoded, err := json.Marshal(Run{AccountID: "acc", JobID: "job", RunID: "run"}.Created(at))
	require.NoError(t, err)
	require.Equal(
		t,
		`{"name":1,"accountId":"acc","timestamp":"2026-10-03T07:51:52.716809604Z","jobRunCreated":{"jobId":"job","jobRunId":"run"}}`,
		string(encoded),
	)
}
