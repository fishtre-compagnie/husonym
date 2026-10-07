package license

import (
	"errors"

	"connectrpc.com/connect"
)

// Gate names what a license refused: a feature it does not include, no license in force, or the
// cap on sources.
type Gate string

const (
	// GateNotInForce is a refusal because no license is in force: there is none, or it has lapsed.
	GateNotInForce Gate = "license_not_in_force"
	// GateSourceCap is a refusal because a write would bring the instance over its cap on sources.
	GateSourceCap Gate = "source_cap"
)

// FeatureGate is the gate of a refusal because the license does not include the feature.
func FeatureGate(f Feature) Gate {
	return Gate(f)
}

// AllGates returns every gate, in a stable order: the gate of each feature in the order of
// AllFeatures, then GateNotInForce and GateSourceCap.
func AllGates() []Gate {
	features := AllFeatures()
	gates := make([]Gate, 0, len(features)+2)
	for _, f := range features {
		gates = append(gates, FeatureGate(f))
	}
	return append(gates, GateNotInForce, GateSourceCap)
}

// Refusal is the error of anything the license refuses. Cause is what the caller is answered:
// the code and the sentence a client receives are the ones of the cause.
type Refusal struct {
	AccountId string
	// Gates are what refused; a job that uses several missing features names each of them.
	Gates []Gate
	Cause error
}

// NewRefusal refuses the account with the cause, naming the gates that closed.
func NewRefusal(accountId string, cause error, gates ...Gate) *Refusal {
	return &Refusal{AccountId: accountId, Gates: gates, Cause: cause}
}

func (r *Refusal) Error() string {
	return r.Cause.Error()
}

// Unwrap gives the answer of a handler that returns the refusal to its caller.
func (r *Refusal) Unwrap() error {
	return r.Cause
}

// Message is the sentence a person is told, without the code that Error puts before it when the
// cause is a Connect error.
func (r *Refusal) Message() string {
	var connectErr *connect.Error
	if errors.As(r.Cause, &connectErr) {
		return connectErr.Message()
	}
	return r.Cause.Error()
}
