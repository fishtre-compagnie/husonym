package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_NewConfig(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     Config
	}{
		{
			"nothing is set: no model",
			Settings{},
			Config{MinConfidence: 0.5},
		},
		{
			"a local server needs a URL and a model, no key",
			Settings{URL: "http://llama:8080/v1", Model: "local-model"},
			Config{BaseURL: "http://llama:8080/v1", Model: "local-model", MinConfidence: 0.5},
		},
		{
			"every setting",
			Settings{URL: "https://models.example/v1", APIKey: "key", Model: "m", MinConfidence: "0.8"},
			Config{BaseURL: "https://models.example/v1", APIKey: "key", Model: "m", MinConfidence: 0.8},
		},
		{
			"a key alone keeps the model such a deployment has always asked",
			Settings{OpenAIAPIKey: "sk-1"},
			Config{APIKey: "sk-1", Model: "gpt-4o-mini", MinConfidence: 0.5},
		},
		{
			"the client library's own variables stand in for the missing settings",
			Settings{OpenAIBaseURL: "https://proxy.example/v1", OpenAIAPIKey: "sk-1"},
			Config{BaseURL: "https://proxy.example/v1", APIKey: "sk-1", Model: "gpt-4o-mini", MinConfidence: 0.5},
		},
		{
			"the key of OpenAI is not sent to the endpoint of the setting: no key is said by setting none",
			Settings{URL: "http://llama:8080/v1", Model: "m", OpenAIAPIKey: "sk-1", OpenAIBaseURL: "https://proxy.example/v1"},
			Config{BaseURL: "http://llama:8080/v1", Model: "m", MinConfidence: 0.5},
		},
		{
			"the key of the setting goes with the URL of the client library's variable",
			Settings{APIKey: "local", Model: "m", OpenAIBaseURL: "https://proxy.example/v1", OpenAIAPIKey: "sk-1"},
			Config{BaseURL: "https://proxy.example/v1", APIKey: "local", Model: "m", MinConfidence: 0.5},
		},
		{
			"the organization and the project of OpenAI go to the OpenAI API only",
			Settings{OpenAIAPIKey: "sk-1", OpenAIOrganization: "org-1", OpenAIProject: "proj-1"},
			Config{APIKey: "sk-1", Model: "gpt-4o-mini", MinConfidence: 0.5, Organization: "org-1", Project: "proj-1"},
		},
		{
			"and to no other endpoint",
			Settings{
				OpenAIBaseURL: "https://proxy.example/v1", OpenAIAPIKey: "sk-1",
				OpenAIOrganization: "org-1", OpenAIProject: "proj-1",
			},
			Config{BaseURL: "https://proxy.example/v1", APIKey: "sk-1", Model: "gpt-4o-mini", MinConfidence: 0.5},
		},
		{
			"a URL of the client library's variable without a key names no model",
			Settings{OpenAIBaseURL: "https://proxy.example/v1"},
			Config{MinConfidence: 0.5},
		},
		{
			"the settings win over the client library's variables",
			Settings{
				URL: "http://llama:8080/v1", APIKey: "local", Model: "m",
				OpenAIBaseURL: "https://proxy.example/v1", OpenAIAPIKey: "sk-1",
			},
			Config{BaseURL: "http://llama:8080/v1", APIKey: "local", Model: "m", MinConfidence: 0.5},
		},
		{
			"a model and the key of an account are asked at the OpenAI API",
			Settings{Model: "m", OpenAIAPIKey: "sk-1"},
			Config{Model: "m", APIKey: "sk-1", MinConfidence: 0.5},
		},
		{
			"a model at the endpoint of the client library's variable needs no key",
			Settings{Model: "m", OpenAIBaseURL: "http://llama:8080/v1"},
			Config{BaseURL: "http://llama:8080/v1", Model: "m", MinConfidence: 0.5},
		},
		{
			"the bounds of the threshold are allowed",
			Settings{Model: "m", APIKey: "key", MinConfidence: "0"},
			Config{Model: "m", APIKey: "key", MinConfidence: 0},
		},
		{
			"spaces around a value are not part of it",
			Settings{URL: " http://llama:8080/v1 ", Model: " m ", MinConfidence: " 1 "},
			Config{BaseURL: "http://llama:8080/v1", Model: "m", MinConfidence: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewConfig(&tt.settings)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_NewConfig_RefusesWhatCannotWork(t *testing.T) {
	for name, tt := range map[string]struct {
		settings Settings
		message  string
	}{
		"a URL without a model": {
			Settings{URL: "http://llama:8080/v1"},
			"PII_DETECT_LLM_URL is set without PII_DETECT_LLM_MODEL",
		},
		"a URL without a model, even with a key": {
			Settings{URL: "http://llama:8080/v1", APIKey: "key"},
			"PII_DETECT_LLM_URL is set without PII_DETECT_LLM_MODEL",
		},
		"a URL that is not one": {
			Settings{URL: "llama:8080", Model: "m"},
			"PII_DETECT_LLM_URL",
		},
		"a model with neither a URL nor a key, which would be asked at the OpenAI API without one": {
			Settings{Model: "m"},
			"PII_DETECT_LLM_MODEL is set without PII_DETECT_LLM_URL or PII_DETECT_LLM_API_KEY",
		},
		"the same with a threshold": {
			Settings{Model: "m", MinConfidence: "0.7"},
			"PII_DETECT_LLM_MODEL is set without PII_DETECT_LLM_URL or PII_DETECT_LLM_API_KEY",
		},
		"a URL with a query, which would not be sent": {
			Settings{URL: "http://llama:8080/v1?api-version=2", Model: "m"},
			"must not hold a query",
		},
		"a URL with a user and a password, which would not be sent": {
			Settings{URL: "http://user:secret@llama:8080/v1", Model: "m"},
			"must not hold a user",
		},
		"the same in the client library's variable": {
			Settings{OpenAIBaseURL: "https://user:secret@proxy.example/v1", OpenAIAPIKey: "sk-1"},
			"must not hold a user",
		},
		"a threshold that is not a number": {
			Settings{Model: "m", APIKey: "key", MinConfidence: "high"},
			"PII_DETECT_LLM_MIN_CONFIDENCE",
		},
		"a threshold under 0": {
			Settings{Model: "m", APIKey: "key", MinConfidence: "-0.1"},
			"PII_DETECT_LLM_MIN_CONFIDENCE",
		},
		"a threshold above 1": {
			Settings{MinConfidence: "1.1"},
			"PII_DETECT_LLM_MIN_CONFIDENCE",
		},
		"a threshold that is not a quantity": {
			Settings{MinConfidence: "NaN"},
			"PII_DETECT_LLM_MIN_CONFIDENCE",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewConfig(&tt.settings)
			require.ErrorContains(t, err, tt.message)
			require.NotContains(t, err.Error(), "secret", "the message does not repeat the URL")
		})
	}
}

func Test_Config_Enabled(t *testing.T) {
	require.False(t, (&Config{}).Enabled())
	require.False(t, (&Config{BaseURL: "http://llama:8080/v1", APIKey: "key"}).Enabled())
	require.True(t, (&Config{Model: "m"}).Enabled())
}

// The host is what the worker logs when it starts: where the requests go.
func Test_Config_Host(t *testing.T) {
	require.Equal(t, "api.openai.com", (&Config{Model: "m"}).Host())
	require.Equal(t, "llama:8080", (&Config{BaseURL: "http://llama:8080/v1", Model: "m"}).Host())
	require.Equal(t, "models.example", (&Config{BaseURL: "https://user:secret@models.example/v1?key=secret"}).Host())
}
