// Package consistencykey resolves the key a run derives its deterministic outputs from.
//
// Both engines derive from it — Athanor for all of its transformers, Benthos for the
// permutation of TransformPhoneNumber in preserve_format — so it belongs to neither.
package consistencykey

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// Resolver walks the cascade:
//
//	account setting  →  ANONYMIZATION_CONSISTENCY_KEY  →  a key generated for the account
//
// A deployment that carries the variable keeps producing the outputs it produced: as long
// as an account holds no setting, the variable wins, and nothing is generated. Switching
// an account over to a key of its own is a deliberate act, because it changes its outputs.
//
// Where no variable is set — a new deployment — every account gets its own on its first
// run, so accounts are kept apart without anyone asking for it.
type Resolver struct {
	client        mgmtv1alpha1connect.AccountSettingServiceClient
	deploymentKey string
}

func NewResolver(
	client mgmtv1alpha1connect.AccountSettingServiceClient,
	deploymentKey string,
) *Resolver {
	return &Resolver{client: client, deploymentKey: deploymentKey}
}

// ForAccount returns the key the runs of this account derive from, empty when nothing
// gives one — which is what a deployment with no variable and an API that cannot keep a
// secret comes to.
//
// A failure to reach the API is an error, never a fallback: the two engines derive
// different values from different keys, so one table that derived from another key would
// break the foreign keys between the tables of the run. Better to try the activity again.
func (r *Resolver) ForAccount(ctx context.Context, accountId string) (string, error) {
	resp, err := r.client.GetAccountConsistencyKey(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetAccountConsistencyKeyRequest{
			AccountId: accountId,
			// One is drawn only when nothing else would give a key.
			GenerateIfAbsent: r.deploymentKey == "",
		}),
	)
	if err != nil {
		// An API that holds no account settings — no encryption password, or a version
		// older than this worker — leaves the deployment as it was before they existed.
		if connect.CodeOf(err) == connect.CodeUnimplemented {
			return r.deploymentKey, nil
		}
		return "", fmt.Errorf("unable to read the consistency key of account %s: %w", accountId, err)
	}
	if key := resp.Msg.GetKey(); key != "" {
		return key, nil
	}
	return r.deploymentKey, nil
}

// HasKey tells whether a run of this account would derive from a key, without drawing one:
// what computes the plan of a run without running it must not give the account a key.
// An account without a key of its own gets one on its first run, where the API can keep a
// secret; that the API cannot is only found out by drawing, so it counts as having one.
func (r *Resolver) HasKey(ctx context.Context, accountId string) (bool, error) {
	if r.deploymentKey != "" {
		return true, nil
	}
	_, err := r.client.GetAccountConsistencyKey(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetAccountConsistencyKeyRequest{AccountId: accountId}),
	)
	if connect.CodeOf(err) == connect.CodeUnimplemented {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("unable to read the consistency key of account %s: %w", accountId, err)
	}
	return true, nil
}
