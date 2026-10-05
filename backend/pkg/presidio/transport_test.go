package presidio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	http_client "github.com/fishtre-compagnie/husonym/internal/http/client"
	"github.com/stretchr/testify/require"
)

// received is what a simulated Presidio got from the client.
type received struct {
	mu     sync.Mutex
	method string
	path   string
	query  string
	header http.Header
	body   string
}

func (r *received) set(req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.method, r.path, r.query = req.Method, req.URL.Path, req.URL.RawQuery
	r.header, r.body = req.Header.Clone(), string(body)
}

// answering starts a simulated Presidio that gives one answer to every request.
func answering(t *testing.T, status int, contentType, body string) (*httptest.Server, *received) {
	t.Helper()
	got := &received{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.set(r)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func answeringJSON(t *testing.T, status int, body string) (*httptest.Server, *received) {
	t.Helper()
	return answering(t, status, "application/json", body)
}

// silent starts a simulated Presidio that never answers.
func silent(t *testing.T) *httptest.Server {
	t.Helper()
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })
	return srv
}

func recorded(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(content)
}

func analyzerAt(t *testing.T, url string) *AnalyzerClient {
	t.Helper()
	client, err := NewAnalyzerClient(url, &http.Client{})
	require.NoError(t, err)
	return client
}

// operations runs each of the two operations against one base URL, with a valid request.
func operations(t *testing.T, url string, httpClient *http.Client) map[string]func(context.Context) error {
	t.Helper()
	analyzer, err := NewAnalyzerClient(url, httpClient)
	require.NoError(t, err)
	return map[string]func(context.Context) error{
		"analyze": func(ctx context.Context) error {
			_, err := analyzer.Analyze(ctx, &AnalyzeRequest{Text: "x", Language: "en"})
			return err
		},
		"supported entities": func(ctx context.Context) error {
			_, err := analyzer.SupportedEntities(ctx, "en")
			return err
		},
	}
}

// A Presidio that answers and does not do what was asked says why: its status and its message
// reach the caller, the same way for both operations.
func TestRefusal_CarriesStatusAndMessage(t *testing.T) {
	answers := map[string]struct {
		status  int
		body    string
		message string
	}{
		"analyzer without a language": {
			http.StatusInternalServerError, recorded(t, "analyze_no_language.json"), "No language provided",
		},
		"analyzer without the language": {
			http.StatusInternalServerError, recorded(t, "analyze_unsupported_language.json"),
			"No matching recognizers were found to serve the request.",
		},
		"a request the service cannot read": {
			http.StatusBadRequest,
			`{"error": "The browser (or proxy) sent a request that this server could not understand."}`,
			"The browser (or proxy) sent a request that this server could not understand.",
		},
	}
	for name, answer := range answers {
		srv, _ := answeringJSON(t, answer.status, answer.body)
		for operation, call := range operations(t, srv.URL, &http.Client{}) {
			t.Run(name+"/"+operation, func(t *testing.T) {
				err := call(context.Background())
				var refused *RefusedError
				require.ErrorAs(t, err, &refused)
				require.Equal(t, operation, refused.Operation)
				require.Equal(t, answer.status, refused.StatusCode)
				require.Equal(t, answer.message, refused.Message)
				require.NotErrorIs(t, err, ErrNoAnswer)
				require.NotErrorIs(t, err, ErrInvalidResponse)
				require.Contains(t, err.Error(), answer.message)
			})
		}
	}

	t.Run("an answer that is not JSON is quoted, cut short", func(t *testing.T) {
		srv, _ := answering(t, http.StatusForbidden, "text/html", strings.Repeat("a", 5000))
		_, err := analyzerAt(t, srv.URL).Analyze(context.Background(), &AnalyzeRequest{Text: "x", Language: "en"})
		var refused *RefusedError
		require.ErrorAs(t, err, &refused)
		require.Equal(t, http.StatusForbidden, refused.StatusCode)
		require.Equal(t, strings.Repeat("a", 512), refused.Message)
	})
}

// A Presidio that cannot be reached, or that a proxy answers for, is told apart from one that
// refuses: it will not answer the next text either.
func TestNoAnswer(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	for operation, call := range operations(t, gone.URL, &http.Client{}) {
		t.Run("unreachable/"+operation, func(t *testing.T) {
			require.ErrorIs(t, call(context.Background()), ErrNoAnswer)
		})
	}

	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		srv, _ := answering(t, status, "text/plain", "upstream")
		for operation, call := range operations(t, srv.URL, &http.Client{}) {
			t.Run(http.StatusText(status)+"/"+operation, func(t *testing.T) {
				err := call(context.Background())
				require.ErrorIs(t, err, ErrNoAnswer)
				var refused *RefusedError
				require.NotErrorAs(t, err, &refused)
			})
		}
	}
}

// A Presidio that never answers does not hold the caller: the client gives up after its own
// limit, sooner when the caller's is shorter, at once when the caller gives up.
func TestCallIsBounded(t *testing.T) {
	require.Equal(t, time.Minute, Timeout)

	t.Run("the client is built with its limit", func(t *testing.T) {
		require.Equal(t, Timeout, analyzerAt(t, "http://presidio").endpoint.timeout)
	})

	t.Run("the limit of the client", func(t *testing.T) {
		client := analyzerAt(t, silent(t).URL)
		client.endpoint.timeout = 50 * time.Millisecond
		start := time.Now()
		_, err := client.Analyze(context.Background(), &AnalyzeRequest{Text: "x", Language: "en"})
		require.ErrorIs(t, err, ErrNoAnswer)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(start), 10*time.Second)
	})

	t.Run("a shorter limit of the caller", func(t *testing.T) {
		client := analyzerAt(t, silent(t).URL)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := client.Analyze(ctx, &AnalyzeRequest{Text: "x", Language: "en"})
		require.ErrorIs(t, err, ErrNoAnswer)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(start), 10*time.Second)
	})

	t.Run("a caller that gives up", func(t *testing.T) {
		client := analyzerAt(t, silent(t).URL)
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		_, err := client.SupportedEntities(ctx, "en")
		require.ErrorIs(t, err, ErrNoAnswer)
		require.ErrorIs(t, err, context.Canceled)
		require.Less(t, time.Since(start), 10*time.Second)
	})
}

// An answer larger than any text Presidio is asked about is not read to its end.
func TestAnswerTooLarge(t *testing.T) {
	huge := `["` + strings.Repeat("A", maxAnswerBytes) + `"]`
	srv, _ := answeringJSON(t, http.StatusOK, huge)
	_, err := analyzerAt(t, srv.URL).SupportedEntities(context.Background(), "en")
	require.ErrorIs(t, err, ErrInvalidResponse)
	require.NotErrorIs(t, err, ErrNoAnswer)
}

// A success is read as JSON whatever it is labeled, and one that is not JSON is not understood.
func TestAnswerNotJSON(t *testing.T) {
	t.Run("JSON labeled as something else is read", func(t *testing.T) {
		srv, _ := answering(t, http.StatusOK, "text/plain", `["PERSON"]`)
		entities, err := analyzerAt(t, srv.URL).SupportedEntities(context.Background(), "en")
		require.NoError(t, err)
		require.Equal(t, []string{"PERSON"}, entities)
	})

	srv, _ := answering(t, http.StatusOK, "text/html", "<html>login</html>")
	for operation, call := range operations(t, srv.URL, &http.Client{}) {
		t.Run(operation, func(t *testing.T) {
			err := call(context.Background())
			require.ErrorIs(t, err, ErrInvalidResponse)
			require.NotErrorIs(t, err, ErrNoAnswer)
		})
	}
}

// The authorization the deployment configures accompanies every call, as it was given.
func TestAuthorizationHeaderIsSent(t *testing.T) {
	srv, got := answeringJSON(t, http.StatusOK, `[]`)
	httpClient := http_client.WithAuth(&http.Client{}, "opaque-token")
	for operation, call := range operations(t, srv.URL, httpClient) {
		t.Run(operation, func(t *testing.T) {
			_ = call(context.Background())
			require.Equal(t, "opaque-token", got.header.Get("Authorization"))
			require.Equal(t, "application/json", got.header.Get("Accept"))
		})
	}
}

func TestNewClient_RefusesABadURL(t *testing.T) {
	for name, url := range map[string]string{
		"empty":       "",
		"relative":    "presidio/analyzer",
		"no host":     "http://",
		"unparsable":  "http://presidio:port",
		"not http(s)": "ftp://presidio",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewAnalyzerClient(url, &http.Client{})
			require.Error(t, err)
		})
	}

	t.Run("no HTTP client", func(t *testing.T) {
		_, err := NewAnalyzerClient("http://presidio", nil)
		require.Error(t, err)
	})

	t.Run("a base URL with a path and a trailing slash is kept", func(t *testing.T) {
		srv, got := answeringJSON(t, http.StatusOK, `[]`)
		_, err := analyzerAt(t, srv.URL+"/presidio/").SupportedEntities(context.Background(), "en")
		require.NoError(t, err)
		require.Equal(t, "/presidio/supportedentities", got.path)
	})
}

// The connection to Presidio is kept from one call to the next, after a refusal as well.
func TestConnectionIsReused(t *testing.T) {
	answers := []struct {
		status int
		body   string
	}{
		{http.StatusOK, `[]`},
		{http.StatusInternalServerError, `{"error":"No language provided"}`},
		{http.StatusOK, `[]`},
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := answers[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_, _ = io.WriteString(w, answer.body)
	}))
	t.Cleanup(srv.Close)

	client := analyzerAt(t, srv.URL)
	var reused []bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) },
	})
	_, err := client.SupportedEntities(ctx, "en")
	require.NoError(t, err)
	_, err = client.SupportedEntities(ctx, "en")
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	_, err = client.SupportedEntities(ctx, "en")
	require.NoError(t, err)

	require.Equal(t, []bool{false, true, true}, reused)
}
