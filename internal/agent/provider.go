package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// isAnthropicProvider returns true if the config points to an Anthropic-native
// API. Provider "anthropic" (or empty, the default) uses the Anthropic
// /v1/messages format. All other providers use the OpenAI /v1/chat/completions
// format — the de facto standard for LLM API compatibility (volcengine ark,
// deepseek, moonshot, qwen, etc.).
func isAnthropicProvider(cfg Config) bool {
	if cfg.Provider == "" {
		return true
	}
	return strings.EqualFold(cfg.Provider, "anthropic")
}

// providerURL returns the API endpoint for the configured provider.
func providerURL(cfg Config) string {
	if isAnthropicProvider(cfg) {
		return buildURL(cfg.BaseURL)
	}
	return buildOpenAIURL(cfg.BaseURL)
}

// buildOpenAIURL constructs the chat/completions endpoint from a base URL.
func buildOpenAIURL(baseURL string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/chat/completions"
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

// buildPayload creates the request body for the configured provider.
func buildPayload(cfg Config, system string, messages []Message) []byte {
	if isAnthropicProvider(cfg) {
		return buildAnthropicPayload(cfg, system, messages)
	}
	return buildOpenAIPayload(cfg, system, messages)
}

// buildAnthropicPayload builds the Anthropic /v1/messages request body with
// prompt caching and extended thinking.
func buildAnthropicPayload(cfg Config, system string, messages []Message) []byte {
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	body["system"] = []map[string]interface{}{
		{"type": "text", "text": system, "cache_control": map[string]string{"type": "ephemeral"}},
	}
	body["tools"] = append(Tools, map[string]interface{}{
		"cache_control": map[string]string{"type": "ephemeral"},
	})
	if supportsThinking(model) {
		budgetTokens := 16000
		body["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": budgetTokens,
		}
		body["max_tokens"] = budgetTokens + 8192
	} else {
		body["max_tokens"] = 8192
	}
	payload, _ := json.Marshal(body)
	return payload
}

// buildOpenAIPayload builds the OpenAI /v1/chat/completions request body.
func buildOpenAIPayload(cfg Config, system string, messages []Message) []byte {
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	body := map[string]interface{}{
		"model":    model,
		"messages": convertMessagesToOpenAI(system, messages),
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
		"tools":      convertToolsToOpenAI(Tools),
		"max_tokens": 8192,
	}
	payload, _ := json.Marshal(body)
	return payload
}

// convertMessagesToOpenAI converts internal Message[] (Anthropic content-block
// format) to OpenAI chat/completions message format. The key differences:
//   - System prompt becomes the first message with role "system"
//   - Assistant tool_use blocks become top-level "tool_calls" array
//   - User tool_result blocks become separate "tool" role messages
func convertMessagesToOpenAI(system string, messages []Message) []map[string]interface{} {
	var out []map[string]interface{}
	out = append(out, map[string]interface{}{
		"role":    "system",
		"content": system,
	})
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			switch content := msg.Content.(type) {
			case string:
				out = append(out, map[string]interface{}{
					"role":    "user",
					"content": content,
				})
			case []interface{}:
				hasToolResult := false
				for _, block := range content {
					if m, ok := block.(map[string]interface{}); ok {
						if t, _ := m["type"].(string); t == "tool_result" {
							hasToolResult = true
							toolUseID, _ := m["tool_use_id"].(string)
							contentStr, _ := m["content"].(string)
							out = append(out, map[string]interface{}{
								"role":         "tool",
								"tool_call_id": toolUseID,
								"content":      contentStr,
							})
						}
					}
				}
				if !hasToolResult {
					var text string
					for _, block := range content {
						if m, ok := block.(map[string]interface{}); ok {
							if t, _ := m["type"].(string); t == "text" {
								if s, ok := m["text"].(string); ok {
									text += s
								}
							}
						}
					}
					if text != "" {
						out = append(out, map[string]interface{}{
							"role":    "user",
							"content": text,
						})
					}
				}
			}
		case "assistant":
			var textContent string
			var toolCalls []map[string]interface{}
			switch content := msg.Content.(type) {
			case string:
				textContent = content
			case []interface{}:
				for _, block := range content {
					if m, ok := block.(map[string]interface{}); ok {
						switch t, _ := m["type"].(string); t {
						case "text":
							if s, ok := m["text"].(string); ok {
								textContent += s
							}
						case "tool_use":
							id, _ := m["id"].(string)
							name, _ := m["name"].(string)
							input, _ := json.Marshal(m["input"])
							toolCalls = append(toolCalls, map[string]interface{}{
								"id":   id,
								"type": "function",
								"function": map[string]interface{}{
									"name":      name,
									"arguments": string(input),
								},
							})
						}
					}
				}
			}
			m := map[string]interface{}{
				"role": "assistant",
			}
			if textContent != "" {
				m["content"] = textContent
			} else {
				m["content"] = nil
			}
			if len(toolCalls) > 0 {
				m["tool_calls"] = toolCalls
			}
			out = append(out, m)
		}
	}
	return out
}

// convertToolsToOpenAI converts Anthropic tool definitions (name, description,
// input_schema) to OpenAI function-calling format (type: function, function:
// {name, description, parameters}).
func convertToolsToOpenAI(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		out = append(out, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  t["input_schema"],
			},
		})
	}
	return out
}

// parseOpenAIStream reads SSE events from the OpenAI streaming API, prints text
// in real-time, and returns assembled content blocks in Anthropic format (so
// the rest of the agent loop — processResponse, ExecTool, etc. — works unchanged).
func (a *Agent) parseOpenAIStream(body interface{ Read([]byte) (int, error) }) ([]byte, Usage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1<<20), 4<<20)

	var textBuilder strings.Builder
	type toolCallAccum struct {
		ID   string
		Name string
		Args string
	}
	toolCalls := map[int]*toolCallAccum{}
	var usage Usage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			break
		}

		var event struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				textBuilder.WriteString(choice.Delta.Content)
			}
			for _, tc := range choice.Delta.ToolCalls {
				if toolCalls[tc.Index] == nil {
					toolCalls[tc.Index] = &toolCallAccum{}
				}
				if tc.ID != "" {
					toolCalls[tc.Index].ID = tc.ID
				}
				if tc.Function.Name != "" {
					toolCalls[tc.Index].Name = tc.Function.Name
				}
				toolCalls[tc.Index].Args += tc.Function.Arguments
			}
		}

		if event.Usage != nil {
			usage.InputTokens = event.Usage.PromptTokens
			usage.OutputTokens = event.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, usage, fmt.Errorf("stream read error: %w", err)
	}

	// Assemble content blocks in Anthropic format so processResponse works unchanged.
	var content []map[string]interface{}
	if textBuilder.Len() > 0 && strings.TrimSpace(textBuilder.String()) != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": textBuilder.String(),
		})
		printAssistantMessage(textBuilder.String())
	}

	indices := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		tc := toolCalls[idx]
		var input interface{}
		if tc.Args != "" {
			json.Unmarshal([]byte(tc.Args), &input)
		}
		content = append(content, map[string]interface{}{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": input,
		})
	}

	result, _ := json.Marshal(content)
	return result, usage, nil
}

// parseOpenAINonStreaming parses a non-streaming OpenAI response (used by
// context compaction). Returns content blocks in Anthropic format.
func parseOpenAINonStreaming(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse OpenAI response: %w", err)
	}
	var content []map[string]interface{}
	if len(result.Choices) > 0 && result.Choices[0].Message.Content != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": result.Choices[0].Message.Content,
		})
	}
	return json.Marshal(content)
}
