package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/internal/piitest"
	"github.com/stretchr/testify/require"
)

// endpoint is a chat completion server whose answers are given the rank of the request
// and its decoded body.
type endpoint struct {
	*piitest.ChatServer
}

func newEndpoint(t *testing.T, answer func(call int, request map[string]any) (int, string)) *endpoint {
	t.Helper()
	return &endpoint{piitest.NewChatServer(t, func(request *piitest.Request) (int, string) {
		return answer(request.Call, request.JSON)
	})}
}

func (e *endpoint) calls() int {
	return len(e.Requests())
}

// classifier returns a classifier that asks the endpoint and does not wait between two
// tries; the waits it was asked for are kept in waited.
func (e *endpoint) classifier(t *testing.T, cfg Config) (c *Classifier, waited *[]time.Duration) {
	t.Helper()
	cfg.BaseURL = e.URL
	if cfg.Model == "" {
		cfg.Model = "test-model"
	}
	c, err := NewClassifier(&cfg)
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

// answer is what a test makes the model say of a column: any member may be left out or
// be of a wrong type.
type answer struct {
	Category   any `json:"category,omitempty"`
	Confidence any `json:"confidence,omitempty"`
}

func answers(byId map[string]answer) string {
	content, _ := json.Marshal(byId)
	return piitest.Completion(string(content))
}

func errorBody(kind, code, message string) string {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "code": code, "message": message}})
	return string(body)
}
