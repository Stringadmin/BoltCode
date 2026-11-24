package agent

import (
	"context"
	"encoding/json"
	"net/http"
)

// CheckConnection sends a small request through the same streaming protocol as
// a normal turn. It uses no project context, tools, or conversation history.
func CheckConnection(ctx context.Context, config Config) error {
	config.ReasoningEffort = ReasoningAuto
	message := []Message{{Role: "user", Content: "Reply with OK."}}
	const instructions = "This is a connection check. Reply briefly."
	var basic []byte
	switch {
	case isAnthropicProvider(config):
		basic = buildAnthropicPayloadWithTools(config, instructions, message, nil)
	case isResponsesProvider(config):
		basic = buildResponsesPayloadWithTools(config, instructions, message, nil)
	default:
		basic = buildOpenAIPayloadWithTools(config, instructions, message, nil)
	}
	var payload map[string]any
	if err := json.Unmarshal(basic, &payload); err != nil {
		return err
	}
	delete(payload, "thinking")
	delete(payload, "output_config")
	delete(payload, "tools")
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if _, ok := payload[key]; ok {
			payload[key] = 256
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	probe := &Agent{Config: config, structuredTurn: true}
	_, _, err, _, _ = probe.doOnce(ctx, &http.Client{}, providerURL(config), body)
	return err
}
