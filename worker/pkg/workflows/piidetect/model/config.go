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
	// defaultBaseURL is the OpenAI API, where requests go when no URL is set.
	defaultBaseURL = "https://api.openai.com/v1/"
)

// Config says which model is asked, where, with which credentials, and from which
// confidence an answer counts. It is all the client sends: the client reads no variable
// of the environment by itself.
type Config struct {
	BaseURL       string  // empty: the OpenAI API
	APIKey        string  // may be empty: a local server needs none
	Model         string  // empty: no model is configured
	MinConfidence float64 // an answer under it is kept out of the report

	// The organization and the project of an OpenAI account. They are sent to the OpenAI
	// API only.
	Organization string
	Project      string
}

// Enabled tells whether a model is configured.
func (c *Config) Enabled() bool {
	return c.Model != ""
}

// Host is where the requests go, without the path of the URL: it is what the worker logs
// when it starts.
func (c *Config) Host() string {
	parsed, err := url.Parse(c.baseURL())
	if err != nil || parsed.Host == "" {
		return "an unreadable URL"
	}
	return parsed.Host
}

func (c *Config) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return c.BaseURL
}

// Settings are the settings of a deployment, as it wrote them.
type Settings struct {
	URL           string // PII_DETECT_LLM_URL
	APIKey        string // PII_DETECT_LLM_API_KEY
	Model         string // PII_DETECT_LLM_MODEL
	MinConfidence string // PII_DETECT_LLM_MIN_CONFIDENCE

	// The variables of an OpenAI account. They stand in for a URL and a key that are not
	// set, each under the conditions NewConfig states.
	OpenAIBaseURL      string // OPENAI_BASE_URL
	OpenAIAPIKey       string // OPENAI_API_KEY
	OpenAIOrganization string // OPENAI_ORG_ID
	OpenAIProject      string // OPENAI_PROJECT_ID
}

// NewConfig reads the settings of a deployment. It refuses settings that cannot work, so
// that the worker says so when it starts and not at the first run.
//
// The URL is PII_DETECT_LLM_URL, else OPENAI_BASE_URL, else the OpenAI API. The key is
// PII_DETECT_LLM_API_KEY; OPENAI_API_KEY stands in for it only when the URL is not
// PII_DETECT_LLM_URL: the key of an OpenAI account is never sent to an endpoint that
// setting names, and such an endpoint is asked without a key by setting none. The
// organization and the project of the account are sent to the OpenAI API only.
//
// A model is configured when a model is named, or when a key is set: a deployment that
// only ever set a key keeps the model and the endpoint it has. A model named with neither
// a URL nor a key is refused: it would be asked at the OpenAI API, without a key.
func NewConfig(s *Settings) (Config, error) {
	cfg := Config{
		BaseURL:       strings.TrimSpace(s.URL),
		APIKey:        strings.TrimSpace(s.APIKey),
		Model:         strings.TrimSpace(s.Model),
		MinConfidence: defaultMinConfidence,
	}
	urlSetting := "PII_DETECT_LLM_URL"
	if cfg.BaseURL == "" {
		cfg.BaseURL, urlSetting = strings.TrimSpace(s.OpenAIBaseURL), "OPENAI_BASE_URL"
		if cfg.APIKey == "" {
			cfg.APIKey = strings.TrimSpace(s.OpenAIAPIKey)
		}
	}
	if cfg.BaseURL == "" {
		cfg.Organization = strings.TrimSpace(s.OpenAIOrganization)
		cfg.Project = strings.TrimSpace(s.OpenAIProject)
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
	if cfg.BaseURL == "" && cfg.APIKey == "" {
		// The client would ask the OpenAI API, without a key, and send it the profiles.
		return Config{}, errors.New(
			"PII_DETECT_LLM_MODEL is set without PII_DETECT_LLM_URL or PII_DETECT_LLM_API_KEY: " +
				"set the URL of the endpoint that serves the model, or the key of the account that does",
		)
	}
	if cfg.BaseURL != "" {
		if err := checkURL(cfg.BaseURL, urlSetting); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// checkURL refuses a URL the client could not use as it is written. The client sends
// neither the query nor the user of a base URL: a URL that holds one would be used
// without it, silently. The message never repeats the URL, which may hold a secret.
func checkURL(raw, setting string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an http or https URL", setting)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not hold a user or a password: the key of the endpoint is PII_DETECT_LLM_API_KEY", setting)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("%s must not hold a query or a fragment: they would not be sent", setting)
	}
	return nil
}
