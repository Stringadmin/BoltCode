package agent

import "fmt"

// validateToolMessageSequence verifies the cross-provider invariant shared by
// Anthropic and OpenAI-compatible APIs: every assistant tool call must be
// followed immediately by exactly one matching tool result before any further
// user or assistant message. OpenAI-compatible proxies occasionally omit IDs,
// so missing IDs may be paired positionally there; native Anthropic requires
// IDs because its tool_result schema references them explicitly.
func validateToolMessageSequence(messages []Message, requireIDs bool) error {
	for i, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		var callIDs []string
		for _, block := range messageContentBlocks(msg.Content) {
			if kind, _ := block["type"].(string); kind == "tool_use" {
				id, _ := block["id"].(string)
				if id == "" {
					if requireIDs {
						return fmt.Errorf("assistant tool call %d at message %d has no id", len(callIDs)+1, i)
					}
					id = positionalToolID(len(callIDs))
				}
				callIDs = append(callIDs, id)
			}
		}
		if len(callIDs) == 0 {
			continue
		}
		if i+1 >= len(messages) || messages[i+1].Role != "user" {
			return fmt.Errorf("assistant message %d has %d tool calls but no following tool-result message", i, len(callIDs))
		}

		resultIDs := make([]string, 0, len(callIDs))
		for _, block := range messageContentBlocks(messages[i+1].Content) {
			if kind, _ := block["type"].(string); kind == "tool_result" {
				id, _ := block["tool_use_id"].(string)
				if id == "" {
					if requireIDs {
						return fmt.Errorf("tool result %d after message %d has no tool_use_id", len(resultIDs)+1, i)
					}
					id = positionalToolID(len(resultIDs))
				}
				resultIDs = append(resultIDs, id)
			}
		}
		if len(resultIDs) != len(callIDs) {
			return fmt.Errorf("assistant message %d has %d tool calls but %d tool results", i, len(callIDs), len(resultIDs))
		}
		seen := make(map[string]bool, len(callIDs))
		for _, id := range callIDs {
			if seen[id] {
				return fmt.Errorf("assistant message %d repeats tool call id %q", i, id)
			}
			seen[id] = true
		}
		for _, id := range resultIDs {
			if !seen[id] {
				return fmt.Errorf("tool result id %q does not match a call in assistant message %d", id, i)
			}
			delete(seen, id)
		}
		if len(seen) != 0 {
			return fmt.Errorf("assistant message %d is missing one or more tool results", i)
		}
	}
	return nil
}

func positionalToolID(index int) string {
	return fmt.Sprintf("#tool-%d", index)
}
