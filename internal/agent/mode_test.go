package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestModeParsingAndCycle(t *testing.T) {
	for input, want := range map[string]Mode{"BUILD": ModeBuild, " plan ": ModePlan, "ask": ModeAsk} {
		got, err := ParseMode(input)
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParseMode("unsafe"); err == nil {
		t.Fatal("unknown mode should fail")
	}
	if NextMode(ModeBuild) != ModePlan || NextMode(ModePlan) != ModeAsk || NextMode(ModeAsk) != ModeBuild {
		t.Fatal("mode cycle should be build -> plan -> ask -> build")
	}
}

func TestModeToolPermissions(t *testing.T) {
	for _, name := range []string{"read_file", "search_code", "list_files", "web_fetch", "git_status", "git_diff", "glob"} {
		if !ModeAllowsTool(ModePlan, name) || !ModeAllowsTool(ModeAsk, name) {
			t.Errorf("inspection tool %q should be available in plan and ask", name)
		}
	}
	if !ModeAllowsTool(ModePlan, "todo_write") || ModeAllowsTool(ModeAsk, "todo_write") {
		t.Fatal("todo_write should be plan-only outside build mode")
	}
	for _, name := range []string{"write_file", "edit_file", "multi_edit", "delete_file", "rename_file", "run_command", "get_diagnostics", "git_commit", "update_memory"} {
		if ModeAllowsTool(ModePlan, name) || ModeAllowsTool(ModeAsk, name) {
			t.Errorf("mutating tool %q escaped read-only mode", name)
		}
	}
}

func TestProviderPayloadsAdvertiseOnlyModeAllowedTools(t *testing.T) {
	providers := []Config{
		{Provider: "anthropic", Model: "claude-test"},
		{Provider: "openai", Model: "gpt-test"},
		{Provider: "deepseek", Model: "deepseek-test"},
		{Provider: "volcengine", Model: "ark-test"},
	}
	for _, cfg := range providers {
		for _, mode := range []Mode{ModeBuild, ModePlan, ModeAsk} {
			t.Run(cfg.Provider+"/"+string(mode), func(t *testing.T) {
				payload := buildPayloadForMode(cfg, "system", []Message{{Role: "user", Content: "hello"}}, mode)
				names := payloadToolNames(t, payload, isAnthropicProvider(cfg))
				for _, name := range names {
					if !ModeAllowsTool(mode, name) {
						t.Fatalf("payload exposed forbidden %s tool %q: %v", mode, name, names)
					}
				}
				if containsString(names, "write_file") != (mode == ModeBuild) {
					t.Fatalf("write_file exposure for %s = %v: %v", mode, containsString(names, "write_file"), names)
				}
				if containsString(names, "todo_write") != (mode != ModeAsk) {
					t.Fatalf("todo_write exposure for %s = %v: %v", mode, containsString(names, "todo_write"), names)
				}
			})
		}
	}
}

func TestExecToolRejectsModeBypassBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Mode: ModePlan, Workdir: dir}
	result := a.ExecTool(ToolCall{ID: "tool_1", Name: "write_file", Input: json.RawMessage(`{"path":"blocked.txt","content":"no"}`)})
	if !result.IsError || result.ToolUseID != "tool_1" {
		t.Fatalf("blocked result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("forbidden tool created a file: %v", err)
	}
}

func payloadToolNames(t *testing.T, payload []byte, anthropic bool) []string {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	definitions, _ := body["tools"].([]interface{})
	names := make([]string, 0, len(definitions))
	for _, raw := range definitions {
		definition, _ := raw.(map[string]interface{})
		if anthropic {
			name, _ := definition["name"].(string)
			names = append(names, name)
			continue
		}
		function, _ := definition["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		names = append(names, name)
	}
	return names
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
