package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if err := command.SaveProfile("deep", &agent.Config{Provider: "deepseek", APIKey: "deep-key", Model: "deepseek-chat", BaseURL: "https://deep", ContextWindow: 256_000, ReasoningEffort: agent.ReasoningHigh}); err != nil {
		t.Fatal(err)
	}
	if err := command.SaveProfile("claude", &agent.Config{Provider: "anthropic", APIKey: "claude-key", Model: "claude-sonnet"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAgentConfigForResume(nil, &agent.Session{ID: "1234abcd", Provider: "deepseek", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "deepseek" || cfg.APIKey != "deep-key" || cfg.ContextWindow != 256_000 || cfg.ReasoningEffort != agent.ReasoningHigh {
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

func TestNewAgentFromSessionRestoresConversationMetadata(t *testing.T) {
	created := time.Now().Add(-time.Hour)
	session := &agent.Session{
		ID:              "1234abcd",
		Workdir:         `F:\project`,
		RepoRoot:        `F:\project`,
		Intent:          "continue work",
		Provider:        "deepseek",
		BaseURL:         "https://example.test/v1",
		Model:           "deepseek-v4-pro",
		ContextWindow:   512_000,
		ReasoningEffort: agent.ReasoningLow,
		Messages:        []agent.Message{{Role: "user", Content: "hello"}},
		CreatedAt:       created,
	}
	a := newAgentFromSession(&agent.Config{Provider: "openai", APIKey: "local-key", Model: "fallback"}, session)
	if a.SessionID != session.ID || a.Workdir != session.Workdir || a.Intent != session.Intent {
		t.Fatalf("restored agent metadata = %+v", a)
	}
	if a.Config.Provider != session.Provider || a.Config.Model != session.Model || a.Config.ContextWindow != session.ContextWindow || a.Config.ReasoningEffort != session.ReasoningEffort {
		t.Fatalf("restored session config = %+v", a.Config)
	}
	if a.Config.APIKey != "local-key" {
		t.Fatal("local credential should be retained when restoring session metadata")
	}
}
