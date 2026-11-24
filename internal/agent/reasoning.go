package agent

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

type reasoningReplay struct {
	Profile string `json:"profile"`
	Text    string `json:"text"`
}

// ReasoningEffort controls how much inference-time reasoning a model may use.
// Auto preserves provider defaults and backwards compatibility.
type ReasoningEffort string

const (
	ReasoningAuto   ReasoningEffort = "auto"
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
	ReasoningXHigh  ReasoningEffort = "xhigh"
)

func ParseReasoningEffort(value string) (ReasoningEffort, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "-" || value == "default" {
		return ReasoningAuto, nil
	}
	switch ReasoningEffort(value) {
	case ReasoningAuto, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh:
		return ReasoningEffort(value), nil
	default:
		return "", fmt.Errorf("思考强度无效，请使用 auto、low、medium、high 或 xhigh")
	}
}

func NormalizeReasoningEffort(value ReasoningEffort) ReasoningEffort {
	parsed, err := ParseReasoningEffort(string(value))
	if err != nil {
		return ReasoningAuto
	}
	return parsed
}

func FormatReasoningEffort(value ReasoningEffort) string {
	return string(NormalizeReasoningEffort(value))
}

func anthropicThinkingBudget(value ReasoningEffort) int {
	switch NormalizeReasoningEffort(value) {
	case ReasoningLow:
		return 2_048
	case ReasoningMedium:
		return 8_192
	case ReasoningHigh:
		return 32_000
	case ReasoningXHigh:
		return 48_000
	default:
		// Auto retains BoltCode's previous extended-thinking default.
		return 16_000
	}
}

var anthropicAdaptive = regexp.MustCompile(`(?i)^claude-(?:(?:opus|sonnet)-4-[6-9]|(?:opus|sonnet|haiku|fable|mythos)-5)(?:-|\.|$)`)
var anthropicEarlyAdaptive = regexp.MustCompile(`(?i)^claude-(?:opus|sonnet)-4-6(?:-|$)`)
var anthropicShortOutput = regexp.MustCompile(`(?i)^claude-opus-4(?:-1)?(?:-\d{8})?$`)
var anthropicManual = regexp.MustCompile(`(?i)^claude-(?:(?:opus|sonnet)-4|haiku-4-5|3-7-sonnet)(?:-|$)`)

func SupportsReasoningSelection(cfg Config) bool {
	return !isAnthropicProvider(cfg) || anthropicAdaptive.MatchString(cfg.Model) || anthropicManual.MatchString(cfg.Model)
}

func deepSeekReasoningModel(cfg Config) bool {
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	return (strings.EqualFold(cfg.Provider, "deepseek") ||
		strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://api.deepseek.com/")) &&
		(model == "deepseek-flash" || model == "deepseek-v4-flash" || model == "deepseek-v4-pro")
}

func requestReasoningEffort(cfg Config) ReasoningEffort {
	effort := NormalizeReasoningEffort(cfg.ReasoningEffort)
	if effort == ReasoningXHigh && deepSeekReasoningModel(cfg) {
		return "max"
	}
	return effort
}

func deepSeekReplayProfile(cfg Config) string {
	if !deepSeekReasoningModel(cfg) {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.Provider+"\n"+cfg.BaseURL+"\n"+cfg.Model)))
}
