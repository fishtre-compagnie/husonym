package mcp_cmd

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/require"
)

// fakeSystem answers GetSystemInformation with the license it holds, or the error it holds.
type fakeSystem struct {
	mgmtv1alpha1connect.UnimplementedUserAccountServiceHandler

	mu      sync.Mutex
	license *mgmtv1alpha1.SystemLicense
	err     error
	reads   int
}

func (f *fakeSystem) GetSystemInformation(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetSystemInformationRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemInformationResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemInformationResponse{License: f.license}), nil
}

func (f *fakeSystem) set(license *mgmtv1alpha1.SystemLicense, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.license, f.err = license, err
}

func (f *fakeSystem) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// gateOn returns a gate reading the fake API, on a clock the test moves.
func gateOn(t *testing.T, system *fakeSystem) (*licenseGate, *time.Time) {
	t.Helper()
	_, handler := mgmtv1alpha1connect.NewUserAccountServiceHandler(system)
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)

	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	gate := newLicenseGate(mgmtv1alpha1connect.NewUserAccountServiceClient(api.Client(), api.URL))
	gate.now = func() time.Time { return now }
	return gate, &now
}

func systemLicense(valid bool, state string, features ...string) *mgmtv1alpha1.SystemLicense {
	return &mgmtv1alpha1.SystemLicense{IsValid: valid, State: state, Features: features}
}

// The feature is allowed when the license is valid and lists mcp.
func Test_licenseGate_AllowsWhenTheLicenseIsValidAndListsMcp(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		license *mgmtv1alpha1.SystemLicense
		want    bool
	}{
		"valid, mcp listed":     {systemLicense(true, "valid", "api_keys", "mcp"), true},
		"in grace, mcp listed":  {systemLicense(true, "grace", "mcp"), true},
		"valid, mcp not listed": {systemLicense(true, "valid", "api_keys"), false},
		"valid, no feature":     {systemLicense(true, "valid"), false},
		"not valid, mcp listed": {systemLicense(false, "frozen", "mcp"), false},
		"not valid, no state":   {systemLicense(false, ""), false},
		"no license at all":     {nil, false},
		// An API older than the features sends a license with an empty state, and no list.
		"old API, valid":   {systemLicense(true, ""), true},
		"old API, invalid": {systemLicense(false, ""), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			system := &fakeSystem{}
			system.set(tc.license, nil)
			gate, _ := gateOn(t, system)

			got, err := gate.Allowed(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// The answer is kept for a minute: two calls within it read the API once, and the first after it
// reads it again, and sees the license it holds then.
func Test_licenseGate_KeepsTheAnswerForAMinute(t *testing.T) {
	t.Parallel()
	system := &fakeSystem{}
	system.set(systemLicense(true, "valid", "mcp"), nil)
	gate, now := gateOn(t, system)

	allowed, err := gate.Allowed(t.Context())
	require.NoError(t, err)
	require.True(t, allowed)

	system.set(systemLicense(true, "valid"), nil)
	*now = now.Add(59 * time.Second)
	allowed, err = gate.Allowed(t.Context())
	require.NoError(t, err)
	require.True(t, allowed, "the answer kept was not served")
	require.Equal(t, 1, system.readCount())

	*now = now.Add(time.Second)
	allowed, err = gate.Allowed(t.Context())
	require.NoError(t, err)
	require.False(t, allowed, "the answer was kept past a minute")
	require.Equal(t, 2, system.readCount())
}

// An error is not kept: the next call asks the API again.
func Test_licenseGate_DoesNotKeepAnError(t *testing.T) {
	t.Parallel()
	system := &fakeSystem{}
	system.set(nil, connect.NewError(connect.CodeUnavailable, nil))
	gate, _ := gateOn(t, system)

	_, err := gate.Allowed(t.Context())
	require.Error(t, err)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))

	system.set(systemLicense(true, "valid", "mcp"), nil)
	allowed, err := gate.Allowed(t.Context())
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 2, system.readCount())
}

// Tool calls overlap: the gate answers them all, and asks the API once.
func Test_licenseGate_SafeForOverlappingCalls(t *testing.T) {
	t.Parallel()
	system := &fakeSystem{}
	system.set(systemLicense(true, "valid", "mcp"), nil)
	gate, _ := gateOn(t, system)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			allowed, err := gate.Allowed(t.Context())
			require.NoError(t, err)
			require.True(t, allowed)
		})
	}
	wg.Wait()
	require.Equal(t, 1, system.readCount())
}
