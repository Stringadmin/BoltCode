package shell

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
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

	fmt.Println()
	fmt.Println(titleStyle.Render("  Recent sessions"))
	fmt.Println()
	for i, s := range sessions {
		if i >= 15 {
			fmt.Printf("  ... and %d more\n", len(sessions)-15)
			break
		}
		preview := s.Intent
		if preview == "" {
			preview = firstUserMessage(s.Messages)
		}
		if preview == "" {
			preview = "(no summary)"
		}
		preview = truncateRunes(preview, 48)
		fmt.Printf("  %s  %s  %s\n",
			infoStyle.Render(s.ID),
			ansiDarkGrayStyle(s.UpdatedAt.Format("2006-01-02 15:04")),
			preview)
	}
	fmt.Println()
	fmt.Print("  resume which? [id / Enter=latest / q=quit]: ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "q" || line == "quit" {
		return nil
	}
	if line == "" {
		return startResumedAgent(version, &sessions[0])
	}
	s, err := agent.LoadSession(line)
	if err != nil {
		return err
	}
	return startResumedAgent(version, s)
}

// startResumedAgent re-enters the agent loop with a saved session's messages.
func startResumedAgent(version string, s *agent.Session) error {
	// Load the agent config (API key etc.) — try the session's model's profile
	// context is gone, so just use the active profile.
	app, _ := command.Load(s.Workdir)
	cfg, err := loadAgentConfigForResume(app)
	if err != nil {
		return err
	}

	a := &agent.Agent{
		Config:            *cfg,
		Workdir:           s.Workdir,
		RepoRoot:          s.RepoRoot,
		Intent:            s.Intent,
		Messages:          s.Messages,
		SessionID:         s.ID,
		SessionCreatedAt:  &s.CreatedAt,
	}

	// If the saved model differs, honor it so resume uses what the session had.
	if s.Model != "" {
		a.Config.Model = s.Model
	}

	// Simple stdin/stdout input (no full readline shell needed for resume).
	inputFn := func() string {
		var line string
		fmt.Print("  ❯ ")
		buf := make([]byte, 4096)
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "exit"
		}
		line = strings.TrimRight(string(buf[:n]), "\r\n")
		return line
	}

	a.ConfirmFn = func(cmd string) bool {
		fmt.Printf("\n  \033[38;5;220m⚠ run:\033[0m %s\n", cmd)
		fmt.Print("  [y/n] > ")
		buf := make([]byte, 64)
		n, _ := os.Stdin.Read(buf)
		ans := strings.ToLower(strings.TrimSpace(string(buf[:n])))
		return ans != "n" && ans != "no"
	}

	return a.Run(inputFn)
}

// loadAgentConfigForResume gets an agent config; if not in a boltcode project
// (app nil), still try the user-level config.
func loadAgentConfigForResume(app *command.App) (*agent.Config, error) {
	if app != nil {
		return app.LoadAgentConfig()
	}
	// Fall back to loading the active profile directly.
	own, err := command.LoadOwnAgentConfig()
	if err == nil {
		return own, nil
	}
	return nil, fmt.Errorf("no API key configured — run 'boltcode' then 'config' first")
}

// firstUserMessage returns the first user message text, if extractable.
func firstUserMessage(msgs []agent.Message) string {
	for _, m := range msgs {
		if m.Role == "user" {
			if s, ok := m.Content.(string); ok {
				return s
			}
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// ansiDarkGrayStyle wraps s in a dark-gray color.
func ansiDarkGrayStyle(s string) string {
	return "\033[38;5;241m" + s + "\033[0m"
}
