// Package runevents defines the lifecycle events of a job run: created, failed, succeeded.
package runevents

import (
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Run identifies the job run an event is about. RunID is the identifier of the root
// workflow execution that owns the run's life.
type Run struct {
	AccountID string
	JobID     string
	RunID     string
}

// Created returns the "job run created" event of r, stamped with at converted to UTC.
func (r Run) Created(at time.Time) *Event {
	return r.event(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED, at)
}

// Failed returns the "job run failed" event of r, stamped with at converted to UTC.
func (r Run) Failed(at time.Time) *Event {
	return r.event(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED, at)
}

// Succeeded returns the "job run succeeded" event of r, stamped with at converted to UTC.
func (r Run) Succeeded(at time.Time) *Event {
	return r.event(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED, at)
}

func (r Run) event(kind mgmtv1alpha1.AccountHookEvent, at time.Time) *Event {
	return &Event{kind: kind, run: r, at: at.UTC()}
}

// Event is one lifecycle event of a job run. It comes from a Run or from decoding, and
// is never modified afterwards. The package reads no clock: the instant of an event is
// the one its builder was given.
type Event struct {
	kind mgmtv1alpha1.AccountHookEvent
	run  Run
	at   time.Time
}

// Kind returns the kind of the event. It is always one of the three job run kinds for
// an event that was built or decoded.
func (e *Event) Kind() mgmtv1alpha1.AccountHookEvent {
	return e.kind
}

// AccountID returns the account the run belongs to.
func (e *Event) AccountID() string {
	return e.run.AccountID
}
