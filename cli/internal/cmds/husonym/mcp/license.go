package mcp_cmd

import (
	"context"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// licenseKept is how long the answer of the API about the license is kept: a license changes
// by the day, and a tool call must not wait for the API a second time for it.
const licenseKept = time.Minute

// licenseGate says whether the license of the instance includes the mcp feature, as the API
// reports it. The answer is kept for licenseKept; an error is not, so that the next call asks
// again. It is safe for the tool calls that overlap.
type licenseGate struct {
	client mgmtv1alpha1connect.UserAccountServiceClient
	now    func() time.Time

	mu      sync.Mutex
	allowed bool
	until   time.Time
}

func newLicenseGate(client mgmtv1alpha1connect.UserAccountServiceClient) *licenseGate {
	return &licenseGate{client: client, now: time.Now}
}

// Allowed is the answer the server asks for on each tool call. The lock is held through the
// call to the API: calls that overlap while it is read wait for that one answer rather than
// each asking.
func (g *licenseGate) Allowed(ctx context.Context) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.now().Before(g.until) {
		return g.allowed, nil
	}
	res, err := g.client.GetSystemInformation(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemInformationRequest{}))
	if err != nil {
		return false, err
	}
	g.allowed = includesMcp(res.Msg.GetLicense())
	g.until = g.now().Add(licenseKept)
	return g.allowed, nil
}

// includesMcp says whether the license the API reports allows the mcp feature.
func includesMcp(l *mgmtv1alpha1.SystemLicense) bool {
	if !l.GetIsValid() {
		return false
	}
	// An API older than the features says nothing of them, and has an empty state: the features
	// cannot be known, and a valid license then allows what it always did, mcp included.
	if l.GetState() == "" {
		return true
	}
	return slices.Contains(l.GetFeatures(), string(license.FeatureMcp))
}
