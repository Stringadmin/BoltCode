package agent

import (
	"strings"
	"testing"
)

func TestCompactMessagesActuallyReplacesLargeFirstContext(t *testing.T) {
	a := &Agent{
		Config: Config{Provider: "anthropic", APIKey: "x", BaseURL: "://invalid"},
		Messages: []Message{
			{Role: "user", Content: strings.Repeat("large project scan ", 500)},
			{Role: "assistant", Content: "acknowledged"},
			{Role: "user", Content: "continue"},
		},
	}
	before := estimateTokens(a.Messages)
	if err := a.compactMessages(1); err != nil {
		t.Fatal(err)
	}
	after := estimateTokens(a.Messages)
	if after >= before {
		t.Fatalf("compaction did not reduce context: before=%d after=%d", before, after)
	}
	first, _ := a.Messages[0].Content.(string)
	if !strings.Contains(first, "Context summary") {
		t.Fatalf("first message is not compacted summary: %q", first)
	}
}

func TestCompactMessagesDoesNotSplitToolCallAndResult(t *testing.T) {
	a := &Agent{
		Config: Config{Provider: "anthropic", APIKey: "x", BaseURL: "://invalid"},
		Messages: []Message{
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "tool_use", "id": "call-1", "name": "read_file", "input": map[string]interface{}{"path": "main.go"}},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call-1", "content": "ok"},
			}},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "done"},
			}},
		},
	}
	if err := a.compactMessages(2); err != nil {
		t.Fatal(err)
	}
	if err := validateToolMessageSequence(a.Messages, true); err != nil {
		t.Fatalf("compacted history broke tool pairing: %v\n%+v", err, a.Messages)
	}
}
