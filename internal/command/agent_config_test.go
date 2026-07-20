package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// clearEnvConfig unsets every env var loadFromEnv consults, so tests are
// deterministic even on machines where real keys (ANTHROPIC_API_KEY, …) are
// present in the environment.
func clearEnvConfig(t *testing.T) {
	t.Helper()
	for _, k := range envAPIKeys {
		t.Setenv(k.env, "")
	}
	t.Setenv("BOLTCODE_PROVIDER", "")
	t.Setenv("BOLTCODE_MODEL", "")
	t.Setenv("BOLTCODE_BASE_URL", "")
	t.Setenv("BOLTCODE_CONTEXT_WINDOW", "")
	t.Setenv("BOLTCODE_REASONING_EFFORT", "")
}

func TestLoadFromEnvNoVarsReturnsNil(t *testing.T) {
	clearEnvConfig(t)
	cfg, err := loadFromEnv()
	if err != nil || cfg != nil {
		t.Fatalf("loadFromEnv with no vars = (%+v, %v), want (nil, nil)", cfg, err)
	}
}

func TestLoadFromEnvBoltcodeKeyDefaultsAnthropic(t *testing.T) {
	clearEnvConfig(t)
	t.Setenv("BOLTCODE_API_KEY", "sk-test")
	cfg, err := loadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "anthropic" || cfg.APIKey != "sk-test" || cfg.Model != "" || cfg.BaseURL != "" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadFromEnvProviderSpecificKeys(t *testing.T) {
	tests := []struct {
		env         string
		wantProvide string
		wantBase    string
	}{
		{"ANTHROPIC_API_KEY", "anthropic", ""},
		{"OPENAI_API_KEY", "openai", "https://api.openai.com/v1"},
		{"ARK_API_KEY", "volc", "https://ark.cn-beijing.volces.com/api/v3"},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			clearEnvConfig(t)
			t.Setenv(tt.env, "key")
			cfg, err := loadFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Provider != tt.wantProvide || cfg.BaseURL != tt.wantBase {
				t.Fatalf("%s → config = %+v, want provider %s base %q", tt.env, cfg, tt.wantProvide, tt.wantBase)
			}
		})
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	clearEnvConfig(t)
	t.Setenv("BOLTCODE_API_KEY", "sk-test")
	t.Setenv("BOLTCODE_PROVIDER", "deepseek")
	t.Setenv("BOLTCODE_MODEL", "deepseek-chat")
	t.Setenv("BOLTCODE_BASE_URL", "https://example.test/v1")
	t.Setenv("BOLTCODE_CONTEXT_WINDOW", "128000")
	t.Setenv("BOLTCODE_REASONING_EFFORT", "low")
	cfg, err := loadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" || cfg.BaseURL != "https://example.test/v1" ||
		cfg.ContextWindow != 128000 || cfg.ReasoningEffort != agent.ReasoningLow {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadFromEnvMalformedValuesAreHardErrors(t *testing.T) {
	clearEnvConfig(t)
	t.Setenv("BOLTCODE_API_KEY", "sk-test")
	t.Setenv("BOLTCODE_CONTEXT_WINDOW", "huge")
	if _, err := loadFromEnv(); err == nil {
		t.Fatal("bad BOLTCODE_CONTEXT_WINDOW should error")
	}

	clearEnvConfig(t)
	t.Setenv("BOLTCODE_API_KEY", "sk-test")
	t.Setenv("BOLTCODE_REASONING_EFFORT", "extreme")
	if _, err := loadFromEnv(); err == nil {
		t.Fatal("bad BOLTCODE_REASONING_EFFORT should error")
	}
}

// The saved profile must win over env vars — env is the headless fallback,
// not an override of the user's explicit interactive choice.
func TestLoadAgentConfigPrefersProfileOverEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".boltcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	clearEnvConfig(t)
	t.Setenv("BOLTCODE_API_KEY", "env-key")

	app := setupRepo(t)
	if err := SaveProfile("mine", &agent.Config{Provider: "openai", APIKey: "profile-key", Model: "gpt-x"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadAgentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "profile-key" {
		t.Fatalf("profile should win over env, got key %q", cfg.APIKey)
	}
}

// With no profile and no Claude Code settings, the env var is what unblocks
// headless runs.
func TestLoadAgentConfigFallsBackToEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	clearEnvConfig(t)
	t.Setenv("OPENAI_API_KEY", "env-key")

	app := setupRepo(t)
	cfg, err := app.LoadAgentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "env-key" || cfg.Provider != "openai" {
		t.Fatalf("env fallback config = %+v", cfg)
	}
}
