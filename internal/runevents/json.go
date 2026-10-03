package runevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// wireEvent is the serialized form of an event. It is stored in workflow histories and
// sent to webhooks: the names, the order of the fields and the absence of the blocks of
// the other kinds are fixed. The kind is written as its number.
type wireEvent struct {
	Name      mgmtv1alpha1.AccountHookEvent `json:"name"`
	AccountID string                        `json:"accountId"`
	Timestamp time.Time                     `json:"timestamp"`
	Created   *wireRun                      `json:"jobRunCreated,omitempty"`
	Succeeded *wireRun                      `json:"jobRunSucceeded,omitempty"`
	Failed    *wireRun                      `json:"jobRunFailed,omitempty"`
}

// wireRun is the block of an event's kind. It is a pointer in wireEvent so that a block
// with empty identifiers is still written.
type wireRun struct {
	JobID string `json:"jobId"`
	RunID string `json:"jobRunId"`
}

// block returns the place of the block of the given kind, nil when the kind has none.
func (w *wireEvent) block(kind mgmtv1alpha1.AccountHookEvent) **wireRun {
	switch kind {
	case mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED:
		return &w.Created
	case mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED:
		return &w.Succeeded
	case mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED:
		return &w.Failed
	default:
		return nil
	}
}

func (w *wireEvent) blockCount() int {
	count := 0
	for _, block := range []*wireRun{w.Created, w.Succeeded, w.Failed} {
		if block != nil {
			count++
		}
	}
	return count
}

// MarshalJSON encodes the event in its fixed serialized form. It fails on the zero Event.
// Its receiver is a value so that an Event encodes the same whether it is held by value
// or by pointer.
//
//nolint:gocritic // hugeParam: the value receiver is what makes both forms encode
func (e Event) MarshalJSON() ([]byte, error) {
	wire := wireEvent{Name: e.kind, AccountID: e.run.AccountID, Timestamp: e.at}
	block := wire.block(e.kind)
	if block == nil {
		return nil, fmt.Errorf("event kind %d is not a job run event", e.kind)
	}
	*block = &wireRun{JobID: e.run.JobID, RunID: e.run.RunID}
	return json.Marshal(wire)
}

// UnmarshalJSON decodes the serialized form of an event. It fails unless the kind is a
// job run kind and the only block present is the one of that kind. JSON null is a no-op.
func (e *Event) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var wire wireEvent
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	block := wire.block(wire.Name)
	if block == nil {
		return fmt.Errorf("event kind %d is not a job run event", wire.Name)
	}
	if *block == nil {
		return fmt.Errorf("event of kind %s has no block of its kind", wire.Name)
	}
	if wire.blockCount() != 1 {
		return errors.New("event has blocks of several kinds")
	}
	*e = Event{
		kind: wire.Name,
		run:  Run{AccountID: wire.AccountID, JobID: (*block).JobID, RunID: (*block).RunID},
		at:   wire.Timestamp,
	}
	return nil
}
