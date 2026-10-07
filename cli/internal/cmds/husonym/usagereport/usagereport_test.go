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
	"github.com/spf13/cobra"
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

// Bad flags are refused by the command itself, before it builds a client or asks for anything.
func Test_Cmd_BadFlagsAreRefusedBeforeAnyCall(t *testing.T) {
	for name, tt := range map[string]struct {
		args []string
		msg  string
	}{
		"missing from":  {[]string{"--to", "2026-12", "--print"}, "--from is required"},
		"missing to":    {[]string{"--from", "2026-01", "--print"}, "--to is required"},
		"bad from":      {[]string{"--from", "2026-1", "--to", "2026-12", "--print"}, "--from is required"},
		"month 13":      {[]string{"--from", "2026-01", "--to", "2026-13", "--print"}, "--to is required"},
		"month 00":      {[]string{"--from", "2026-00", "--to", "2026-12", "--print"}, "--from is required"},
		"from after to": {[]string{"--from", "2026-12", "--to", "2026-01", "--print"}, "must not be after"},
		"no output":     {[]string{"--from", "2026-01", "--to", "2026-12"}, "at least one of"},
	} {
		t.Run(name, func(t *testing.T) {
			root := &cobra.Command{Use: "husonym", SilenceErrors: true, SilenceUsage: true}
			root.PersistentFlags().String("api-key", "", "")
			root.PersistentFlags().Bool("debug", false, "")
			root.AddCommand(NewCmd())
			root.SetArgs(append([]string{"usage-report"}, tt.args...))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			// A client built for a real address would fail differently; the message proves
			// the flags were refused first.
			t.Setenv("HUSONYM_API_URL", "http://127.0.0.1:1")
			err := root.ExecuteContext(t.Context())
			require.ErrorContains(t, err, tt.msg)
		})
	}
}

func Test_writeReport_ForceReplacesWith0600(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	opts := validOpts(path)
	opts.force = true
	require.NoError(t, writeReport(t.Context(), newClient(t, f), "acc-1", opts, &bytes.Buffer{}, &bytes.Buffer{}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left")
}

func Test_writeReport_RefusalLeavesNoTemporaryFile(t *testing.T) {
	f := &fakeUsage{document: testDocument}
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o600))

	err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func Test_writeReport_FailedWriteLeavesNothing(t *testing.T) {
	f := &fakeUsage{document: testDocument}

	t.Run("directory does not exist", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing", "report.json")
		err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
		require.Error(t, err)
		require.NoFileExists(t, path)
	})

	t.Run("read-only directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		path := filepath.Join(dir, "report.json")

		err := writeReport(t.Context(), newClient(t, f), "acc-1", validOpts(path), &bytes.Buffer{}, &bytes.Buffer{})
		require.Error(t, err)
		require.NoFileExists(t, path)
		entries, rerr := os.ReadDir(dir)
		require.NoError(t, rerr)
		require.Empty(t, entries)
	})
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
