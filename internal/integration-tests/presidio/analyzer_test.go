// Package presidio holds the tests of the analyzer image built from docker/presidio-fr.
//
// They build the image, which downloads PyTorch and a model: they run only when
// PRESIDIO_IMAGE_TESTS=1.
//
// What a run leaves on the machine: the image husonym-presidio-analyzer-test:latest (3.9 GB),
// kept so that the next run reuses its layers. A run after a change to docker/presidio-fr builds
// a new image under that name and leaves the previous one without a name. To remove them:
//
//	docker rmi husonym-presidio-analyzer-test:latest
//	docker images -a --filter dangling=true   # the unnamed ones, each removed by `docker rmi <id>`
package presidio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	imageTestsEnvKey = "PRESIDIO_IMAGE_TESTS"

	// The image keeps this name after the tests, so that a later run reuses its layers.
	analyzerImageRepo = "husonym-presidio-analyzer-test"
	analyzerImageTag  = "latest"
	analyzerPort      = "3000/tcp"

	// Relative to this package's directory, where `go test` runs.
	analyzerBuildContext = "../../../docker/presidio-fr"

	analyzerStartupTimeout = 5 * time.Minute
)

// analyzerContainer holds the analyzer test container and its base URL.
type analyzerContainer struct {
	URL           string
	TestContainer *testcontainers.DockerContainer
}

var (
	analyzerOnce sync.Once
	analyzer     *analyzerContainer
	errAnalyzer  error
)

// TestMain tears down the analyzer the tests started. It is started the first time a test
// needs it: one container serves the whole package.
func TestMain(m *testing.M) {
	code := m.Run()
	if analyzer != nil {
		_ = analyzer.TearDown(context.Background())
	}
	os.Exit(code)
}

// startAnalyzer returns the base URL of the analyzer built from docker/presidio-fr. The first
// call builds the image and starts the container; the test is skipped unless
// PRESIDIO_IMAGE_TESTS=1.
func startAnalyzer(t *testing.T) (baseURL string) {
	t.Helper()
	if os.Getenv(imageTestsEnvKey) != "1" {
		t.Skipf("skipping the analyzer image tests, set %s=1 to enable", imageTestsEnvKey)
	}
	analyzerOnce.Do(func() {
		analyzer, errAnalyzer = newAnalyzerContainer(context.Background())
	})
	require.NoError(t, errAnalyzer, "the analyzer image did not build or start")
	return analyzer.URL
}

// newAnalyzerContainer builds the image and starts a container that answers on /health.
func newAnalyzerContainer(ctx context.Context) (*analyzerContainer, error) {
	buildContext, err := filepath.Abs(analyzerBuildContext)
	if err != nil {
		return nil, err
	}
	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:        buildContext,
			Dockerfile:     "Dockerfile",
			Repo:           analyzerImageRepo,
			Tag:            analyzerImageTag,
			KeepImage:      true,
			BuildLogWriter: os.Stderr,
		}),
		testcontainers.WithExposedPorts(analyzerPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/health").WithPort(analyzerPort).WithStartupTimeout(analyzerStartupTimeout),
		),
	)
	if err != nil {
		return &analyzerContainer{TestContainer: container}, err
	}
	endpoint, err := container.PortEndpoint(ctx, analyzerPort, "http")
	if err != nil {
		return &analyzerContainer{TestContainer: container}, err
	}
	return &analyzerContainer{URL: endpoint, TestContainer: container}, nil
}

// TearDown terminates the container.
func (a *analyzerContainer) TearDown(ctx context.Context) error {
	if a.TestContainer != nil {
		return a.TestContainer.Terminate(ctx)
	}
	return nil
}

// finding is one result of POST /analyze. Start and End count characters, not bytes.
type finding struct {
	EntityType  string  `json:"entity_type"`
	Start       int     `json:"start"`
	End         int     `json:"end"`
	Score       float64 `json:"score"`
	Explanation struct {
		Recognizer string `json:"recognizer"`
	} `json:"analysis_explanation"`
}

// text returns the characters of source the finding covers.
func (f finding) text(source string) string {
	return string([]rune(source)[f.Start:f.End])
}

type analyzeRequest struct {
	Text                  string   `json:"text"`
	Language              string   `json:"language"`
	Entities              []string `json:"entities,omitempty"`
	ReturnDecisionProcess bool     `json:"return_decision_process"`
}

// analyze posts one text and returns its findings ordered by position, then by entity type.
func analyze(t *testing.T, baseURL, language, text string, entities ...string) []finding {
	t.Helper()
	body, err := json.Marshal(analyzeRequest{
		Text: text, Language: language, Entities: entities, ReturnDecisionProcess: true,
	})
	require.NoError(t, err)
	findings, err := decodeFindings(postJSON(t, baseURL+"/analyze", body))
	require.NoError(t, err)
	return findings
}

func decodeFindings(body []byte) ([]finding, error) {
	var findings []finding
	if err := json.Unmarshal(body, &findings); err != nil {
		return nil, fmt.Errorf("unexpected analyzer response %q: %w", body, err)
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		return a.EntityType < b.EntityType
	})
	return findings, nil
}

func postJSON(t *testing.T, endpoint string, body []byte) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return do(t, req)
}

func get(t *testing.T, endpoint string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, http.NoBody)
	require.NoError(t, err)
	return do(t, req)
}

func do(t *testing.T, req *http.Request) []byte {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equalf(t, http.StatusOK, resp.StatusCode, "%s %s: %s", req.Method, req.URL, body)
	return body
}

// supportedEntities returns the sorted entity types the analyzer declares for a language.
func supportedEntities(t *testing.T, baseURL, language string) []string {
	t.Helper()
	var entities []string
	body := get(t, baseURL+"/supportedentities?language="+url.QueryEscape(language))
	require.NoError(t, json.Unmarshal(body, &entities))
	sort.Strings(entities)
	return entities
}
