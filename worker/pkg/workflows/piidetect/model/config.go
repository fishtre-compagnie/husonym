// Package model asks one language model, through an OpenAI-compatible chat completion
// endpoint, which columns of a table hold personal data. It builds the request, holds the
// answer to its form, and applies the confidence threshold.
//
// Nothing of a request or of an answer is logged here or put in an error, beyond what
// Error says of itself.
package model

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	// defaultModel is what a deployment that only sets a key asks.
	defaultModel         = "gpt-4o-mini"
	defaultMinConfidence = 0.5
	// defaultHost is where the client library sends requests when no URL is set.
	defaultHost = "api.openai.com"
)

// Config says which model is asked, where, and from which confidence an answer counts.
type Config struct {
	BaseURL       string  // empty: the default of the client library (the OpenAI API)
	APIKey        string  // may be empty: a local server needs none
	Model         string  // empty: no model is configured
	MinConfidence float64 // an answer under it is kept out of the report
}

// Enabled tells whether a model is configured.
func (c Config) Enabled() bool {
	return c.Model != ""
}

// Host is where the requests go, without the path, the credentials or the query of the
// URL: it is what the worker logs when it starts.
func (c Config) Host() string {
	if c.BaseURL == "" {
		return defaultHost
	}
	parsed, err := url.Parse(c.BaseURL)
	if err != nil || parsed.Host == "" {
		return "an unreadable URL"
	}
	return parsed.Host
}

// Settings are the settings of a deployment, as it wrote them.
type Settings struct {
	URL           string // PII_DETECT_LLM_URL
	APIKey        string // PII_DETECT_LLM_API_KEY
	Model         string // PII_DETECT_LLM_MODEL
	MinConfidence string // PII_DETECT_LLM_MIN_CONFIDENCE

	// The variables of the client library, which stand in for a URL and a key that are
	// not set.
	OpenAIBaseURL string // OPENAI_BASE_URL
	OpenAIAPIKey  string // OPENAI_API_KEY
}

// NewConfig reads the settings of a deployment. A model is configured when a model is
// named, or when a key is set: a deployment that only ever set a key keeps the model and
// the endpoint it has. It refuses settings that cannot work, so that the worker says so
// when it starts and not at the first run.
func NewConfig(s *Settings) (Config, error) {
	cfg := Config{
		BaseURL:       firstSet(s.URL, s.OpenAIBaseURL),
		APIKey:        firstSet(s.APIKey, s.OpenAIAPIKey),
		Model:         strings.TrimSpace(s.Model),
		MinConfidence: defaultMinConfidence,
	}
	if threshold := strings.TrimSpace(s.MinConfidence); threshold != "" {
		value, err := strconv.ParseFloat(threshold, 64)
		if err != nil || math.IsNaN(value) || value < 0 || value > 1 {
			return Config{}, fmt.Errorf("PII_DETECT_LLM_MIN_CONFIDENCE must be a number from 0 to 1, got %q", threshold)
		}
		cfg.MinConfidence = value
	}
	if cfg.Model == "" {
		if strings.TrimSpace(s.URL) != "" {
			return Config{}, errors.New(
				"PII_DETECT_LLM_URL is set without PII_DETECT_LLM_MODEL: the model of an endpoint cannot be guessed",
			)
		}
		if cfg.APIKey == "" {
			// No model and no key: detection runs on the rules.
			return Config{MinConfidence: cfg.MinConfidence}, nil
		}
		cfg.Model = defaultModel
	}
	if cfg.BaseURL != "" {
		parsed, err := url.Parse(cfg.BaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, errors.New(
				"the URL of the model endpoint (PII_DETECT_LLM_URL, or OPENAI_BASE_URL in its place) " +
					"must be an http or https URL",
			)
		}
	}
	return cfg, nil
}

func firstSet(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
