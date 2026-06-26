package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ProfileFile is the on-disk shape of ~/.boltcode/agent.json: a set of named
// profiles plus a pointer to the active one.
type ProfileFile struct {
	Profiles []Profile `json:"profiles"`
	Current  string    `json:"current,omitempty"`
}

// profilePath returns ~/.boltcode/agent.json.
func profilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boltcode", "agent.json")
}

// LoadProfiles reads ~/.boltcode/agent.json, migrating the legacy single-config
// format (flat Config with api_key) into a one-entry profile set if needed.
// Returns an empty file (not an error) when missing.
func LoadProfiles() (*ProfileFile, error) {
	path := profilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProfileFile{}, nil
		}
		return nil, err
	}

	// New multi-profile format.
	var pf ProfileFile
	if err := json.Unmarshal(data, &pf); err == nil && pf.Profiles != nil {
		return &pf, nil
	}

	// Legacy flat Config → migrate into a "default" profile.
	var legacy Config
	if err := json.Unmarshal(data, &legacy); err == nil && legacy.APIKey != "" {
		name := "default"
		return &ProfileFile{
			Profiles: []Profile{{
				Name: name, Provider: legacy.Provider,
				APIKey: legacy.APIKey, Model: legacy.Model, BaseURL: legacy.BaseURL,
			}},
			Current: name,
		}, nil
	}
	return &ProfileFile{}, nil
}

// SaveProfiles writes the profile set with restrictive permissions.
func SaveProfiles(pf *ProfileFile) error {
	path := profilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(pf, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

// CurrentProfile returns the active profile, or nil if none.
func (pf *ProfileFile) CurrentProfile() *Profile {
	for i := range pf.Profiles {
		if pf.Profiles[i].Name == pf.Current {
			return &pf.Profiles[i]
		}
	}
	return nil
}

// SaveProfile upserts a profile by name and makes it the active one. Used by
// the interactive /profile new flow inside an agent session.
func SaveProfile(name string, cfg *Config) error {
	pf, err := LoadProfiles()
	if err != nil {
		pf = &ProfileFile{}
	}
	p := Profile{
		Name: name, Provider: cfg.Provider,
		APIKey: cfg.APIKey, Model: cfg.Model, BaseURL: cfg.BaseURL,
	}
	found := false
	for i := range pf.Profiles {
		if pf.Profiles[i].Name == name {
			pf.Profiles[i] = p
			found = true
			break
		}
	}
	if !found {
		pf.Profiles = append(pf.Profiles, p)
	}
	pf.Current = name
	return SaveProfiles(pf)
}

// SwitchProfile sets the active profile by name. Returns the activated profile
// or an error if the name doesn't exist.
func SwitchProfile(name string) (*Profile, error) {
	pf, err := LoadProfiles()
	if err != nil {
		return nil, err
	}
	for i := range pf.Profiles {
		if pf.Profiles[i].Name == name {
			pf.Current = name
			if err := SaveProfiles(pf); err != nil {
				return nil, err
			}
			return &pf.Profiles[i], nil
		}
	}
	return nil, fmt.Errorf("no profile named %q — have: %s", name, pf.ProfileNames())
}

// SetCurrentModel updates the model of the active profile only, leaving the
// rest untouched. Backs the /model <id> slash command.
func SetCurrentModel(model string) error {
	pf, err := LoadProfiles()
	if err != nil {
		return err
	}
	for i := range pf.Profiles {
		if pf.Profiles[i].Name == pf.Current {
			pf.Profiles[i].Model = model
			return SaveProfiles(pf)
		}
	}
	return fmt.Errorf("no active profile to set model on")
}

// ProfileNames returns a comma-joined list of profile names for messages.
func (pf *ProfileFile) ProfileNames() string {
	if pf == nil || len(pf.Profiles) == 0 {
		return "(none)"
	}
	out := ""
	for i, p := range pf.Profiles {
		if i > 0 {
			out += ", "
		}
		out += p.Name
	}
	return out
}
