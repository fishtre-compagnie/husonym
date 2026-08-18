// Package presidio est un client HTTP minimal pour le service Microsoft Presidio
// Analyzer (open-source, MIT). Il expose uniquement l'endpoint POST /analyze dont
// nous avons besoin pour le scan de contenu PII.
//
// Ce client est volontairement indépendant du paquet internal/ee/presidio (qui est
// sous licence Enterprise) afin que la détection PII par contenu reste utilisable
// en production sans dépendance à du code sous licence EE. L'API REST de Presidio
// est publique : https://microsoft.github.io/presidio/api-docs/api-docs.html
package presidio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnalyzeRequest est une requête d'analyse de texte.
type AnalyzeRequest struct {
	// Text est le texte à analyser (requis).
	Text string
	// Language est la langue du texte, ISO 639-1 (ex: "en"). Requis par Presidio.
	Language string
	// ScoreThreshold filtre les résultats sous ce score (0-1). Ignoré si <= 0.
	ScoreThreshold float64
}

// AnalyzeResult est une entité détectée par Presidio.
type AnalyzeResult struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// Analyzer est l'abstraction du service d'analyse (facilite les tests/mocks).
type Analyzer interface {
	Analyze(ctx context.Context, req AnalyzeRequest) ([]AnalyzeResult, error)
}

// Client appelle un serveur Presidio Analyzer.
type Client struct {
	baseURL    string
	httpClient *http.Client
	headers    map[string]string
}

// Option configure le Client.
type Option func(*Client)

// WithHTTPClient fournit un *http.Client personnalisé.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithHeaders ajoute des en-têtes HTTP (ex: Authorization) à chaque requête.
func WithHeaders(headers map[string]string) Option {
	return func(c *Client) { c.headers = headers }
}

// NewClient crée un client pointant sur l'URL de base du serveur Presidio Analyzer.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type analyzePayload struct {
	Text           string   `json:"text"`
	Language       string   `json:"language"`
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
}

// Analyze envoie le texte au endpoint POST /analyze et retourne les entités détectées.
func (c *Client) Analyze(ctx context.Context, req AnalyzeRequest) ([]AnalyzeResult, error) {
	payload := analyzePayload{Text: req.Text, Language: req.Language}
	if req.ScoreThreshold > 0 {
		threshold := req.ScoreThreshold
		payload.ScoreThreshold = &threshold
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal analyze request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/analyze",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to build analyze request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("presidio analyze request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"presidio analyze returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var results []AnalyzeResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("unable to decode analyze response: %w", err)
	}
	return results, nil
}
