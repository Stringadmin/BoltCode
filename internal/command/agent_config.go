package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// agentConfigPath returns the path to the agent config file.
// Stored at user level (~/.boltcode/agent.json) so it works across projects.
func agentConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boltcode", "agent.json")
}

// LoadAgentConfig reads the active profile's config. It checks in order:
//  1. ~/.boltcode/agent.json (the active profile)
//  2. environment variables (BOLTCODE_API_KEY / ANTHROPIC_API_KEY / …) — the
//     headless path for CI and scripts where the config wizard can't run
//  3. ~/.claude/settings.json (reuse Claude Code's config — zero setup)
func (a *App) LoadAgentConfig() (*agent.Config, error) {
	pf, err := agent.LoadProfiles()
	if err == nil {
		if p := pf.CurrentProfile(); p != nil && p.APIKey != "" {
			cfg := p.ToConfig()
			return &cfg, nil
		}
	}

	// A malformed env var is a hard error (surface the typo), not a silent
	// fall-through to the next source.
	cfg, envErr := loadFromEnv()
	if envErr != nil {
		return nil, envErr
	}
	if cfg != nil {
		return cfg, nil
	}

	// Fall back to Claude Code's settings
	cfg, err = loadFromClaudeCode()
	if err == nil {
		return cfg, nil
	}

	return nil, fmt.Errorf("no API key configured — run 'config' to set up, or set BOLTCODE_API_KEY")
}

// envAPIKeys lists the recognized API-key environment variables in priority
// order, each with the provider it implies ("" for BOLTCODE_API_KEY, whose
// provider comes from BOLTCODE_PROVIDER, default anthropic). Keep this list in
// sync with the secret-scrubbing list in internal/agent/exec.go.
var envAPIKeys = []struct {
	env      string
	provider string
}{
	{"BOLTCODE_API_KEY", ""},
	{"ANTHROPIC_AUTH_TOKEN", "anthropic"},
	{"ANTHROPIC_API_KEY", "anthropic"},
	{"OPENAI_API_KEY", "openai"},
	{"ARK_API_KEY", "volc"},
	{"VOLC_API_KEY", "volc"},
}

// loadFromEnv builds an agent config from environment variables. The first
// non-empty key var in envAPIKeys wins. Optional overrides: BOLTCODE_MODEL,
// BOLTCODE_BASE_URL (defaults to the provider's well-known endpoint),
// BOLTCODE_CONTEXT_WINDOW, BOLTCODE_REASONING_EFFORT. Returns (nil, nil) when
// no key var is set, so callers fall through to the next config source;
// (nil, err) when a var IS set but malformed.
func loadFromEnv() (*agent.Config, error) {
	var key, provider string
	for _, k := range envAPIKeys {
		if v := strings.TrimSpace(os.Getenv(k.env)); v != "" {
			key, provider = v, k.provider
			break
		}
	}
	if key == "" {
		return nil, nil
	}
	if provider == "" {
		provider = strings.TrimSpace(os.Getenv("BOLTCODE_PROVIDER"))
	}
	if provider == "" {
		provider = "anthropic"
	}
	cfg := &agent.Config{
		Provider: provider,
		APIKey:   key,
		Model:    strings.TrimSpace(os.Getenv("BOLTCODE_MODEL")),
		BaseURL:  strings.TrimSpace(os.Getenv("BOLTCODE_BASE_URL")),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultProviderBaseURL(provider)
	}
	if v := strings.TrimSpace(os.Getenv("BOLTCODE_CONTEXT_WINDOW")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("BOLTCODE_CONTEXT_WINDOW=%q is not a positive integer", v)
		}
		cfg.ContextWindow = n
	}
	if v := strings.TrimSpace(os.Getenv("BOLTCODE_REASONING_EFFORT")); v != "" {
		effort, err := agent.ParseReasoningEffort(v)
		if err != nil {
			return nil, fmt.Errorf("BOLTCODE_REASONING_EFFORT: %w", err)
		}
		cfg.ReasoningEffort = effort
	}
	return cfg, nil
}

// defaultProviderBaseURL returns the well-known endpoint for a known provider
// id, or "" for providers (anthropic/custom) that have a built-in default or
// require an explicit BOLTCODE_BASE_URL.
func defaultProviderBaseURL(provider string) string {
	for _, p := range agent.KnownProviders {
		if strings.EqualFold(p.ID, provider) {
			return p.BaseURL
		}
	}
	return ""
}

// loadFromClaudeCode reads API config from Claude Code's settings.json
func loadFromClaudeCode() (*agent.Config, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	token := settings.Env["ANTHROPIC_AUTH_TOKEN"]
	if token == "" {
		token = settings.Env["ANTHROPIC_API_KEY"]
	}
	if token == "" {
		return nil, fmt.Errorf("no token in claude settings")
	}

	model := settings.Env["ANTHROPIC_MODEL"]
	if model == "" {
		model = agent.DefaultModel
	}
	baseURL := settings.Env["ANTHROPIC_BASE_URL"]

	return &agent.Config{
		Provider: "anthropic",
		APIKey:   token,
		Model:    model,
		BaseURL:  baseURL,
	}, nil
}

// SaveProfile upserts a profile by name and makes it the active one. Used by
// the config wizard when creating/editing a named provider configuration. This
// delegates to agent.SaveProfile so the load-modify-save runs under the
// cross-process profile lock (two `boltcode` processes editing profiles
// concurrently would otherwise lose an update).
func SaveProfile(name string, cfg *agent.Config) error {
	return agent.SaveProfile(name, cfg)
}

// SaveAgentConfig is a compatibility shim — saves as the "default" profile.
func SaveAgentConfig(cfg *agent.Config) error {
	return SaveProfile("default", cfg)
}

// ListProfiles returns all configured profiles and the active name.
func ListProfiles() ([]agent.Profile, string, error) {
	pf, err := agent.LoadProfiles()
	if err != nil {
		return nil, "", err
	}
	return pf.Profiles, pf.Current, nil
}

// LoadOwnAgentConfig reads ONLY BoltCode's own ~/.boltcode/agent.json (no Claude
// fallback) and returns the active profile as a Config. Used to pre-fill the
// config wizard with the current profile's values.
func LoadOwnAgentConfig() (*agent.Config, error) {
	pf, err := agent.LoadProfiles()
	if err != nil {
		return nil, err
	}
	if p := pf.CurrentProfile(); p != nil {
		cfg := p.ToConfig()
		return &cfg, nil
	}
	return nil, fmt.Errorf("no boltcode agent config")
}

// OwnProfileName returns the active profile's name (empty if none).
func OwnProfileName() string {
	pf, _ := agent.LoadProfiles()
	if pf == nil {
		return ""
	}
	return pf.Current
}
