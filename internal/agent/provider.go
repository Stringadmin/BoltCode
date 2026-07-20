package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
)

var fallbackToolCallSeq uint64

// isAnthropicProvider returns true if the config points to an Anthropic-native
// API. Provider "anthropic" (or empty, the default) uses the Anthropic
// /v1/messages format. Other providers use either OpenAI Responses (Codex
// endpoints) or Chat Completions (the common compatibility format used by
// volcengine ark, deepseek, moonshot, qwen, etc.).
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
	if isResponsesProvider(cfg) {
		return buildResponsesURL(cfg.BaseURL)
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
	return buildPayloadForMode(cfg, system, messages, ModeBuild)
}

func buildPayloadForMode(cfg Config, system string, messages []Message, mode Mode) []byte {
	tools := ToolsForMode(mode)
	if isAnthropicProvider(cfg) {
		return buildAnthropicPayloadWithTools(cfg, system, messages, tools)
	}
	if isResponsesProvider(cfg) {
		return buildResponsesPayloadWithTools(cfg, system, messages, tools)
	}
	return buildOpenAIPayloadWithTools(cfg, system, messages, tools)
}

// buildAnthropicPayload builds the Anthropic /v1/messages request body with
// prompt caching and extended thinking.
func buildAnthropicPayload(cfg Config, system string, messages []Message) []byte {
	return buildAnthropicPayloadWithTools(cfg, system, messages, ToolsForMode(ModeBuild))
}

func buildAnthropicPayloadWithTools(cfg Config, system string, messages []Message, toolDefinitions []map[string]interface{}) []byte {
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
	// Prompt caching belongs on the final real tool definition. Appending a
	// standalone {cache_control: ...} object creates an invalid nameless tool.
	tools := make([]map[string]interface{}, len(toolDefinitions))
	for i, tool := range toolDefinitions {
		clone := make(map[string]interface{}, len(tool)+1)
		for key, value := range tool {
			clone[key] = value
		}
		tools[i] = clone
	}
	if len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = map[string]string{"type": "ephemeral"}
	}
	body["tools"] = tools
	// Only send the thinking payload to models known to support it. An
	// explicit non-auto effort must NOT force the field onto other models —
	// the Anthropic API rejects "thinking" for them with a 400.
	if supportsThinking(model) {
		budgetTokens := anthropicThinkingBudget(NormalizeReasoningEffort(cfg.ReasoningEffort))
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
	return buildOpenAIPayloadWithTools(cfg, system, messages, ToolsForMode(ModeBuild))
}

func buildOpenAIPayloadWithTools(cfg Config, system string, messages []Message, toolDefinitions []map[string]interface{}) []byte {
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
		"tools":      convertToolsToOpenAI(toolDefinitions),
		"max_tokens": 8192,
	}
	if effort := NormalizeReasoningEffort(cfg.ReasoningEffort); effort != ReasoningAuto {
		body["reasoning_effort"] = string(effort)
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
	var pendingToolIDs []string
	out = append(out, map[string]interface{}{
		"role":    "system",
		"content": system,
	})
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if content, ok := msg.Content.(string); ok {
				out = append(out, map[string]interface{}{
					"role":    "user",
					"content": content,
				})
			} else {
				blocks := messageContentBlocks(msg.Content)
				hasToolResult := false
				resultIndex := 0
				for _, block := range blocks {
					if t, _ := block["type"].(string); t == "tool_result" {
						hasToolResult = true
						toolUseID, _ := block["tool_use_id"].(string)
						if toolUseID == "" && resultIndex < len(pendingToolIDs) {
							toolUseID = pendingToolIDs[resultIndex]
						}
						contentStr, _ := block["content"].(string)
						if isError, _ := block["is_error"].(bool); isError {
							contentStr = "Tool error: " + contentStr
						}
						out = append(out, map[string]interface{}{
							"role":         "tool",
							"tool_call_id": toolUseID,
							"content":      contentStr,
						})
						resultIndex++
					}
				}
				if hasToolResult {
					pendingToolIDs = nil
				}
				if !hasToolResult {
					var text string
					for _, block := range blocks {
						if t, _ := block["type"].(string); t == "text" {
							if s, ok := block["text"].(string); ok {
								text += s
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
			if content, ok := msg.Content.(string); ok {
				textContent = content
			} else {
				for _, block := range messageContentBlocks(msg.Content) {
					switch t, _ := block["type"].(string); t {
					case "text":
						if s, ok := block["text"].(string); ok {
							textContent += s
						}
					case "tool_use":
						id, _ := block["id"].(string)
						if id == "" {
							id = nextFallbackToolCallID(len(toolCalls))
						}
						name, _ := block["name"].(string)
						input, _ := json.Marshal(block["input"])
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
				pendingToolIDs = make([]string, 0, len(toolCalls))
				for _, call := range toolCalls {
					pendingToolIDs = append(pendingToolIDs, call["id"].(string))
				}
			}
			out = append(out, m)
		}
	}
	return out
}

func nextFallbackToolCallID(index int) string {
	seq := atomic.AddUint64(&fallbackToolCallSeq, 1)
	return fmt.Sprintf("call_boltcode_%d_%d", seq, index)
}

// messageContentBlocks accepts both shapes produced inside the process and
// after JSON round-tripping. Tool results are built as []map in loop.go, while
// loaded sessions decode the same JSON as []interface{}.
func messageContentBlocks(content interface{}) []map[string]interface{} {
	switch blocks := content.(type) {
	case []map[string]interface{}:
		return blocks
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(blocks))
		for _, block := range blocks {
			if m, ok := block.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
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
		data, ok := parseSSEDataLine(line)
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var event struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
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
			return nil, usage, fmt.Errorf("invalid OpenAI-compatible stream event: %w", err)
		}
		if event.Error != nil {
			return nil, usage, fmt.Errorf("OpenAI-compatible stream error: %s", event.Error.Message)
		}

		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				textBuilder.WriteString(choice.Delta.Content)
			}
			for _, tc := range choice.Delta.ToolCalls {
				if tc.Index < 0 {
					return nil, usage, fmt.Errorf("OpenAI-compatible stream used negative tool index %d", tc.Index)
				}
				if toolCalls[tc.Index] == nil {
					toolCalls[tc.Index] = &toolCallAccum{}
				}
				if tc.ID != "" {
					// Some OpenAI-compatible gateways replace the call ID on later
					// chunks while keeping the same index, function and argument
					// stream. The gateway validates tool output against its latest
					// ID, so follow the most recent non-empty value.
					toolCalls[tc.Index].ID = tc.ID
				}
				if tc.Function.Name != "" {
					if toolCalls[tc.Index].Name != "" && toolCalls[tc.Index].Name != tc.Function.Name {
						return nil, usage, fmt.Errorf("OpenAI-compatible stream changed tool name at index %d", tc.Index)
					}
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
		if tc.ID == "" {
			tc.ID = nextFallbackToolCallID(idx)
		}
		if tc.Name == "" {
			return nil, usage, fmt.Errorf("tool call %d is missing a function name", idx)
		}
		var input interface{} = map[string]interface{}{}
		if tc.Args != "" {
			if err := json.Unmarshal([]byte(tc.Args), &input); err != nil {
				return nil, usage, fmt.Errorf("tool call %s has invalid arguments: %w", tc.Name, err)
			}
		}
		content = append(content, map[string]interface{}{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": input,
		})
	}
	if len(content) == 0 {
		return nil, usage, fmt.Errorf("OpenAI-compatible stream ended without text or tool calls")
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
