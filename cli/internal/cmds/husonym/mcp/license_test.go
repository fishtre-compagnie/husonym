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
	mcp_server "github.com/fishtre-compagnie/husonym/cli/internal/mcp"
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

// The feature is allowed when the license is valid and lists mcp. A license that is not in
// force is told apart from one that lacks the feature: it includes none.
func Test_licenseGate_AllowsWhenTheLicenseIsValidAndListsMcp(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		license *mgmtv1alpha1.SystemLicense
		want    bool
		// notInForce is set when the answer is that no license is in force.
		notInForce bool
	}{
		"valid, mcp listed":     {license: systemLicense(true, "valid", "api_keys", "mcp"), want: true},
		"in grace, mcp listed":  {license: systemLicense(true, "grace", "mcp"), want: true},
		"valid, mcp not listed": {license: systemLicense(true, "valid", "api_keys")},
		"valid, no feature":     {license: systemLicense(true, "valid")},
		"not valid, mcp listed": {license: systemLicense(false, "frozen", "mcp"), notInForce: true},
		"not valid, no state":   {license: systemLicense(false, ""), notInForce: true},
		"no license at all":     {notInForce: true},
		// An API older than the features sends a license with an empty state, and no list.
		"old API, valid":   {license: systemLicense(true, ""), want: true},
		"old API, invalid": {license: systemLicense(false, ""), notInForce: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			system := &fakeSystem{}
			system.set(tc.license, nil)
			gate, _ := gateOn(t, system)

			got, err := gate.Allowed(t.Context())
			if tc.notInForce {
				require.ErrorIs(t, err, mcp_server.ErrNoLicenseInForce)
				require.False(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// That no license is in force is an answer of the API, not a failure to read it: it is kept
// like any other answer.
func Test_licenseGate_KeepsTheAnswerThatNoLicenseIsInForce(t *testing.T) {
	t.Parallel()
	system := &fakeSystem{}
	system.set(systemLicense(false, "frozen", "mcp"), nil)
	gate, now := gateOn(t, system)

	for range 3 {
		_, err := gate.Allowed(t.Context())
		require.ErrorIs(t, err, mcp_server.ErrNoLicenseInForce)
	}
	require.Equal(t, 1, system.readCount())

	system.set(systemLicense(true, "valid", "mcp"), nil)
	*now = now.Add(licenseKept)
	allowed, err := gate.Allowed(t.Context())
	require.NoError(t, err)
	require.True(t, allowed)
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

	// The answers are collected and looked at once every call is back: a failed requirement
	// stops the goroutine that makes it, which must be the one of the test.
	type answer struct {
		allowed bool
		err     error
	}
	answers := make([]answer, 20)
	var wg sync.WaitGroup
	for i := range answers {
		wg.Go(func() {
			allowed, err := gate.Allowed(t.Context())
			answers[i] = answer{allowed: allowed, err: err}
		})
	}
	wg.Wait()
	for _, got := range answers {
		require.NoError(t, got.err)
		require.True(t, got.allowed)
	}
	require.Equal(t, 1, system.readCount())
}
