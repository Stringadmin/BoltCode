package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// isResponsesProvider detects endpoints that expose OpenAI's Responses API.
// An explicit endpoint wins; Codex gateways conventionally place their v1
// root below /codex/ and require /responses rather than chat/completions.
func isResponsesProvider(cfg Config) bool {
	baseURL := strings.ToLower(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"))
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	return provider == "responses" || provider == "openai-responses" ||
		strings.HasSuffix(baseURL, "/responses") || strings.Contains(baseURL, "/codex/")
}

func buildResponsesURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "https://api.openai.com/v1/responses"
	}
	if strings.HasSuffix(strings.ToLower(baseURL), "/responses") {
		return baseURL
	}
	if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL + "/responses"
	}
	return baseURL + "/v1/responses"
}

func buildResponsesPayloadWithTools(cfg Config, system string, messages []Message, toolDefinitions []map[string]interface{}) []byte {
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	body := map[string]interface{}{
		"model":             model,
		"instructions":      system,
		"input":             convertMessagesToResponses(messages),
		"stream":            true,
		"store":             false,
		"max_output_tokens": 8192,
	}
	if tools := convertToolsToResponses(toolDefinitions); len(tools) > 0 {
		body["tools"] = tools
	}
	if effort := NormalizeReasoningEffort(cfg.ReasoningEffort); effort != ReasoningAuto {
		body["reasoning"] = map[string]interface{}{"effort": string(effort)}
	}
	payload, _ := json.Marshal(body)
	return payload
}

func convertMessagesToResponses(messages []Message) []map[string]interface{} {
	var input []map[string]interface{}
	for _, message := range messages {
		switch message.Role {
		case "user":
			if text, ok := message.Content.(string); ok {
				if text != "" {
					input = append(input, responsesTextMessage("user", "input_text", text))
				}
				continue
			}
			var text strings.Builder
			flushText := func() {
				if text.Len() > 0 {
					input = append(input, responsesTextMessage("user", "input_text", text.String()))
					text.Reset()
				}
			}
			for _, block := range messageContentBlocks(message.Content) {
				switch kind, _ := block["type"].(string); kind {
				case "tool_result":
					flushText()
					callID, _ := block["tool_use_id"].(string)
					output, _ := block["content"].(string)
					if isError, _ := block["is_error"].(bool); isError {
						output = "Tool error: " + output
					}
					input = append(input, map[string]interface{}{
						"type":    "function_call_output",
						"call_id": callID,
						"output":  output,
					})
				case "text":
					if value, ok := block["text"].(string); ok {
						text.WriteString(value)
					}
				}
			}
			flushText()

		case "assistant":
			if text, ok := message.Content.(string); ok {
				if text != "" {
					input = append(input, responsesTextMessage("assistant", "output_text", text))
				}
				continue
			}
			var text strings.Builder
			flushText := func() {
				if text.Len() > 0 {
					input = append(input, responsesTextMessage("assistant", "output_text", text.String()))
					text.Reset()
				}
			}
			for _, block := range messageContentBlocks(message.Content) {
				switch kind, _ := block["type"].(string); kind {
				case "tool_use":
					flushText()
					callID, _ := block["id"].(string)
					if callID == "" {
						callID = nextFallbackToolCallID(len(input))
					}
					name, _ := block["name"].(string)
					arguments, _ := json.Marshal(block["input"])
					input = append(input, map[string]interface{}{
						"type":      "function_call",
						"call_id":   callID,
						"name":      name,
						"arguments": string(arguments),
					})
				case "text":
					if value, ok := block["text"].(string); ok {
						text.WriteString(value)
					}
				}
			}
			flushText()
		}
	}
	return input
}

func responsesTextMessage(role, contentType, text string) map[string]interface{} {
	return map[string]interface{}{
		"role": role,
		"content": []map[string]interface{}{
			{"type": contentType, "text": text},
		},
	}
}

func convertToolsToResponses(toolDefinitions []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(toolDefinitions))
	for _, tool := range toolDefinitions {
		name, _ := tool["name"].(string)
		if name == "" {
			continue
		}
		description, _ := tool["description"].(string)
		out = append(out, map[string]interface{}{
			"type":        "function",
			"name":        name,
			"description": description,
			"parameters":  tool["input_schema"],
		})
	}
	return out
}

type responsesOutputItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responsesStreamEvent struct {
	Type        string               `json:"type"`
	Delta       string               `json:"delta"`
	Text        string               `json:"text"`
	Arguments   string               `json:"arguments"`
	OutputIndex int                  `json:"output_index"`
	Item        *responsesOutputItem `json:"item"`
	Message     string               `json:"message"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
	Response *struct {
		Status string                `json:"status"`
		Output []responsesOutputItem `json:"output"`
		Usage  *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

type responsesToolCall struct {
	ID   string
	Name string
	Args string
}

func (a *Agent) parseResponsesStream(body io.Reader) ([]byte, Usage, error) {
	return parseResponsesStream(body, true)
}

func parseResponsesStream(body io.Reader, render bool) ([]byte, Usage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1<<20), 4<<20)
	var textBuilder strings.Builder
	toolCalls := make(map[int]*responsesToolCall)
	var usage Usage

	setItem := func(index int, item responsesOutputItem) {
		if item.Type != "function_call" {
			return
		}
		call := toolCalls[index]
		if call == nil {
			call = &responsesToolCall{}
			toolCalls[index] = call
		}
		if item.CallID != "" {
			call.ID = item.CallID
		}
		if item.Name != "" {
			call.Name = item.Name
		}
		if item.Arguments != "" {
			call.Args = item.Arguments
		}
	}
	appendItemText := func(item responsesOutputItem) {
		if item.Type != "message" || textBuilder.Len() > 0 {
			return
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				textBuilder.WriteString(content.Text)
			}
		}
	}

	for scanner.Scan() {
		data, ok := parseSSEDataLine(scanner.Text())
		if !ok || data == "" || data == "[DONE]" {
			continue
		}
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, usage, fmt.Errorf("invalid Responses stream event: %w", err)
		}
		switch event.Type {
		case "response.output_text.delta":
			textBuilder.WriteString(event.Delta)
		case "response.output_text.done":
			if textBuilder.Len() == 0 {
				textBuilder.WriteString(event.Text)
			}
		case "response.output_item.added", "response.output_item.done":
			if event.Item != nil {
				setItem(event.OutputIndex, *event.Item)
				appendItemText(*event.Item)
			}
		case "response.function_call_arguments.delta":
			call := toolCalls[event.OutputIndex]
			if call == nil {
				call = &responsesToolCall{}
				toolCalls[event.OutputIndex] = call
			}
			call.Args += event.Delta
		case "response.function_call_arguments.done":
			call := toolCalls[event.OutputIndex]
			if call == nil {
				call = &responsesToolCall{}
				toolCalls[event.OutputIndex] = call
			}
			if event.Arguments != "" {
				call.Args = event.Arguments
			}
		case "response.completed":
			if event.Response != nil {
				if event.Response.Usage != nil {
					usage.InputTokens = event.Response.Usage.InputTokens
					usage.OutputTokens = event.Response.Usage.OutputTokens
				}
				for index, item := range event.Response.Output {
					setItem(index, item)
					appendItemText(item)
				}
			}
		case "response.failed", "error":
			message := event.Message
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
			}
			if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
				message = event.Response.Error.Message
			}
			if message == "" {
				message = "Responses API request failed"
			}
			return nil, usage, fmt.Errorf("Responses stream error: %s", message)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, usage, fmt.Errorf("Responses stream read error: %w", err)
	}

	var content []map[string]interface{}
	if text := textBuilder.String(); strings.TrimSpace(text) != "" {
		content = append(content, map[string]interface{}{"type": "text", "text": text})
		if render {
			printAssistantMessage(text)
		}
	}
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		call := toolCalls[index]
		if call.ID == "" {
			call.ID = nextFallbackToolCallID(index)
		}
		if call.Name == "" {
			return nil, usage, fmt.Errorf("Responses tool call %d is missing a function name", index)
		}
		var input interface{} = map[string]interface{}{}
		if call.Args != "" {
			if err := json.Unmarshal([]byte(call.Args), &input); err != nil {
				return nil, usage, fmt.Errorf("Responses tool call %s has invalid arguments: %w", call.Name, err)
			}
		}
		content = append(content, map[string]interface{}{
			"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
		})
	}
	if len(content) == 0 {
		return nil, usage, fmt.Errorf("Responses stream ended without text or tool calls")
	}
	result, _ := json.Marshal(content)
	return result, usage, nil
}
