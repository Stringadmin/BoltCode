package shell

import (
	"fmt"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/tui"
)

// Resume handles `boltcode --resume [id]`. With an id it loads that saved
// session and re-enters the agent conversation where it left off. Without an
// id it lists recent sessions for the user to pick (or resume the latest).
func Resume(version string, args []string) error {
	// No id → list recent sessions and let the user pick.
	if len(args) == 0 {
		return listAndPickSession(version)
	}
	id := strings.TrimSpace(args[0])
	s, err := agent.LoadSession(id)
	if err != nil {
		return err
	}
	return startResumedAgent(version, s)
}

// listAndPickSession shows recent sessions and resumes the chosen one.
func listAndPickSession(version string) error {
	sessions, err := agent.ListSessions()
	if err != nil || len(sessions) == 0 {
		return fmt.Errorf("no saved sessions — start one with 'boltcode' then 'run'")
	}

	selected, err := tui.PickSession(sessions)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	return startResumedAgent(version, selected)
}

// startResumedAgent re-enters the agent loop with a saved session's messages.
func startResumedAgent(version string, s *agent.Session) error {
	// Load the agent config (API key etc.) — try the session's model's profile
	// context is gone, so just use the active profile.
	app, _ := command.Load(s.Workdir)
	cfg, err := loadAgentConfigForResume(app, s)
	if err != nil {
		return err
	}

	a := &agent.Agent{
		Config:           *cfg,
		Workdir:          s.Workdir,
		RepoRoot:         s.RepoRoot,
		Intent:           s.Intent,
		Messages:         s.Messages,
		TodoList:         s.TodoList,
		SessionID:        s.ID,
		SessionCreatedAt: &s.CreatedAt,
	}

	// If the saved model differs, honor it so resume uses what the session had.
	if s.Provider != "" {
		a.Config.Provider = s.Provider
		a.Config.BaseURL = s.BaseURL
	}
	if s.Model != "" {
		a.Config.Model = s.Model
	}
	if s.ContextWindow > 0 {
		a.Config.ContextWindow = s.ContextWindow
	}

	return tui.RunChat(a)
}

// loadAgentConfigForResume gets an agent config; if not in a boltcode project
// (app nil), still try the user-level config.
func loadAgentConfigForResume(app *command.App, session *agent.Session) (*agent.Config, error) {
	var cfg *agent.Config
	var err error
	if app != nil {
		cfg, err = app.LoadAgentConfig()
	} else {
		cfg, err = command.LoadOwnAgentConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("no API key configured — run 'boltcode' then 'config' first")
	}
	if session == nil || session.Provider == "" || strings.EqualFold(cfg.Provider, session.Provider) {
		return cfg, nil
	}
	if profiles, loadErr := agent.LoadProfiles(); loadErr == nil {
		for _, profile := range profiles.Profiles {
			if strings.EqualFold(profile.Provider, session.Provider) && profile.APIKey != "" {
				matched := profile.ToConfig()
				return &matched, nil
			}
		}
	}
	return nil, fmt.Errorf("session %s uses provider %q, but no matching local profile is configured", session.ID, session.Provider)
}
