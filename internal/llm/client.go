package llm

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/spf13/viper"
)

// NewOpenAIClientFromEnv builds an OpenAI-compatible client from LLM_BASE_URL
// and LLM_API_KEY (falling back to OPENAI_API_KEY). Returns (nil, false) when
// nothing is configured, in which case LLM-backed features are disabled.
func NewOpenAIClientFromEnv() (*openai.Client, bool) {
	baseUrl := viper.GetString("LLM_BASE_URL")
	apiKey := viper.GetString("LLM_API_KEY")
	if apiKey == "" {
		apiKey = viper.GetString("OPENAI_API_KEY")
	}
	if baseUrl == "" && apiKey == "" {
		return nil, false
	}

	opts := []option.RequestOption{}
	if baseUrl != "" {
		opts = append(opts, option.WithBaseURL(baseUrl))
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	client := openai.NewClient(opts...)
	return &client, true
}
