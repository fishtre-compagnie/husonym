// Package presidio est un client HTTP minimal pour le service Microsoft Presidio
// Analyzer (open-source, MIT). Il expose uniquement l'endpoint POST /analyze dont
// nous avons besoin pour le scan de contenu PII.
//
// This client is deliberately independent from the internal/ee/presidio package
// (which is Enterprise-licensed), so that content-based PII detection stays usable
// in production without depending on EE code. The Presidio REST API is public:
// https://microsoft.github.io/presidio/api-docs/api-docs.html
package presidio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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

// ErrNoAnswer is the error of an analysis the analyzer did not answer: it could not be
// reached, took too long, or was answered for by a proxy. An analyzer that answers with an
// error has one of its own: it was asked, and refused this text.
var ErrNoAnswer = errors.New("the analyzer did not answer")

// notTheAnalyzer says whether a status is that of what stands before the analyzer — a proxy
// that cannot reach it, or waited for it too long — or of an analyzer that takes no more.
func notTheAnalyzer(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
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

// analyzeTimeout is how long the analyzer is waited for, for one text. A text is short, and
// analyzed in milliseconds: past this, the analyzer is not answering, and the scan that asks
// it would wait without end.
const analyzeTimeout = 15 * time.Second

// NewClient crée un client pointant sur l'URL de base du serveur Presidio Analyzer.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: analyzeTimeout},
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
		return nil, fmt.Errorf("presidio analyze request failed: %w", errors.Join(ErrNoAnswer, err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("presidio analyze answer cut short: %w", errors.Join(ErrNoAnswer, err))
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf(
			"presidio analyze returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
		if notTheAnalyzer(resp.StatusCode) {
			return nil, errors.Join(ErrNoAnswer, err)
		}
		return nil, err
	}

	var results []AnalyzeResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("unable to decode analyze response: %w", err)
	}
	return results, nil
}
