package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// agentConfigPath returns the path to the agent config file.
// Stored at user level (~/.boltcode/agent.json) so it works across projects.
func agentConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boltcode", "agent.json")
}

// LoadAgentConfig reads the active profile's config. It checks in order:
// 1. ~/.boltcode/agent.json (the active profile)
// 2. ~/.claude/settings.json (reuse Claude Code's config — zero setup)
func (a *App) LoadAgentConfig() (*agent.Config, error) {
	pf, err := agent.LoadProfiles()
	if err == nil {
		if p := pf.CurrentProfile(); p != nil && p.APIKey != "" {
			cfg := p.ToConfig()
			return &cfg, nil
		}
	}

	// Fall back to Claude Code's settings
	cfg, err := loadFromClaudeCode()
	if err == nil {
		return cfg, nil
	}

	return nil, fmt.Errorf("no API key configured — run 'config' to set up")
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
