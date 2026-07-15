package agent

import "strings"

// ModelOption is one selectable model in a provider's model list.
type ModelOption struct {
	ID    string // canonical model id sent to the API
	Label string // human-readable label (optional, falls back to ID)
}

// providerModels maps provider names to their available models. Used by the
// /model picker to show a selection menu. Providers not in this map fall back
// to a free-text input prompt.
var providerModels = map[string][]ModelOption{
	"anthropic": {
		{"claude-sonnet-4-20250514", "Claude Sonnet 4 (latest)"},
		{"claude-opus-4", "Claude Opus 4"},
		{"claude-3-5-sonnet-latest", "Claude 3.5 Sonnet"},
		{"claude-3-5-haiku-latest", "Claude 3.5 Haiku (fast)"},
	},
	"openai": {
		{"gpt-4o", "GPT-4o"},
		{"gpt-4o-mini", "GPT-4o mini (fast)"},
		{"gpt-4-turbo", "GPT-4 Turbo"},
		{"o1", "o1 (reasoning)"},
		{"o3-mini", "o3-mini (reasoning)"},
	},
	"volc": {
		{"glm-5.2", "GLM-5.2"},
		{"glm-4-plus", "GLM-4 Plus"},
		{"glm-4-long", "GLM-4 Long (128k)"},
		{"doubao-pro-32k", "Doubao Pro 32k"},
		{"doubao-pro-4k", "Doubao Pro 4k"},
	},
	"deepseek": {
		{"deepseek-chat", "DeepSeek Chat"},
		{"deepseek-reasoner", "DeepSeek Reasoner (R1)"},
	},
	"moonshot": {
		{"moonshot-v1-8k", "Moonshot v1 8k"},
		{"moonshot-v1-32k", "Moonshot v1 32k"},
		{"moonshot-v1-128k", "Moonshot v1 128k"},
	},
	"qwen": {
		{"qwen-turbo", "Qwen Turbo"},
		{"qwen-plus", "Qwen Plus"},
		{"qwen-max", "Qwen Max"},
	},
}

// ModelsForProvider returns the model list for a provider, or nil if unknown.
// Exported for use by the TUI model picker.
func ModelsForProvider(provider string) []ModelOption {
	return modelsForProvider(provider)
}

// modelsForProvider returns the model list for a provider, or nil if unknown.
func modelsForProvider(provider string) []ModelOption {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		p = "anthropic"
	}
	return providerModels[p]
}
