package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// endpoint is a server that speaks the chat completion API. It keeps what it was asked.
type endpoint struct {
	server *httptest.Server
	answer func(call int, request map[string]any) (status int, body string)

	mu       sync.Mutex
	bodies   []string
	requests []map[string]any
	headers  []http.Header
	paths    []string
}

func newEndpoint(t *testing.T, answer func(call int, request map[string]any) (int, string)) *endpoint {
	t.Helper()
	e := &endpoint{answer: answer}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var request map[string]any
		_ = json.Unmarshal(body, &request)

		e.mu.Lock()
		e.bodies = append(e.bodies, string(body))
		e.requests = append(e.requests, request)
		e.headers = append(e.headers, r.Header.Clone())
		e.paths = append(e.paths, r.Method+" "+r.URL.Path)
		call := len(e.bodies)
		e.mu.Unlock()

		status, answer := e.answer(call, request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *endpoint) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.bodies)
}

// classifier returns a classifier that asks the endpoint and does not wait between two
// tries; the waits it was asked for are kept in waited.
func (e *endpoint) classifier(t *testing.T, cfg Config) (c *Classifier, waited *[]time.Duration) {
	t.Helper()
	cfg.BaseURL = e.server.URL + "/v1"
	if cfg.Model == "" {
		cfg.Model = "test-model"
	}
	c, err := NewClassifier(cfg)
	require.NoError(t, err)
	waited = &[]time.Duration{}
	c.wait = func(_ context.Context, d time.Duration) error {
		*waited = append(*waited, d)
		return nil
	}
	return c, waited
}

// sentUserMessage returns the document and the rest of the user message of a request.
func sentUserMessage(t *testing.T, request map[string]any) (document map[string]any, rest string) {
	t.Helper()
	content := messageContent(t, request, 1, "user")
	decoder := json.NewDecoder(strings.NewReader(content))
	require.NoError(t, decoder.Decode(&document))
	return document, content[decoder.InputOffset():]
}

func messageContent(t *testing.T, request map[string]any, index int, role string) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 2)
	message, ok := messages[index].(map[string]any)
	require.True(t, ok)
	require.Equal(t, role, message["role"])
	content, ok := message["content"].(string)
	require.True(t, ok)
	return content
}

// askedNames returns the names of the columns of a request, by their id.
func askedNames(t *testing.T, request map[string]any) map[string]string {
	t.Helper()
	document, _ := sentUserMessage(t, request)
	names := map[string]string{}
	for id, column := range document["columns"].(map[string]any) {
		names[id] = column.(map[string]any)["name"].(string)
	}
	return names
}

type answer struct {
	Category   any `json:"category,omitempty"`
	Confidence any `json:"confidence,omitempty"`
}

// completion is the body of a successful answer whose message holds content.
func completion(content string) string {
	return completionEnding(content, "stop")
}

func completionEnding(content, finishReason string) string {
	body, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": finishReason,
			"message": map[string]any{"role": "assistant", "content": content},
		}},
	})
	return string(body)
}

func answers(byId map[string]answer) string {
	content, _ := json.Marshal(byId)
	return completion(string(content))
}

func errorBody(kind, code, message string) string {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "code": code, "message": message}})
	return string(body)
}
