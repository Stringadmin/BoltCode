package tui

import (
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// renderResumeHistory converts persisted protocol messages into the same
// visible role hierarchy used by live chat. Internal project context,
// reasoning metadata, and tool_result payloads remain hidden.
func renderResumeHistory(messages []agent.Message) []string {
	var lines []string
	for _, message := range messages {
		text := visibleHistoryText(message)
		if text == "" {
			continue
		}
		lines = append(lines, "")
		switch message.Role {
		case "user":
			lines = append(lines, chatUserStyle.Render("  ◆ you"))
			for _, line := range strings.Split(text, "\n") {
				lines = append(lines, "    "+line)
			}
		case "assistant":
			lines = append(lines, "  "+chatAssistantStyle.Render(" ✦ assistant "), "")
			lines = append(lines, strings.Split(agent.RenderMarkdownForTUI(text), "\n")...)
		}
	}
	return lines
}

func visibleHistoryText(message agent.Message) string {
	text := ""
	if value, ok := message.Content.(string); ok {
		text = value
	} else {
		for _, block := range historyContentBlocks(message.Content) {
			if kind, _ := block["type"].(string); kind == "text" {
				if value, ok := block["text"].(string); ok {
					text += value
				}
			}
		}
	}
	text = strings.TrimSpace(text)
	if message.Role == "user" {
		if marker := strings.LastIndex(text, "## User request\n"); marker >= 0 {
			text = strings.TrimSpace(text[marker+len("## User request\n"):])
		}
		if strings.HasPrefix(text, "Hi. Here is the project context:") || strings.HasPrefix(text, "[System]") {
			return ""
		}
	}
	return text
}

func historyContentBlocks(content interface{}) []map[string]interface{} {
	switch blocks := content.(type) {
	case []map[string]interface{}:
		return blocks
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(blocks))
		for _, block := range blocks {
			if value, ok := block.(map[string]interface{}); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}
