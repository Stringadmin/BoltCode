package shell

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chzyer/readline"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
)

func TestProfileNameFromInput(t *testing.T) {
	profiles := []agent.Profile{{Name: "work"}, {Name: "local"}}

	if got, exists := profileNameFromInput("2", profiles); got != "local" || !exists {
		t.Fatalf("numeric selection = %q, %v; want local, true", got, exists)
	}
	if got, exists := profileNameFromInput("WORK", profiles); got != "work" || !exists {
		t.Fatalf("case-insensitive selection = %q, %v; want work, true", got, exists)
	}
	if got, exists := profileNameFromInput("personal", profiles); got != "personal" || exists {
		t.Fatalf("new profile = %q, %v; want personal, false", got, exists)
	}
}

func TestProviderFromInput(t *testing.T) {
	p, ok := providerFromInput("3")
	if !ok || p.ID != "volc" {
		t.Fatalf("provider 3 = %+v, %v; want volc", p, ok)
	}

	p, ok = providerFromInput("DEEPSEEK")
	if !ok || p.ID != "deepseek" {
		t.Fatalf("provider by ID = %+v, %v; want deepseek", p, ok)
	}

	p, ok = providerFromInput("ollama")
	if !ok || p.ID != "ollama" || !p.Custom {
		t.Fatalf("custom provider = %+v, %v", p, ok)
	}

	if _, ok = providerFromInput("99"); ok {
		t.Fatal("out-of-range numeric provider selection should fail")
	}
}

func TestModelFromInput(t *testing.T) {
	models := agent.ModelsForProvider("deepseek")
	if got, ok := modelFromInput("2", models); !ok || got != "deepseek-reasoner" {
		t.Fatalf("model 2 = %q, %v; want deepseek-reasoner", got, ok)
	}
	if got, ok := modelFromInput("new-model", models); !ok || got != "new-model" {
		t.Fatalf("custom model = %q, %v", got, ok)
	}
	if _, ok := modelFromInput("9", models); ok {
		t.Fatal("out-of-range numeric model selection should fail")
	}
	if got, ok := modelFromInput("7", nil); !ok || got != "7" {
		t.Fatalf("numeric custom model = %q, %v; want 7, true", got, ok)
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, value := range []string{"", "https://api.example.com/v1", "http://localhost:11434"} {
		if err := validateBaseURL(value); err != nil {
			t.Errorf("validateBaseURL(%q): %v", value, err)
		}
	}
	for _, value := range []string{"api.example.com", "ftp://api.example.com", "https://"} {
		if err := validateBaseURL(value); err == nil {
			t.Errorf("validateBaseURL(%q) unexpectedly succeeded", value)
		}
	}
}

func TestProviderDefaults(t *testing.T) {
	if got := providerDefaultURL("deepseek"); got != "https://api.deepseek.com/v1" {
		t.Fatalf("deepseek default URL = %q", got)
	}
	if providerRequiresBaseURL("anthropic") {
		t.Fatal("anthropic should not require a custom Base URL")
	}
	if !providerRequiresBaseURL("ollama") {
		t.Fatal("unknown provider should require a Base URL")
	}
	if providerRequiresBaseURL("") {
		t.Fatal("empty legacy provider should use Anthropic defaults")
	}
	if got := providerDisplay(""); got != "Anthropic (Claude) [anthropic]" {
		t.Fatalf("empty provider display = %q", got)
	}
}

func TestMaskKey(t *testing.T) {
	if got := maskKey("sk-1234567890"); got != "sk-1...7890" {
		t.Fatalf("maskKey() = %q", got)
	}
	if got := maskKey("short"); got != "*****" {
		t.Fatalf("short maskKey() = %q", got)
	}
}

func TestRunConfigCancelBeforeSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	rl, err := readline.NewEx(&readline.Config{
		Prompt: mainPrompt,
		Stdin:  io.NopCloser(strings.NewReader("test-profile\nq\n")),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Close()

	s := &Session{rl: rl}
	s.runConfig(nil)

	profiles, _, err := command.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Fatalf("cancelled wizard saved %d profiles", len(profiles))
	}
}

func TestRunConfigPasswordKeepsFollowingInputIntact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".boltcode"), 0o700); err != nil {
		t.Fatal(err)
	}

	// The model deliberately contains b/d/f. On Windows these letters were
	// previously interpreted as editing commands after term.ReadPassword raced
	// with readline for console events.
	input := "test-profile\n4\nsk-dfb-secret\ndfb-model\n\n\n"
	rl, err := readline.NewEx(&readline.Config{
		Prompt: mainPrompt,
		Stdin:  io.NopCloser(strings.NewReader(input)),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Close()

	s := &Session{rl: rl}
	s.runConfig(nil)

	profiles, current, err := command.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || current != "test-profile" {
		t.Fatalf("profiles = %+v, current = %q", profiles, current)
	}
	p := profiles[0]
	if p.Provider != "deepseek" || p.APIKey != "sk-dfb-secret" || p.Model != "dfb-model" {
		t.Fatalf("saved input was corrupted: %+v", p)
	}
}
