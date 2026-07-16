package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
)

func TestLoadAgentConfigForResumeMatchesSavedProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".boltcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := command.SaveProfile("deep", &agent.Config{Provider: "deepseek", APIKey: "deep-key", Model: "deepseek-chat", BaseURL: "https://deep"}); err != nil {
		t.Fatal(err)
	}
	if err := command.SaveProfile("claude", &agent.Config{Provider: "anthropic", APIKey: "claude-key", Model: "claude-sonnet"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAgentConfigForResume(nil, &agent.Session{ID: "1234abcd", Provider: "deepseek", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "deepseek" || cfg.APIKey != "deep-key" {
		t.Fatalf("matched config = %+v", cfg)
	}
}

func TestLoadAgentConfigForLegacyResumeUsesActiveProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".boltcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := command.SaveProfile("active", &agent.Config{Provider: "openai", APIKey: "key", Model: "gpt"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAgentConfigForResume(nil, &agent.Session{ID: "1234abcd"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" || cfg.APIKey != "key" {
		t.Fatalf("legacy resume config = %+v", cfg)
	}
}
