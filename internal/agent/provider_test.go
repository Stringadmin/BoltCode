package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertMessagesToOpenAIIncludesEveryInMemoryToolResult(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "inspect the project"},
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "I will inspect it."},
			map[string]interface{}{"type": "tool_use", "id": "call_1", "name": "list_files", "input": map[string]interface{}{"path": "."}},
			map[string]interface{}{"type": "tool_use", "id": "call_2", "name": "read_file", "input": map[string]interface{}{"path": "go.mod"}},
			map[string]interface{}{"type": "tool_use", "id": "call_3", "name": "read_file", "input": map[string]interface{}{"path": "dispatch.go"}},
		}},
		// This is the exact in-memory type produced by loop.go. The old
		// converter only handled []interface{} and silently dropped all three.
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "call_1", "content": "files"},
			{"type": "tool_result", "tool_use_id": "call_2", "content": "module x"},
			{"type": "tool_result", "tool_use_id": "call_3", "content": "package main"},
		}},
	}

	out := convertMessagesToOpenAI("system", messages)
	if len(out) != 6 { // system + user + assistant + 3 tool messages
		t.Fatalf("converted messages = %d: %#v", len(out), out)
	}
	assistant, ok := out[2]["tool_calls"].([]map[string]interface{})
	if !ok || len(assistant) != 3 {
		t.Fatalf("assistant tool calls = %#v", out[2]["tool_calls"])
	}
	for i, wantID := range []string{"call_1", "call_2", "call_3"} {
		tool := out[i+3]
		if tool["role"] != "tool" || tool["tool_call_id"] != wantID {
			t.Fatalf("tool message %d = %#v", i, tool)
		}
	}
}

func TestConvertMessagesToOpenAIHandlesJSONDecodedBlocks(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "call_a", "name": "read_file", "input": map[string]interface{}{"path": "main.go"}},
		}},
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "tool_result", "tool_use_id": "call_a", "content": "ok"},
		}},
	}
	out := convertMessagesToOpenAI("system", messages)
	if len(out) != 3 || out[2]["tool_call_id"] != "call_a" {
		t.Fatalf("decoded blocks converted incorrectly: %#v", out)
	}
}

func TestConvertMessagesToOpenAIPairsMissingLegacyIDs(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "", "name": "read_file", "input": map[string]interface{}{"path": "main.go"}},
		}},
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "", "content": "ok"},
		}},
	}
	out := convertMessagesToOpenAI("system", messages)
	calls := out[1]["tool_calls"].([]map[string]interface{})
	generatedID := calls[0]["id"].(string)
	if generatedID == "" || out[2]["tool_call_id"] != generatedID {
		t.Fatalf("fallback IDs not paired: %#v", out)
	}
}

func TestParseOpenAIStreamGeneratesMissingToolCallID(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	a := &Agent{}
	data, _, err := a.parseOpenAIStream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(data, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0]["id"] == "" || blocks[0]["name"] != "read_file" {
		t.Fatalf("parsed tool block = %#v", blocks)
	}
}

func TestParseOpenAIStreamRejectsInvalidToolArguments(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","function":{"name":"read_file","arguments":"{"}}]}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	a := &Agent{}
	if _, _, err := a.parseOpenAIStream(strings.NewReader(stream)); err == nil {
		t.Fatal("invalid streamed tool arguments should return an error")
	}
}
