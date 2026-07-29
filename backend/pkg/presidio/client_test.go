package presidio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAnalyze(t *testing.T) {
	var gotBody analyzePayload
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"entity_type":"EMAIL_ADDRESS","start":0,"end":10,"score":1.0}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/", WithHeaders(map[string]string{"Authorization": "Bearer x"}))
	threshold := 0.5
	results, err := c.Analyze(context.Background(), AnalyzeRequest{
		Text:           "a@b.com",
		Language:       "en",
		ScoreThreshold: threshold,
	})
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if len(results) != 1 || results[0].EntityType != "EMAIL_ADDRESS" || results[0].Score != 1.0 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if gotBody.Text != "a@b.com" || gotBody.Language != "en" {
		t.Errorf("unexpected request body: %+v", gotBody)
	}
	if gotBody.ScoreThreshold == nil || *gotBody.ScoreThreshold != 0.5 {
		t.Errorf("score_threshold not forwarded: %+v", gotBody.ScoreThreshold)
	}
	if gotAuth != "Bearer x" {
		t.Errorf("auth header not forwarded: %q", gotAuth)
	}
}

func TestClientAnalyzeNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Analyze(context.Background(), AnalyzeRequest{Text: "x", Language: "en"})
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}
}
