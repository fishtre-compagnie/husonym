package runevents

import (
	"encoding/json"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

const (
	goldenCreated   = `{"name":1,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:52.716809604Z","jobRunCreated":{"jobId":"job-replay","jobRunId":"datasync-before"}}`
	goldenSucceeded = `{"name":3,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunSucceeded":{"jobId":"job-replay","jobRunId":"datasync-before"}}`
	goldenFailed    = `{"name":2,"accountId":"acc-replay","timestamp":"2026-10-03T07:51:53.058540394Z","jobRunFailed":{"jobId":"job-replay","jobRunId":"datasync-before"}}`
)

var (
	goldenRun = Run{AccountID: "acc-replay", JobID: "job-replay", RunID: "datasync-before"}
	createdAt = time.Date(2026, time.October, 3, 7, 51, 52, 716809604, time.UTC)
	endedAt   = time.Date(2026, time.October, 3, 7, 51, 53, 58540394, time.UTC)
)

// The input of the workflow that processes an event: the event under the field Event.
type wrapper struct {
	Event *Event
}

type goldenCase struct {
	name   string
	event  *Event
	kind   mgmtv1alpha1.AccountHookEvent
	golden string
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{
			name:   "created",
			event:  goldenRun.Created(createdAt),
			kind:   mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED,
			golden: goldenCreated,
		},
		{
			name:   "succeeded",
			event:  goldenRun.Succeeded(endedAt),
			kind:   mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
			golden: goldenSucceeded,
		},
		{
			name:   "failed",
			event:  goldenRun.Failed(endedAt),
			kind:   mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			golden: goldenFailed,
		},
	}
}

func Test_Event_MarshalJSON_Golden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			fromPointer, err := json.Marshal(tc.event)
			require.NoError(t, err)
			require.Equal(t, tc.golden, string(fromPointer))

			fromValue, err := json.Marshal(*tc.event)
			require.NoError(t, err)
			require.Equal(t, tc.golden, string(fromValue))

			wrapped, err := json.Marshal(&wrapper{Event: tc.event})
			require.NoError(t, err)
			require.Equal(t, `{"Event":`+tc.golden+`}`, string(wrapped))
		})
	}
}

func Test_Event_UnmarshalJSON_RoundTripsGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"Event":` + tc.golden + `}`

			var decoded wrapper
			require.NoError(t, json.Unmarshal([]byte(input), &decoded))
			require.NotNil(t, decoded.Event)
			require.Equal(t, tc.kind, decoded.Event.Kind())
			require.Equal(t, "acc-replay", decoded.Event.AccountID())

			encoded, err := json.Marshal(&decoded)
			require.NoError(t, err)
			require.Equal(t, input, string(encoded))
		})
	}
}

func Test_Event_UnmarshalJSON_NullEvent(t *testing.T) {
	var decoded wrapper
	require.NoError(t, json.Unmarshal([]byte(`{"Event":null}`), &decoded))
	require.Nil(t, decoded.Event)
}

func Test_Event_MarshalJSON_KeepsEmptyIds(t *testing.T) {
	encoded, err := json.Marshal(Run{}.Created(createdAt))
	require.NoError(t, err)
	require.Equal(
		t,
		`{"name":1,"accountId":"","timestamp":"2026-10-03T07:51:52.716809604Z","jobRunCreated":{"jobId":"","jobRunId":""}}`,
		string(encoded),
	)
}

func Test_Event_MarshalJSON_ZeroValue(t *testing.T) {
	_, err := json.Marshal(Event{})
	require.Error(t, err)

	_, err = json.Marshal(&wrapper{Event: &Event{}})
	require.Error(t, err)
}

func Test_Event_UnmarshalJSON_Rejects(t *testing.T) {
	const (
		head  = `"accountId":"acc","timestamp":"2026-10-03T07:51:52Z"`
		block = `{"jobId":"job","jobRunId":"run"}`
	)
	tests := []struct {
		name  string
		input string
	}{
		{"wildcard kind", `{"name":0,` + head + `}`},
		{"wildcard kind with a block", `{"name":0,` + head + `,"jobRunCreated":` + block + `}`},
		{"unknown kind", `{"name":4,` + head + `,"jobRunCreated":` + block + `}`},
		{"no kind", `{` + head + `,"jobRunCreated":` + block + `}`},
		{"no block", `{"name":1,` + head + `}`},
		{"block of another kind", `{"name":1,` + head + `,"jobRunFailed":` + block + `}`},
		{
			"two blocks",
			`{"name":1,` + head + `,"jobRunCreated":` + block + `,"jobRunSucceeded":` + block + `}`,
		},
		{
			"malformed timestamp",
			`{"name":1,"accountId":"acc","timestamp":"yesterday","jobRunCreated":` + block + `}`,
		},
		{"not an object", `"created"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event Event
			require.Error(t, json.Unmarshal([]byte(tt.input), &event))
		})
	}
}

func Test_Event_UnmarshalJSON_IgnoresUnknownFields(t *testing.T) {
	input := `{"name":2,"accountId":"acc","extra":{"a":1},"timestamp":"2026-10-03T07:51:52Z","jobRunFailed":{"jobId":"job","jobRunId":"run","more":true}}`

	var event Event
	require.NoError(t, json.Unmarshal([]byte(input), &event))
	require.Equal(t, mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED, event.Kind())

	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	require.Equal(
		t,
		`{"name":2,"accountId":"acc","timestamp":"2026-10-03T07:51:52Z","jobRunFailed":{"jobId":"job","jobRunId":"run"}}`,
		string(encoded),
	)
}
