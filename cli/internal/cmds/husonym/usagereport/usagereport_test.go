package usagereport_cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/require"
)

const testDocument = `{"period":{"from":"2026-01","to":"2026-12"},"runs":12,"note":"a < b & c"}`

type fakeUsage struct {
	mgmtv1alpha1connect.UnimplementedUsageServiceHandler
	calls    atomic.Int32
	document string
	err      error
	got      *mgmtv1alpha1.GetUsagePeriodReportRequest
}

func (f *fakeUsage) GetUsagePeriodReport(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetUsagePeriodReportRequest],
) (*connect.Response[mgmtv1alpha1.GetUsagePeriodReportResponse], error) {
	f.calls.Add(1)
	f.got = req.Msg
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetUsagePeriodReportResponse{
		Document:       f.document,
		Seal:           "c2VhbA==",
		KeyFingerprint: "abc123",
	}), nil
}

func newClient(t *testing.T, f *fakeUsage) mgmtv1alpha1connect.UsageServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewUsageServiceHandler(f))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return mgmtv1alpha1connect.NewUsageServiceClient(srv.Client(), srv.URL)
}

func validOpts(output string) options {
	return options{from: "2026-01", to: "2026-12", output: output}
}

func Test_writeReport_FileHasTwoLines(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")

	err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, testDocument+"\n"+`{"seal":"c2VhbA==","key_fingerprint":"abc123"}`+"\n", string(got))
	require.Equal(t, "acc-1", f.got.GetAccountId())
	require.Equal(t, "2026-01", f.got.GetFromMonth())
	require.Equal(t, "2026-12", f.got.GetToMonth())

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func Test_writeReport_RefusesToOverwrite(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o600))

	err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
	require.ErrorContains(t, err, "--force")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "keep me", string(got))
}

func Test_writeReport_ForceOverwrites(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("old", 100)), 0o600))

	opts := validOpts(path)
	opts.force = true
	err := writeReport(t.Context(), newClient(t, f), "acc-1", opts, &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, testDocument+"\n"+`{"seal":"c2VhbA==","key_fingerprint":"abc123"}`+"\n", string(got))
}

func Test_writeReport_PrintIndentsWithoutChangingContent(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	var stdout, stderr bytes.Buffer

	opts := options{from: "2026-01", to: "2026-12", print: true}
	err := writeReport(t.Context(), newClient(t, f), "acc-1", opts, &stdout, &stderr)
	require.NoError(t, err)

	require.Contains(t, stdout.String(), "\n  \"runs\": 12,\n")
	require.JSONEq(t, testDocument, stdout.String())
	require.Empty(t, stderr.String())
}

func Test_writeReport_PrintWithOutputWritesBoth(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer

	opts := validOpts(path)
	opts.print = true
	require.NoError(t, writeReport(t.Context(), newClient(t, f), "acc-1", opts, &stdout, &bytes.Buffer{}))

	require.NotEmpty(t, stdout.String())
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(got), testDocument+"\n"))
}

func Test_writeReport_PrintsInvalidJsonAsItIs(t *testing.T) {
	f := &fakeUsage{document: "not json"}
	var stdout, stderr bytes.Buffer

	opts := options{from: "2026-01", to: "2026-12", print: true}
	require.NoError(t, writeReport(t.Context(), newClient(t, f), "acc-1", opts, &stdout, &stderr))

	require.Equal(t, "not json\n", stdout.String())
	require.Contains(t, stderr.String(), "not valid JSON")
}

func Test_writeReport_BadInputFailsBeforeAnyCall(t *testing.T) {
	for name, opts := range map[string]options{
		"bad from":      {from: "2026-1", to: "2026-12", print: true},
		"month 13":      {from: "2026-01", to: "2026-13", print: true},
		"month 00":      {from: "2026-00", to: "2026-12", print: true},
		"from after to": {from: "2026-12", to: "2026-01", print: true},
		"no output":     {from: "2026-01", to: "2026-12"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeUsage{document: testDocument}
			err := writeReport(t.Context(), newClient(t, f), "acc-1", opts, &bytes.Buffer{}, &bytes.Buffer{})
			require.Error(t, err)
			require.Zero(t, f.calls.Load())
		})
	}
}

func Test_writeReport_SameMonthIsAllowed(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	opts := options{from: "2026-03", to: "2026-03", print: true}
	require.NoError(t, writeReport(t.Context(), newClient(t, f), "acc-1", opts, &bytes.Buffer{}, &bytes.Buffer{}))
}

func Test_writeReport_ServerErrorIsReturnedAsIs(t *testing.T) {
	f := &fakeUsage{err: connect.NewError(connect.CodePermissionDenied, errTest("not allowed"))}
	path := filepath.Join(t.TempDir(), "report.json")

	err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.ErrorContains(t, err, "not allowed")
	require.NoFileExists(t, path)
}

type errTest string

func (e errTest) Error() string { return string(e) }
