package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesProviderDetectionAndURL(t *testing.T) {
	tests := []struct {
		config Config
		want   bool
		url    string
	}{
		{Config{Provider: "openai", BaseURL: "https://right.codes/codex/v1"}, true, "https://right.codes/codex/v1/responses"},
		{Config{Provider: "openai-responses", BaseURL: "https://example.test/v1"}, true, "https://example.test/v1/responses"},
		{Config{Provider: "openai", BaseURL: "https://example.test/v1/responses"}, true, "https://example.test/v1/responses"},
		{Config{Provider: "openai", BaseURL: "https://example.test/v1/chat/completions"}, false, "https://example.test/v1/chat/completions"},
	}
	for _, test := range tests {
		if got := isResponsesProvider(test.config); got != test.want {
			t.Errorf("isResponsesProvider(%+v) = %v, want %v", test.config, got, test.want)
		}
		if got := providerURL(test.config); got != test.url {
			t.Errorf("providerURL(%+v) = %q, want %q", test.config, got, test.url)
		}
	}
}

func TestResponsesPayloadUsesFunctionCallOutput(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "call_1", "name": "list_files", "input": map[string]interface{}{"path": "."}},
		}},
		{Role: "user", Content: []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "call_1", "content": "main.go"},
		}},
	}
	payload := buildResponsesPayloadWithTools(
		Config{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/codex/v1", ReasoningEffort: ReasoningHigh},
		"system",
		messages,
		Tools[:1],
	)
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "system" || body["stream"] != true || body["store"] != false {
		t.Fatalf("Responses request metadata = %#v", body)
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	input := body["input"].([]interface{})
	if len(input) != 3 {
		t.Fatalf("input items = %d: %#v", len(input), input)
	}
	call := input[1].(map[string]interface{})
	result := input[2].(map[string]interface{})
	if call["type"] != "function_call" || call["call_id"] != "call_1" {
		t.Fatalf("function call item = %#v", call)
	}
	if result["type"] != "function_call_output" || result["call_id"] != "call_1" || result["output"] != "main.go" {
		t.Fatalf("function output item = %#v", result)
	}
	tools := body["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" || tool["name"] == "" || tool["parameters"] == nil || tool["function"] != nil {
		t.Fatalf("Responses tool schema = %#v", tool)
	}
}

func TestParseResponsesStreamAssemblesFunctionCall(t *testing.T) {
	stream := strings.Join([]string{
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read_file","arguments":""}}`,
		`event: response.function_call_arguments.delta`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"main.go\"}"}`,
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"main.go\"}"}}`,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":12,"output_tokens":4}}}`,
		"",
	}, "\n")
	data, usage, err := parseResponsesStream(strings.NewReader(stream), false)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", usage)
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(data, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0]["id"] != "call_1" || blocks[0]["name"] != "read_file" {
		t.Fatalf("tool blocks = %#v", blocks)
	}
	if blocks[0]["input"].(map[string]interface{})["path"] != "main.go" {
		t.Fatalf("tool input = %#v", blocks[0]["input"])
	}
}

func TestCodexResponsesRequestSendsMatchingToolOutput(t *testing.T) {
	var receivedOutput bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/v1/responses" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		var body struct {
			Input []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		var callID, outputID string
		for _, item := range body.Input {
			if item.Type == "function_call" {
				callID = item.CallID
			}
			if item.Type == "function_call_output" {
				outputID = item.CallID
			}
		}
		receivedOutput = callID == "call_C6" && outputID == callID
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":1}}}`)
		fmt.Fprintln(w)
	}))
	defer srv.Close()

	a := &Agent{
		Config: Config{Provider: "openai", BaseURL: srv.URL + "/codex/v1", APIKey: "k", Model: "gpt-test"},
		Messages: []Message{
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "tool_use", "id": "call_C6", "name": "list_files", "input": map[string]interface{}{"path": "."}},
			}},
			{Role: "user", Content: []map[string]interface{}{
				{"type": "tool_result", "tool_use_id": "call_C6", "content": "main.go"},
			}},
		},
	}
	content, usage, err := a.callAPI("system")
	if err != nil {
		t.Fatal(err)
	}
	if !receivedOutput || len(content) == 0 || usage.InputTokens != 8 {
		t.Fatalf("receivedOutput=%v usage=%+v content=%s", receivedOutput, usage, content)
	}
}
