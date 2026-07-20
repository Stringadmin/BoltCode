package agent

import (
	"os"
	"testing"
)

// withTempHomeProfiles redirects ~/.boltcode to a temp dir.
func withTempHomeProfiles(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	oldP := os.Getenv("USERPROFILE")
	oldH := os.Getenv("HOME")
	os.Setenv("USERPROFILE", dir)
	os.Setenv("HOME", dir)
	return func() {
		os.Setenv("USERPROFILE", oldP)
		os.Setenv("HOME", oldH)
	}
}

// mockInput returns an InputFn that yields the given lines in sequence.
func mockInput(lines ...string) func() string {
	i := 0
	return func() string {
		if i >= len(lines) {
			return "q"
		}
		line := lines[i]
		i++
		return line
	}
}

func TestProfilePickerSwitchByNumber(t *testing.T) {
	restore := withTempHomeProfiles(t)
	defer restore()

	// Seed two profiles.
	pf := &ProfileFile{
		Profiles: []Profile{
			{Name: "anthropic", Provider: "anthropic", APIKey: "sk-a", Model: "claude-x"},
			{Name: "ark", Provider: "volc", APIKey: "sk-ark", Model: "glm-5.2"},
		},
		Current: "anthropic",
	}
	if err := SaveProfiles(pf); err != nil {
		t.Fatal(err)
	}

	a := &Agent{Config: pf.Profiles[0].ToConfig(), InputFn: mockInput("2")}
	a.profilePicker()

	// Agent config should now reflect profile 2 (ark).
	if a.Config.Model != "glm-5.2" {
		t.Errorf("after switch, model = %q, want glm-5.2", a.Config.Model)
	}
	if a.Config.APIKey != "sk-ark" {
		t.Errorf("after switch, key = %q, want sk-ark", a.Config.APIKey)
	}
	// Persisted current should be ark.
	loaded, _ := LoadProfiles()
	if loaded.Current != "ark" {
		t.Errorf("persisted current = %q, want ark", loaded.Current)
	}
}

func TestProfilePickerQuit(t *testing.T) {
	restore := withTempHomeProfiles(t)
	defer restore()
	pf := &ProfileFile{
		Profiles: []Profile{{Name: "a", APIKey: "k", Model: "m"}},
		Current:  "a",
	}
	SaveProfiles(pf)

	a := &Agent{Config: pf.Profiles[0].ToConfig(), InputFn: mockInput("q")}
	a.profilePicker()
	// No switch happened.
	if a.Config.APIKey != "k" {
		t.Errorf("config changed on quit")
	}
}

func TestProfilePickerInvalidThenSwitch(t *testing.T) {
	restore := withTempHomeProfiles(t)
	defer restore()
	pf := &ProfileFile{
		Profiles: []Profile{
			{Name: "a", APIKey: "k1", Model: "m1"},
			{Name: "b", APIKey: "k2", Model: "m2"},
		},
		Current: "a",
	}
	SaveProfiles(pf)

	a := &Agent{Config: pf.Profiles[0].ToConfig(), InputFn: mockInput("9", "2")}
	a.profilePicker()
	if a.Config.APIKey != "k2" {
		t.Errorf("expected switch to b after invalid, got key %q", a.Config.APIKey)
	}
}

func TestCreateNewProfile(t *testing.T) {
	restore := withTempHomeProfiles(t)
	defer restore()
	pf := &ProfileFile{
		Profiles: []Profile{{Name: "old", APIKey: "k", Model: "m"}},
		Current:  "old",
	}
	SaveProfiles(pf)

	// Picker: choose "n" (new), then fill fields.
	a := &Agent{Config: pf.Profiles[0].ToConfig(),
		InputFn: mockInput("n", "newprof", "anthropic", "sk-new", "claude-y", "", "256k", "high")}
	a.profilePicker()

	if a.Config.APIKey != "sk-new" {
		t.Errorf("new profile not activated, key=%q", a.Config.APIKey)
	}
	loaded, _ := LoadProfiles()
	found := false
	for _, p := range loaded.Profiles {
		if p.Name == "newprof" {
			found = true
			if p.Model != "claude-y" {
				t.Errorf("new profile model = %q", p.Model)
			}
			if p.ContextWindow != 256_000 {
				t.Errorf("new profile context = %d", p.ContextWindow)
			}
			if p.ReasoningEffort != ReasoningHigh {
				t.Errorf("new profile reasoning effort = %q", p.ReasoningEffort)
			}
		}
	}
	if !found {
		t.Error("new profile not persisted")
	}
	if loaded.Current != "newprof" {
		t.Errorf("current = %q, want newprof", loaded.Current)
	}
}

func TestCreateNewProfileCancelOnEmptyKey(t *testing.T) {
	restore := withTempHomeProfiles(t)
	defer restore()
	a := &Agent{InputFn: mockInput("n", "p", "anthropic", "", "q")}
	a.profilePicker()
	// No profile should exist.
	loaded, _ := LoadProfiles()
	if len(loaded.Profiles) != 0 {
		t.Errorf("profile created despite empty key: %d", len(loaded.Profiles))
	}
}
