// Package piitest holds what the tests of the PII detection packages share: a server
// that speaks the chat completion API, and the registration of activities in a test
// environment that runs activities only.
package piitest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// Request is a request the server was sent.
type Request struct {
	// Call is the rank of the request, from 1.
	Call   int
	Path   string // the method and the path
	Header http.Header
	Body   string
	// JSON is the body, decoded.
	JSON map[string]any
	// What the user message of the request describes: the table, and its columns by id.
	Table   string
	Columns map[string]map[string]any
}

// IDs returns the ids of the columns of the request, in their order: c1, c2, and so on.
func (r *Request) IDs() []string {
	ids := make([]string, 0, len(r.Columns))
	for i := 1; i <= len(r.Columns); i++ {
		ids = append(ids, fmt.Sprintf("c%d", i))
	}
	return ids
}

// ChatServer is a server that speaks the chat completion API. It keeps what it was sent
// and answers each request with what its answer function returns.
type ChatServer struct {
	URL string // the base URL to configure, with its /v1

	server   *httptest.Server
	mu       sync.Mutex
	requests []Request
}

// NewChatServer starts a server that is closed at the end of the test.
func NewChatServer(t *testing.T, answer func(request *Request) (status int, body string)) *ChatServer {
	t.Helper()
	s := &ChatServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		request := Request{Path: r.Method + " " + r.URL.Path, Header: r.Header.Clone(), Body: string(body)}
		_ = json.Unmarshal(body, &request.JSON)
		var envelope struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &envelope)
		if len(envelope.Messages) == 2 {
			var document struct {
				Table   string                    `json:"table"`
				Columns map[string]map[string]any `json:"columns"`
			}
			_ = json.NewDecoder(strings.NewReader(envelope.Messages[1].Content)).Decode(&document)
			request.Table, request.Columns = document.Table, document.Columns
		}

		s.mu.Lock()
		request.Call = len(s.requests) + 1
		s.requests = append(s.requests, request)
		s.mu.Unlock()

		status, answer := answer(&request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	s.URL = s.server.URL + "/v1"
	t.Cleanup(s.server.Close)
	return s
}

// Close stops the server before the end of the test.
func (s *ChatServer) Close() {
	s.server.Close()
}

// Requests returns the requests the server was sent, in their order.
func (s *ChatServer) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request{}, s.requests...)
}

// Completion is the body of a successful answer whose message holds content.
func Completion(content string) string {
	return CompletionEnding(content, "stop")
}

// CompletionEnding is the body of a successful answer that ended for a reason.
func CompletionEnding(content, finishReason string) string {
	body, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": finishReason,
			"message": map[string]any{"role": "assistant", "content": content},
		}},
	})
	return string(body)
}

// Answers is the body of an answer that gives each id a category and a confidence.
func Answers(byId map[string][2]any) string {
	content := map[string]any{}
	for id, answer := range byId {
		content[id] = map[string]any{"category": answer[0], "confidence": answer[1]}
	}
	encoded, _ := json.Marshal(content)
	return Completion(string(encoded))
}

// EveryColumn answers the same for every column of a request.
func EveryColumn(category string, confidence float64) func(*Request) (int, string) {
	return func(request *Request) (int, string) {
		byId := map[string][2]any{}
		for _, id := range request.IDs() {
			byId[id] = [2]any{category, confidence}
		}
		return http.StatusOK, Answers(byId)
	}
}

// ActivityRegistry registers the activities of a package in an environment that runs
// activities only: the workflows it is given are left out.
type ActivityRegistry struct {
	Env *testsuite.TestActivityEnvironment
}

func (r ActivityRegistry) RegisterWorkflow(any) {}

func (r ActivityRegistry) RegisterActivity(a any) { r.Env.RegisterActivity(a) }
