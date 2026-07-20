package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/keyring"
)

const aesPrefix = "$AES$"

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

// profileLockPath returns the path to the cross-process lock guarding
// ~/.boltcode/agent.json. That file is shared across every BoltCode process on
// the machine, so two terminals editing profiles concurrently would race on
// the LoadProfiles → modify → SaveProfiles read-modify-write and lose one
// update. The lock serializes those RMW sequences.
func profileLockPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boltcode", ".profiles.lock")
}

// withProfileLock acquires the cross-process profile lock, runs fn, and
// releases it. It is the single chokepoint that keeps profile RMW sequences
// atomic across concurrent BoltCode processes.
func withProfileLock(fn func() error) error {
	fl, err := fio.NewFileLock(profileLockPath())
	if err != nil {
		return fmt.Errorf("opening profile lock: %w", err)
	}
	defer fl.Close()
	if err := fl.Lock(); err != nil {
		return fmt.Errorf("acquiring profile lock: %w", err)
	}
	defer fl.Unlock()
	return fn()
}

// LoadProfiles reads ~/.boltcode/agent.json, migrating the legacy single-config
// format (flat Config with api_key) into a one-entry profile set if needed.
// Returns an empty file (not an error) when missing. API keys stored with the
// encryption prefix are transparently decrypted.
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
		// Decrypt encrypted API keys.
		for i := range pf.Profiles {
			if strings.HasPrefix(pf.Profiles[i].APIKey, aesPrefix) {
				plain, decErr := keyring.Unwrap(pf.Profiles[i].APIKey[len(aesPrefix):])
				if decErr != nil {
					// Log but don't fail — the user can re-enter the key.
					pf.Profiles[i].APIKey = ""
				} else {
					pf.Profiles[i].APIKey = plain
				}
			}
		}
		return &pf, nil
	}

	// Legacy flat Config → migrate into a "default" profile.
	var legacy Config
	if err := json.Unmarshal(data, &legacy); err == nil && legacy.APIKey != "" {
		name := "default"
		return &ProfileFile{
			Profiles: []Profile{{
				Name: name, Provider: legacy.Provider,
				APIKey: legacy.APIKey, Model: legacy.Model, BaseURL: legacy.BaseURL, ContextWindow: legacy.ContextWindow, ReasoningEffort: legacy.ReasoningEffort,
			}},
			Current: name,
		}, nil
	}
	return &ProfileFile{}, nil
}

// SaveProfiles writes the profile set with restrictive permissions. The write
// is atomic (temp + rename) so a crash mid-write can't corrupt the file that
// holds every API key. API keys are encrypted at rest using AES-256-GCM with a
// per-machine key stored at ~/.boltcode/.key (0600).
func SaveProfiles(pf *ProfileFile) error {
	path := profilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Clone profiles so encryption doesn't mutate the caller's struct.
	clone := *pf
	clone.Profiles = make([]Profile, len(pf.Profiles))
	for i := range pf.Profiles {
		clone.Profiles[i] = pf.Profiles[i]
		if clone.Profiles[i].APIKey != "" && !strings.HasPrefix(clone.Profiles[i].APIKey, aesPrefix) {
			wrapped, err := keyring.Wrap(clone.Profiles[i].APIKey)
			if err != nil {
				return fmt.Errorf("encrypting key for profile %q: %w", pf.Profiles[i].Name, err)
			}
			clone.Profiles[i].APIKey = aesPrefix + wrapped
		}
	}
	data, err := json.MarshalIndent(clone, "", "  ")
	if err != nil {
		return err
	}
	return fio.WriteFileAtomic(path, data, 0o600)
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
// the interactive /profile new flow inside an agent session. The whole
// load-modify-save is locked so two concurrent processes can't clobber each
// other's profiles.
func SaveProfile(name string, cfg *Config) error {
	return withProfileLock(func() error {
		pf, err := LoadProfiles()
		if err != nil {
			pf = &ProfileFile{}
		}
		p := Profile{
			Name: name, Provider: cfg.Provider,
			APIKey: cfg.APIKey, Model: cfg.Model, BaseURL: cfg.BaseURL, ContextWindow: cfg.ContextWindow, ReasoningEffort: NormalizeReasoningEffort(cfg.ReasoningEffort),
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
	})
}

// SwitchProfile sets the active profile by name. Returns the activated profile
// or an error if the name doesn't exist. Locked to keep the read-modify-write
// atomic across processes.
func SwitchProfile(name string) (*Profile, error) {
	var activated *Profile
	if err := withProfileLock(func() error {
		pf, err := LoadProfiles()
		if err != nil {
			return err
		}
		for i := range pf.Profiles {
			if pf.Profiles[i].Name == name {
				pf.Current = name
				if err := SaveProfiles(pf); err != nil {
					return err
				}
				activated = &pf.Profiles[i]
				return nil
			}
		}
		return fmt.Errorf("no profile named %q — have: %s", name, pf.ProfileNames())
	}); err != nil {
		return nil, err
	}
	return activated, nil
}

// SetCurrentModel updates the model of the active profile only, leaving the
// rest untouched. Backs the /model <id> slash command. Locked for the same
// reason as SaveProfile.
func SetCurrentModel(model string) error {
	return withProfileLock(func() error {
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
	})
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
