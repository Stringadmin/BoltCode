package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempHome redirects ~/.boltcode to a temp dir for the test, returning a
// restore func. profilePath() reads HOME each call, so this is reliable.
func withTempHome(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	old := os.Getenv("USERPROFILE")
	// On Windows os.UserHomeDir prefers USERPROFILE; on Unix, HOME.
	os.Setenv("USERPROFILE", dir)
	os.Setenv("HOME", dir)
	return func() {
		os.Setenv("USERPROFILE", old)
		os.Setenv("HOME", old)
	}
}

func TestLoadProfilesEmpty(t *testing.T) {
	restore := withTempHome(t)
	defer restore()
	pf, err := LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(pf.Profiles))
	}
}

func TestLegacyConfigMigration(t *testing.T) {
	restore := withTempHome(t)
	defer restore()
	// Write a legacy single-config agent.json.
	legacy := Config{Provider: "anthropic", APIKey: "sk-legacy", Model: "claude-x", BaseURL: "https://p", ContextWindow: 256_000}
	dir := filepath.Dir(profilePath())
	os.MkdirAll(dir, 0o700)
	data, _ := json.Marshal(legacy)
	os.WriteFile(profilePath(), data, 0o600)

	pf, err := LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Profiles) != 1 {
		t.Fatalf("expected 1 migrated profile, got %d", len(pf.Profiles))
	}
	if pf.Profiles[0].Name != "default" || pf.Profiles[0].APIKey != "sk-legacy" {
		t.Errorf("migration wrong: %+v", pf.Profiles[0])
	}
	if pf.Profiles[0].ContextWindow != 256_000 {
		t.Errorf("migrated context window = %d", pf.Profiles[0].ContextWindow)
	}
	if pf.Current != "default" {
		t.Errorf("current = %q, want default", pf.Current)
	}
}

func TestSaveSwitchSetModel(t *testing.T) {
	restore := withTempHome(t)
	defer restore()

	// Save two profiles.
	pf := &ProfileFile{
		Profiles: []Profile{
			{Name: "anthropic", Provider: "anthropic", APIKey: "sk-a", Model: "claude-sonnet-4-20250514"},
			{Name: "ark", Provider: "volc", APIKey: "sk-ark", Model: "glm-5.2", BaseURL: "https://ark", ContextWindow: 192_000},
		},
		Current: "anthropic",
	}
	if err := SaveProfiles(pf); err != nil {
		t.Fatal(err)
	}

	// SetCurrentModel updates only the active profile's model.
	if err := SetCurrentModel("claude-opus-4-8"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadProfiles()
	cur := loaded.CurrentProfile()
	if cur.Model != "claude-opus-4-8" {
		t.Errorf("active model = %q, want claude-opus-4-8", cur.Model)
	}
	// The other profile must be untouched.
	var ark *Profile
	for i := range loaded.Profiles {
		if loaded.Profiles[i].Name == "ark" {
			ark = &loaded.Profiles[i]
		}
	}
	if ark.Model != "glm-5.2" {
		t.Errorf("ark model changed to %q", ark.Model)
	}

	// SwitchProfile activates ark.
	p, err := SwitchProfile("ark")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "ark" || p.BaseURL != "https://ark" || p.ContextWindow != 192_000 {
		t.Errorf("switch returned wrong profile: %+v", p)
	}
	loaded2, _ := LoadProfiles()
	if loaded2.Current != "ark" {
		t.Errorf("current = %q, want ark", loaded2.Current)
	}

	// SwitchProfile on unknown name errors.
	if _, err := SwitchProfile("nope"); err == nil {
		t.Fatal("switch to unknown should error")
	}
}
