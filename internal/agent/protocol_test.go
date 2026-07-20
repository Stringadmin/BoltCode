package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicThinkingAndMultipleToolsSurviveRoundTrip(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":12}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"inspect architecture"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_123"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"list_files"}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"internal\"}"}}`,
		`data: {"type":"content_block_stop","index":2}`,
		`data: {"type":"message_delta","usage":{"output_tokens":8}}`,
		"",
	}, "\n")

	a := &Agent{}
	data, usage, err := a.parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 8 {
		t.Fatalf("usage = %+v", usage)
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(data, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 || blocks[0]["type"] != "thinking" || blocks[0]["signature"] != "sig_123" {
		t.Fatalf("Anthropic blocks = %#v", blocks)
	}

	a.Messages = []Message{{Role: "user", Content: "inspect"}}
	tools := a.processResponse(data)
	if len(tools) != 2 || tools[0].ID != "toolu_1" || tools[1].ID != "toolu_2" {
		t.Fatalf("tool calls = %+v", tools)
	}
	assistantBlocks := messageContentBlocks(a.Messages[1].Content)
	if len(assistantBlocks) != 3 || assistantBlocks[0]["signature"] != "sig_123" {
		t.Fatalf("thinking metadata lost from history: %#v", assistantBlocks)
	}
	a.Messages = append(a.Messages, Message{Role: "user", Content: []map[string]interface{}{
		{"type": "tool_result", "tool_use_id": "toolu_1", "content": "module x"},
		{"type": "tool_result", "tool_use_id": "toolu_2", "content": "files"},
	}})
	if err := validateToolMessageSequence(a.Messages, true); err != nil {
		t.Fatalf("valid Anthropic tool sequence rejected: %v", err)
	}

	payload := buildAnthropicPayload(Config{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}, "system", a.Messages)
	if !strings.Contains(string(payload), `"type":"thinking"`) || !strings.Contains(string(payload), `"signature":"sig_123"`) {
		t.Fatalf("Anthropic payload dropped required thinking block: %s", payload)
	}
}

func TestValidateToolMessageSequenceRejectsMissingAndMismatchedResults(t *testing.T) {
	assistant := Message{Role: "assistant", Content: []interface{}{
		map[string]interface{}{"type": "tool_use", "id": "call_1", "name": "read_file", "input": map[string]interface{}{}},
		map[string]interface{}{"type": "tool_use", "id": "call_2", "name": "list_files", "input": map[string]interface{}{}},
	}}

	if err := validateToolMessageSequence([]Message{assistant}, false); err == nil {
		t.Fatal("missing tool-result message should fail")
	}
	oneResult := Message{Role: "user", Content: []map[string]interface{}{
		{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"},
	}}
	if err := validateToolMessageSequence([]Message{assistant, oneResult}, false); err == nil {
		t.Fatal("insufficient tool results should fail")
	}
	mismatch := Message{Role: "user", Content: []map[string]interface{}{
		{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"},
		{"type": "tool_result", "tool_use_id": "wrong", "content": "ok"},
	}}
	if err := validateToolMessageSequence([]Message{assistant, mismatch}, false); err == nil {
		t.Fatal("mismatched tool result should fail")
	}
}

func TestOpenAIAllowsPositionallyPairedMissingIDsButAnthropicDoesNot(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "", "name": "read_file", "input": map[string]interface{}{}},
		}},
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "", "content": "ok"},
		}},
	}
	if err := validateToolMessageSequence(messages, false); err != nil {
		t.Fatalf("OpenAI positional fallback rejected: %v", err)
	}
	if err := validateToolMessageSequence(messages, true); err == nil {
		t.Fatal("Anthropic sequence without IDs should fail")
	}
}

func TestRebindLatestSingleToolCallUpdatesCallAndResultTogether(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "call_old", "name": "read_file", "input": map[string]interface{}{}},
		}},
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "call_old", "content": "ok"},
		}},
	}
	if !rebindLatestSingleToolCall(messages, "call_new") {
		t.Fatal("single tool call was not rebound")
	}
	if err := validateToolMessageSequence(messages, false); err != nil {
		t.Fatalf("rebound sequence invalid: %v", err)
	}
	if got := messageContentBlocks(messages[0].Content)[0]["id"]; got != "call_new" {
		t.Fatalf("call id = %v", got)
	}
	if got := messageContentBlocks(messages[1].Content)[0]["tool_use_id"]; got != "call_new" {
		t.Fatalf("result id = %v", got)
	}

	multi := []Message{
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "a", "name": "read_file", "input": map[string]interface{}{}},
			map[string]interface{}{"type": "tool_use", "id": "b", "name": "list_files", "input": map[string]interface{}{}},
		}},
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "a", "content": "a"},
			{"type": "tool_result", "tool_use_id": "b", "content": "b"},
		}},
	}
	if rebindLatestSingleToolCall(multi, "unknown") {
		t.Fatal("ambiguous multi-call turn must not be rebound")
	}
}

func TestEmptyProviderStreamsFailClearly(t *testing.T) {
	a := &Agent{}
	if _, _, err := a.parseStream(strings.NewReader("")); err == nil {
		t.Fatal("empty Anthropic stream should fail")
	}
	if _, _, err := a.parseOpenAIStream(strings.NewReader("data: [DONE]\n\n")); err == nil {
		t.Fatal("empty OpenAI stream should fail")
	}
}

func TestAnthropicPayloadContainsOnlyValidNamedTools(t *testing.T) {
	payload := buildAnthropicPayload(Config{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}, "system", []Message{{Role: "user", Content: "hello"}})
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]interface{})
	if len(tools) != len(Tools) {
		t.Fatalf("payload tools = %d, definitions = %d", len(tools), len(Tools))
	}
	for i, raw := range tools {
		tool := raw.(map[string]interface{})
		if tool["name"] == nil || tool["input_schema"] == nil {
			t.Fatalf("tool %d is missing required schema fields: %#v", i, tool)
		}
		_, cached := tool["cache_control"]
		if cached != (i == len(tools)-1) {
			t.Fatalf("tool %d cache marker = %v; only final tool should be cached", i, cached)
		}
	}
}

func TestSSEDataLineAcceptsOptionalSpace(t *testing.T) {
	for _, line := range []string{`data:{"ok":true}`, `data: {"ok":true}`} {
		data, ok := parseSSEDataLine(line)
		if !ok || data != `{"ok":true}` {
			t.Fatalf("parseSSEDataLine(%q) = %q, %v", line, data, ok)
		}
	}
}

func TestProviderStreamErrorsSurfaceOriginalMessage(t *testing.T) {
	a := &Agent{}
	openAIStream := "data:{\"error\":{\"message\":\"model unavailable\",\"type\":\"server_error\"}}\n\n"
	if _, _, err := a.parseOpenAIStream(strings.NewReader(openAIStream)); err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("OpenAI stream error = %v", err)
	}
	badAnthropic := "data:{not-json}\n\n"
	if _, _, err := a.parseStream(strings.NewReader(badAnthropic)); err == nil || !strings.Contains(err.Error(), "invalid Anthropic stream event") {
		t.Fatalf("Anthropic malformed event error = %v", err)
	}
}

func TestAnthropicToolThinkingRequiresSignature(t *testing.T) {
	stream := strings.Join([]string{
		`data:{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`data:{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason"}}`,
		`data:{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}`,
		`data:{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"main.go\"}"}}`,
		"",
	}, "\n")
	a := &Agent{}
	if _, _, err := a.parseStream(strings.NewReader(stream)); err == nil || !strings.Contains(err.Error(), "missing the signature") {
		t.Fatalf("missing signature error = %v", err)
	}
}
