package agent

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
)

// Usage holds token counts from an API response.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// SlashCommand is one entry in the slash-command table, for completion + help.
type SlashCommand struct {
	Name string // e.g. "/compact"
	Desc string
}

// SlashCommands is the full list, used by shell readline completion and the
// inline hint shown when the user types "/" alone.
var SlashCommands = []SlashCommand{
	{"/help", "show all commands"},
	{"/tokens", "show token usage"},
	{"/time", "show elapsed time"},
	{"/model", "show / switch model"},
	{"/profile", "list / switch provider profiles"},
	{"/diff", "show uncommitted changes"},
	{"/files", "list worktree files"},
	{"/clear", "clear conversation history"},
	{"/compact", "summarize old messages (LLM)"},
	{"/context", "show context-usage bar"},
	{"/retry", "retry the last failed API request"},
	{"/restrict", "re-enable command confirmation (undo auto-allow)"},
	{"/triage", "review codebase, prioritized items"},
	{"/sessions", "list saved conversations"},
	{"/exit", "end session"},
}

// handleSlashCommand processes /commands typed during the agent session.
// Returns true if the command was handled (loop should continue).
func (a *Agent) handleSlashCommand(input string) bool {
	switch {
	case input == "/":
		return a.slashCommandPicker()

	case input == "/exit":
		a.ShouldExit = true
		return true

	case input == "/help" || input == "/?":
		fmt.Println("\n" + ansiDarkGray + "  ─────────────────────────────────")
		fmt.Println("  Slash commands:")
		fmt.Println("    /help        this message")
		fmt.Println("    /tokens      show token usage")
		fmt.Println("    /time        show elapsed time")
		fmt.Println("    /model       show current model")
		fmt.Println("    /model <id>  switch model (saved to profile)")
		fmt.Println("    /profile     interactive picker — switch by number or create new")
		fmt.Println("    /profile <n> switch by name directly")
		fmt.Println("    /diff        show uncommitted changes")
		fmt.Println("    /files       list files in worktree")
		fmt.Println("    /clear       clear conversation history")
		fmt.Println("    /compact     summarize old messages (LLM) to free context")
		fmt.Println("    /context     show context-usage bar vs model window")
		fmt.Println("    /retry       retry the last failed API request")
		fmt.Println("    /restrict    re-enable command confirmation (undo auto-allow)")
		fmt.Println("    /triage [worktree|repo]  review codebase, give prioritized action items")
		fmt.Println("    /sessions    list saved conversations (resume with boltcode --resume <id>)")
		fmt.Println("    done/exit    end session")
		fmt.Println("  ─────────────────────────────────" + ansiReset)
		return true

	case input == "/tokens" || input == "/usage":
		fmt.Println("\n" + a.Stats.Display())
		return true

	case input == "/time":
		fmt.Printf("\n  "+ansiGray+"elapsed: %s"+ansiReset+"\n", formatDuration(a.Stats.Elapsed()))
		return true

	case input == "/model":
		return a.modelPicker()

	case len(input) > 7 && input[:7] == "/model ":
		newModel := input[7:]
		a.Config.Model = newModel
		// Persist to the active profile so the change survives restarts.
		if err := SetCurrentModel(newModel); err != nil {
			fmt.Printf("\n  "+ansiYellow+"⚠ saved for this session only: %v"+ansiReset+"\n", err)
		} else {
			fmt.Printf("\n  "+ansiGreen+"✓ switched to: %s (saved to profile)"+ansiReset+"\n", newModel)
		}
		return true

	case input == "/profile":
		return a.profilePicker()

	case len(input) > 9 && input[:9] == "/profile ":
		return a.switchProfile(input[9:])

	case input == "/diff":
		a.showDiff()
		return true

	case input == "/files":
		a.showFiles()
		return true

	case input == "/clear":
		a.Messages = a.Messages[:1]
		fmt.Println("\n  " + ansiGreen + "✓ conversation cleared" + ansiReset)
		return true

	case input == "/restrict":
		// Undo a prior "a/allow" so every command is confirmed again. Without
		// this, AutoAllow was permanent for the session — a risky state to be
		// stuck in. (Destructive commands are still confirmed even under
		// AutoAllow, but /restrict restores full confirmation for everything.)
		a.AutoAllow = false
		fmt.Println("\n  " + ansiGreen + "✓ command confirmation re-enabled" + ansiReset)
		return true

	case input == "/compact":
		// LLM-summarized compaction: older history becomes a summary, recent
		// messages kept verbatim. Falls back to hard truncation on error.
		if len(a.Messages) <= 8 {
			fmt.Println("\n  " + ansiGray + "context already small" + ansiReset)
			return true
		}
		fmt.Println("\n  " + ansiCyan + "compacting context (LLM summary)..." + ansiReset)
		if err := a.compactMessages(6); err != nil {
			fmt.Println("\n  " + ansiRed + "✗ " + err.Error() + ansiReset)
			return true
		}
		if a.Stats != nil {
			a.Stats.LastInput = estimateTokens(a.Messages)
		}
		fmt.Println("  " + ansiGreen + "✓ context compacted" + ansiReset)
		return true

	case input == "/triage", input == "/triage worktree":
		return a.runTriage("worktree")
	case input == "/triage repo":
		return a.runTriage("repo")
	case input == "/sessions":
		return a.showSessions()
	case len(input) > 17 && input[:17] == "/sessions delete ":
		id := strings.TrimSpace(input[17:])
		if !validSessionID(id) {
			fmt.Println("\n  " + ansiRed + "invalid session id: " + id + ansiReset)
			return true
		}
		if err := DeleteSession(id); err != nil {
			fmt.Println("\n  " + ansiRed + "✗ " + err.Error() + ansiReset)
			return true
		}
		fmt.Println("\n  " + ansiGreen + "✓ session " + id + " deleted" + ansiReset)
		return true
	case input == "/context":
		return a.showContext()
	}
	return false
}

// showContext displays the current context-usage bar and window size, so the
// user can see how close they are to the model's limit.
func (a *Agent) showContext() bool {
	window := modelContextWindow(a.Config.Model)
	used := a.currentContextTokens()
	fmt.Println()
	fmt.Printf("  %scontext usage:%s %s\n", ansiBold, ansiReset, a.Config.Model)
	fmt.Printf("  %s\n", contextBar(used, window))
	pct := int(float64(used) / float64(window) * 100)
	switch {
	case pct >= 90:
		fmt.Printf("  " + ansiRed + "⚠ near limit — run /compact to summarize history" + ansiReset + "\n")
	case pct >= 80:
		fmt.Printf("  " + ansiYellow + "auto-compact will trigger soon" + ansiReset + "\n")
	default:
		fmt.Printf("  " + ansiDarkGray + "healthy" + ansiReset + "\n")
	}
	return true
}

// showSessions lists recent saved conversations so the user can resume one
// from the shell with `boltcode --resume <id>`.
func (a *Agent) showSessions() bool {
	sessions, err := ListSessions()
	if err != nil || len(sessions) == 0 {
		fmt.Println("\n  " + ansiGray + "no saved sessions yet" + ansiReset)
		return true
	}
	fmt.Println()
	fmt.Println("  " + ansiBold + "recent sessions:" + ansiReset)
	for i, s := range sessions {
		if i >= 10 {
			fmt.Printf("  "+ansiDarkGray+"... and %d more\n"+ansiReset, len(sessions)-10)
			break
		}
		cur := "  "
		if s.ID == a.SessionID {
			cur = ansiGreen + "▶ " + ansiReset
		}
		preview := s.Intent
		if preview == "" {
			preview = firstUserText(s.Messages)
		}
		if preview == "" {
			preview = "(no summary)"
		}
		if len([]rune(preview)) > 40 {
			preview = string([]rune(preview)[:40]) + "…"
		}
		fmt.Printf("  %s%-10s %s  %s\n", cur, s.ID,
			ansiDarkGray+s.UpdatedAt.Format("01-02 15:04")+ansiReset, ansiGray+preview+ansiReset)
	}
	fmt.Println("\n  " + ansiDarkGray + "resume with: boltcode --resume <id>" + ansiReset)
	return true
}

// firstUserText extracts the first user message as a string, if possible.
func firstUserText(msgs []Message) string {
	for _, m := range msgs {
		if m.Role == "user" {
			if s, ok := m.Content.(string); ok {
				return s
			}
		}
	}
	return ""
}

// profilePicker is the interactive /profile menu: lists profiles numbered,
// marks the active one, and lets the user pick by number, create a new one,
// or cancel. Designed for ordinary users — no need to type profile names.
//
// Menu output and the selection read both go through PromptFn (readline-backed)
// so the readline line buffer stays in sync — plain fmt.Print before a Readline
// would desync readline and make it return empty immediately.
func (a *Agent) profilePicker() bool {
	if a.PromptFn == nil && a.InputFn == nil {
		return a.showProfiles()
	}
	pf, err := LoadProfiles()
	if err != nil || pf == nil {
		pf = &ProfileFile{}
	}

	for {
		// Build the full menu as one string, then read the choice in a single
		// PromptFn call — the shell side prints it via readline's writer and
		// reads one line, atomically.
		menu := "\n  " + ansiBold + "profiles:" + ansiReset + "\n"
		if len(pf.Profiles) == 0 {
			menu += "  " + ansiGray + "(none yet)" + ansiReset + "\n"
		}
		for i, p := range pf.Profiles {
			marker := "  "
			if p.Name == pf.Current {
				marker = ansiGreen + "▶ " + ansiReset
			}
			menu += fmt.Sprintf("  %s%d) %-16s %s / %s\n", marker, i+1, p.Name,
				ansiGray+p.Provider+ansiReset, ansiGray+p.Model+ansiReset)
		}
		menu += fmt.Sprintf("\n  %s[1-%d] switch  [n] new  [q] quit > %s",
			ansiDarkGray, len(pf.Profiles), ansiReset)
		choice := strings.TrimSpace(a.readWith(menu))
		switch strings.ToLower(choice) {
		case "", "q", "quit":
			return true
		case "n", "new":
			if a.createNewProfile() {
				pf, _ = LoadProfiles()
				if pf == nil {
					pf = &ProfileFile{}
				}
			}
			continue
		default:
			idx := 0
			for _, r := range choice {
				if r < '0' || r > '9' {
					idx = -1
					break
				}
				idx = idx*10 + int(r-'0')
			}
			if idx < 1 || idx > len(pf.Profiles) {
				fmt.Println("  " + ansiRed + "invalid choice" + ansiReset)
				continue
			}
			picked := pf.Profiles[idx-1]
			if _, err := SwitchProfile(picked.Name); err != nil {
				fmt.Printf("  "+ansiRed+"✗ %v"+ansiReset+"\n", err)
				continue
			}
			a.Config = picked.ToConfig()
			fmt.Printf("\n  "+ansiGreen+"✓ active profile: %s"+ansiReset+"\n", picked.Name)
			fmt.Printf("  "+ansiGray+"provider: %s, model: %s"+ansiReset+"\n", picked.Provider, picked.Model)
			return true
		}
	}
}

// slashCommandPicker shows an interactive numbered menu of all slash commands.
// The user selects by number or types the command name directly. Triggered when
// the user types "/" alone.
func (a *Agent) slashCommandPicker() bool {
	menu := "\n  " + ansiBold + "slash commands:" + ansiReset + "\n"
	for i, cmd := range SlashCommands {
		menu += fmt.Sprintf("  %s%d)%s %-20s %s%s%s\n",
			ansiDarkGray, i+1, ansiReset,
			cmd.Name,
			ansiGray, cmd.Desc, ansiReset)
	}
	menu += fmt.Sprintf("\n  %s[1-%d] select  [q] quit > %s",
		ansiDarkGray, len(SlashCommands), ansiReset)
	choice := strings.TrimSpace(a.readWith(menu))
	if choice == "" || choice == "q" || choice == "quit" {
		return true
	}
	// Numeric selection
	if n, err := parseNumber(choice); err == nil && n >= 1 && n <= len(SlashCommands) {
		selected := SlashCommands[n-1].Name
		return a.handleSlashCommand(selected)
	}
	// Direct command name
	if strings.HasPrefix(choice, "/") {
		return a.handleSlashCommand(choice)
	}
	fmt.Println("  " + ansiRed + "invalid choice" + ansiReset)
	return true
}

// modelPicker shows an interactive numbered menu of available models for the
// current provider. The user selects by number. Triggered when the user types
// "/model" without arguments.
func (a *Agent) modelPicker() bool {
	models := modelsForProvider(a.Config.Provider)
	if len(models) == 0 {
		// Unknown provider — fall back to free-text input.
		fmt.Printf("\n  "+ansiGray+"current model: %s"+ansiReset+"\n", a.Config.Model)
		fmt.Print("  " + ansiDarkGray + "enter model id (q=cancel) > " + ansiReset)
		input := strings.TrimSpace(a.readWith(""))
		if input == "" || input == "q" || input == "quit" {
			return true
		}
		a.Config.Model = input
		if err := SetCurrentModel(input); err == nil {
			fmt.Printf("  "+ansiGreen+"✓ switched to: %s"+ansiReset+"\n", input)
		}
		return true
	}

	menu := "\n  " + ansiBold + "models" + ansiReset + ansiGray + " (" + a.Config.Provider + ")" + ansiReset + ":\n"
	for i, m := range models {
		marker := "  "
		if m.ID == a.Config.Model {
			marker = ansiGreen + "▶ " + ansiReset
		}
		label := m.Label
		if label == "" {
			label = m.ID
		}
		menu += fmt.Sprintf("  %s%d) %-30s %s%s%s\n",
			marker, i+1, m.ID, ansiGray, label, ansiReset)
	}
	menu += fmt.Sprintf("\n  %s[1-%d] switch  [q] quit > %s",
		ansiDarkGray, len(models), ansiReset)

	choice := strings.TrimSpace(a.readWith(menu))
	if choice == "" || choice == "q" || choice == "quit" {
		return true
	}
	n, err := parseNumber(choice)
	if err != nil || n < 1 || n > len(models) {
		fmt.Println("  " + ansiRed + "invalid choice" + ansiReset)
		return true
	}
	selected := models[n-1].ID
	a.Config.Model = selected
	if err := SetCurrentModel(selected); err != nil {
		fmt.Printf("  "+ansiYellow+"⚠ saved for this session only: %v"+ansiReset+"\n", err)
	} else {
		fmt.Printf("  "+ansiGreen+"✓ switched to: %s"+ansiReset+"\n", selected)
	}
	return true
}

// parseNumber parses a positive integer from a string.
func parseNumber(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 0, fmt.Errorf("zero")
	}
	return n, nil
}

// readWith prints prompt (via PromptFn if set, else fmt.Print+InputFn) and
// reads one line. Centralizes the readline-sync handling.
func (a *Agent) readWith(prompt string) string {
	if a.PromptFn != nil {
		return a.PromptFn(prompt)
	}
	fmt.Print(prompt)
	return a.InputFn()
}

// createNewProfile collects a name + key fields inline and saves a new profile,
// activating it. A lightweight version of the shell config wizard, usable from
// within an agent session.
func (a *Agent) createNewProfile() bool {
	if a.PromptFn == nil && a.InputFn == nil {
		return false
	}
	name := strings.TrimSpace(a.readWith("\n  profile name (e.g. anthropic / 火山方舟): "))
	if name == "" {
		fmt.Println("  " + ansiGray + "cancelled" + ansiReset)
		return false
	}
	provider := a.readWith("  provider [anthropic]: ")
	if provider == "" {
		provider = "anthropic"
	}
	apiKey := strings.TrimSpace(a.readWith("  API Key: "))
	if apiKey == "" {
		fmt.Println("  " + ansiRed + "API Key required — cancelled" + ansiReset)
		return false
	}
	model := a.readWith("  model [" + DefaultModel + "]: ")
	if model == "" {
		model = DefaultModel
	}
	baseURL := strings.TrimSpace(a.readWith("  base URL (blank=default): "))

	cfg := &Config{Provider: provider, APIKey: apiKey, Model: model, BaseURL: baseURL}
	if err := SaveProfile(name, cfg); err != nil {
		fmt.Printf("  "+ansiRed+"✗ save failed: %v"+ansiReset+"\n", err)
		return false
	}
	a.Config = *cfg
	fmt.Printf("\n  "+ansiGreen+"✓ created + switched to: %s"+ansiReset+"\n", name)
	return true
}

// showProfiles lists all configured provider profiles, marking the active one.
// Non-interactive fallback used when InputFn isn't set.
func (a *Agent) showProfiles() bool {
	pf, err := LoadProfiles()
	if err != nil || pf == nil || len(pf.Profiles) == 0 {
		fmt.Println("\n  " + ansiGray + "no profiles configured — run 'config' in the shell" + ansiReset)
		return true
	}
	fmt.Println()
	fmt.Println("  " + ansiBold + "profiles:" + ansiReset)
	for _, p := range pf.Profiles {
		marker := "  "
		if p.Name == pf.Current {
			marker = ansiGreen + "▶ " + ansiReset
		}
		fmt.Printf("  %s%-16s %s / %s\n", marker, p.Name, ansiGray+p.Provider+ansiReset, ansiGray+p.Model+ansiReset)
	}
	fmt.Println("\n  " + ansiDarkGray + "switch with: /profile" + ansiReset)
	return true
}

// switchProfile activates a named profile and applies it to the running agent.
func (a *Agent) switchProfile(name string) bool {
	p, err := SwitchProfile(name)
	if err != nil {
		fmt.Printf("\n  "+ansiRed+"✗ %v"+ansiReset+"\n", err)
		return true
	}
	a.Config = p.ToConfig()
	fmt.Printf("\n  "+ansiGreen+"✓ active profile: %s"+ansiReset+"\n", p.Name)
	fmt.Printf("  "+ansiGray+"provider: %s, model: %s"+ansiReset+"\n", p.Provider, p.Model)
	return true
}

// showDiff runs git diff in the worktree and displays the result.
func (a *Agent) showDiff() {
	cmd := exec.Command("git", "diff", "--stat")
	cmd.Dir = a.Workdir
	out, _ := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if result == "" {
		// Also check untracked files
		cmd2 := exec.Command("git", "status", "--short")
		cmd2.Dir = a.Workdir
		out2, _ := cmd2.CombinedOutput()
		result = strings.TrimSpace(string(out2))
	}
	if result == "" {
		fmt.Println("\n  " + ansiGray + "no changes yet" + ansiReset)
	} else {
		fmt.Println("\n  " + ansiDarkGray + "─── changes ───" + ansiReset)
		for _, line := range strings.Split(result, "\n") {
			fmt.Println("  " + ansiGray + line + ansiReset)
		}
	}
}

// showFiles lists files in the worktree (Go-native, cross-platform).
func (a *Agent) showFiles() {
	var files []string
	root := a.Workdir
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".boltcode":
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		files = append(files, "./"+filepath.ToSlash(rel))
		return nil
	})
	if len(files) == 0 {
		fmt.Println("\n  " + ansiGray + "worktree is empty" + ansiReset)
	} else {
		fmt.Println("\n  " + ansiDarkGray + "─── files ───" + ansiReset)
		for _, line := range files {
			fmt.Println("  " + ansiGray + line + ansiReset)
		}
		fmt.Printf("  "+ansiDarkGray+"(%d files)"+ansiReset+"\n", len(files))
	}
}
