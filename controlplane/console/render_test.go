package console

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/stretchr/testify/require"
)

// filesWith is the files of the console with one of them replaced.
func filesWith(t *testing.T, name, content string) fs.FS {
	t.Helper()
	files := fstest.MapFS{}
	require.NoError(t, fs.WalkDir(embedded, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(embedded, path)
		files[path] = &fstest.MapFile{Data: data}
		return err
	}))
	files[name] = &fstest.MapFile{Data: []byte(content)}
	return files
}

// noCustomers is a store that is only asked for the list of the customers, and has none.
type noCustomers struct{ Reader }

func (noCustomers) Customers(context.Context, time.Time) ([]cpstore.CustomerSummary, error) {
	return nil, nil
}

func Test_New_TemplateThatDoesNotParse_Fails(t *testing.T) {
	for name, content := range map[string]string{
		"an action left open":     `{{define "content"}}{{if}}{{end}}`,
		"no content for the page": `<p>nothing defined</p>`,
		"a script left open":      `{{define "content"}}<script>var title = "{{end}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newConsole(filesWith(t, "templates/customers.html", content), noCustomers{}, time.Now, slog.Default(), true)

			require.ErrorContains(t, err, "customers.html")
		})
	}
}

func Test_New_WithTheFilesOfTheBinary_Succeeds(t *testing.T) {
	_, err := newConsole(embedded, noCustomers{}, time.Now, slog.Default(), true)

	require.NoError(t, err)
}

// A template that fails once half the page is made must not leave half a page behind.
func Test_Render_TemplateFailingHalfway_AnswersTheFailurePageAlone(t *testing.T) {
	var logs bytes.Buffer
	files := filesWith(t, "templates/customers.html",
		`{{define "content"}}{{with .Body}}<p>BEFORE THE FAILURE</p>{{.NoSuchField}}{{end}}{{end}}`)
	pages, err := newConsole(files, noCustomers{}, time.Now, slog.New(slog.NewTextHandler(&logs, nil)), true)
	require.NoError(t, err)
	rec := httptest.NewRecorder()

	pages.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "BEFORE THE FAILURE")
	require.Contains(t, rec.Body.String(), "<h1>Something went wrong</h1>")
	require.Contains(t, logs.String(), "NoSuchField")
	require.Contains(t, logs.String(), "status=500")
}

// A panic is not left to net/http, which would cut the connection and, with the server of the
// command, say nothing.
func Test_Panic_AnswersTheFailurePageAndLogsFixedWords(t *testing.T) {
	var logs bytes.Buffer
	pages, err := newConsole(embedded, panicking{}, time.Now, slog.New(slog.NewTextHandler(&logs, nil)), true)
	require.NoError(t, err)
	rec := httptest.NewRecorder()

	pages.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "<h1>Something went wrong</h1>")
	require.NotContains(t, logs.String(), "WHAT WAS PANICKED WITH")
	require.Contains(t, logs.String(), "status=500")
}

type panicking struct{ Reader }

func (panicking) Customers(context.Context, time.Time) ([]cpstore.CustomerSummary, error) {
	panic("WHAT WAS PANICKED WITH")
}
