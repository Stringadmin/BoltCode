package agent

import (
	"encoding/json"
	"testing"
)

func TestParseReasoningEffort(t *testing.T) {
	for input, want := range map[string]ReasoningEffort{
		"":       ReasoningAuto,
		"-":      ReasoningAuto,
		" AUTO ": ReasoningAuto,
		"low":    ReasoningLow,
		"MEDIUM": ReasoningMedium,
		"high":   ReasoningHigh,
	} {
		got, err := ParseReasoningEffort(input)
		if err != nil || got != want {
			t.Errorf("ParseReasoningEffort(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParseReasoningEffort("extreme"); err == nil {
		t.Fatal("invalid reasoning effort should fail")
	}
}

func TestAnthropicReasoningEffortControlsThinkingBudget(t *testing.T) {
	tests := []struct {
		effort ReasoningEffort
		budget int
	}{
		{ReasoningAuto, 16_000},
		{ReasoningLow, 2_048},
		{ReasoningMedium, 8_192},
		{ReasoningHigh, 32_000},
	}
	for _, test := range tests {
		t.Run(string(test.effort), func(t *testing.T) {
			payload := buildAnthropicPayload(Config{
				Provider:        "anthropic",
				Model:           "claude-sonnet-4-20250514",
				ReasoningEffort: test.effort,
			}, "system", []Message{{Role: "user", Content: "hello"}})
			var body map[string]interface{}
			if err := json.Unmarshal(payload, &body); err != nil {
				t.Fatal(err)
			}
			thinking, ok := body["thinking"].(map[string]interface{})
			if !ok || int(thinking["budget_tokens"].(float64)) != test.budget {
				t.Fatalf("thinking payload = %#v, want budget %d", thinking, test.budget)
			}
			if got := int(body["max_tokens"].(float64)); got != test.budget+8192 {
				t.Fatalf("max_tokens = %d, want %d", got, test.budget+8192)
			}
		})
	}
}

func TestOpenAICompatibleReasoningEffortIsOptional(t *testing.T) {
	for _, test := range []struct {
		effort ReasoningEffort
		want   string
	}{
		{ReasoningAuto, ""},
		{ReasoningLow, "low"},
		{ReasoningMedium, "medium"},
		{ReasoningHigh, "high"},
	} {
		payload := buildOpenAIPayload(Config{Provider: "deepseek", Model: "deepseek-reasoner", ReasoningEffort: test.effort}, "system", nil)
		var body map[string]interface{}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		got, _ := body["reasoning_effort"].(string)
		if got != test.want {
			t.Errorf("effort %q payload = %q, want %q", test.effort, got, test.want)
		}
	}
}

// Models outside the thinking whitelist must never receive a "thinking"
// payload — the Anthropic API rejects it with a 400 — even when the user set
// an explicit reasoning effort. The effort is simply inert for such models.
func TestAnthropicThinkingNotForcedOnUnsupportedModels(t *testing.T) {
	payload := buildAnthropicPayload(Config{
		Provider:        "anthropic",
		Model:           "claude-3-haiku-20240307",
		ReasoningEffort: ReasoningHigh,
	}, "system", []Message{{Role: "user", Content: "hello"}})
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking payload must not be sent for unsupported model: %s", payload)
	}
	if got := int(body["max_tokens"].(float64)); got != 8192 {
		t.Fatalf("max_tokens = %d, want 8192", got)
	}
}
