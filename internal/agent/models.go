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

// AvailableModels merges the active model and models saved in compatible
// profiles with BoltCode's built-in catalog. The current model is placed first
// so a custom id never disappears from /model just because it isn't compiled
// into providerModels.
func AvailableModels(config Config) []ModelOption {
	var models []ModelOption
	seen := make(map[string]bool)
	builtInLabels := make(map[string]string)
	for _, option := range modelsForProvider(config.Provider) {
		builtInLabels[strings.ToLower(option.ID)] = option.Label
	}
	add := func(id, label string) {
		id = strings.TrimSpace(id)
		key := strings.ToLower(id)
		if id == "" || seen[key] {
			return
		}
		seen[key] = true
		models = append(models, ModelOption{ID: id, Label: label})
	}

	currentLabel := builtInLabels[strings.ToLower(config.Model)]
	if currentLabel == "" && strings.TrimSpace(config.Model) != "" {
		currentLabel = "Configured model · current"
	}
	add(config.Model, currentLabel)

	if profiles, err := LoadProfiles(); err == nil && profiles != nil {
		for _, profile := range profiles.Profiles {
			if !strings.EqualFold(profile.Provider, config.Provider) || !sameModelEndpoint(profile.BaseURL, config.BaseURL) {
				continue
			}
			label := builtInLabels[strings.ToLower(profile.Model)]
			if label == "" {
				label = "Configured · " + profile.Name
			}
			add(profile.Model, label)
		}
	}
	for _, option := range modelsForProvider(config.Provider) {
		add(option.ID, option.Label)
	}
	return models
}

func sameModelEndpoint(left, right string) bool {
	normalize := func(value string) string {
		return strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), "/")
	}
	return normalize(left) == normalize(right)
}

// modelsForProvider returns the model list for a provider, or nil if unknown.
func modelsForProvider(provider string) []ModelOption {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		p = "anthropic"
	}
	return providerModels[p]
}

// ProviderInfo describes a known AI provider for the config wizard: its
// canonical id (stored in Config.Provider), a human-readable display name, the
// default API base URL, and whether it needs fully custom endpoint settings.
type ProviderInfo struct {
	ID      string // stored in Config.Provider (e.g. "anthropic", "volc")
	Name    string // display name (e.g. "Anthropic (Claude)")
	BaseURL string // default API base URL ("" = provider default)
	Custom  bool   // true = user must fill base URL + model manually
}

// KnownProviders is the ordered list shown in the config wizard. The final
// entry is a catch-all "custom" option for any OpenAI-compatible endpoint.
var KnownProviders = []ProviderInfo{
	{ID: "anthropic", Name: "Anthropic (Claude)", BaseURL: ""},
	{ID: "openai", Name: "OpenAI (GPT)", BaseURL: "https://api.openai.com/v1"},
	{ID: "volc", Name: "火山方舟 (豆包/GLM)", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1"},
	{ID: "moonshot", Name: "Moonshot (Kimi)", BaseURL: "https://api.moonshot.cn/v1"},
	{ID: "qwen", Name: "通义千问 (Qwen)", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	{ID: "custom", Name: "自定义 (OpenAI 兼容)", BaseURL: "", Custom: true},
}

// ProviderByID returns the ProviderInfo for an id, or false if unknown.
func ProviderByID(id string) (ProviderInfo, bool) {
	for _, p := range KnownProviders {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return ProviderInfo{}, false
}
