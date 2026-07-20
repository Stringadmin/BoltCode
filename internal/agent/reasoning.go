package agent

import (
	"fmt"
	"strings"
)

// ReasoningEffort controls how much inference-time reasoning a model may use.
// Auto preserves provider defaults and backwards compatibility.
type ReasoningEffort string

const (
	ReasoningAuto   ReasoningEffort = "auto"
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
)

func ParseReasoningEffort(value string) (ReasoningEffort, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "-" || value == "default" {
		return ReasoningAuto, nil
	}
	switch ReasoningEffort(value) {
	case ReasoningAuto, ReasoningLow, ReasoningMedium, ReasoningHigh:
		return ReasoningEffort(value), nil
	default:
		return "", fmt.Errorf("思考强度无效，请使用 auto、low、medium 或 high")
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
	default:
		// Auto retains BoltCode's previous extended-thinking default.
		return 16_000
	}
}
