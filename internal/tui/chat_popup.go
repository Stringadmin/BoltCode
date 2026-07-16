package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func (m *ChatModel) showCommandPopup() (tea.Model, tea.Cmd) {
	items := make([]popupItem, 0, len(agent.SlashCommands)+1)
	items = append(items, popupItem{id: "/sessions", label: "/sessions", desc: "browse conversations in this workspace · Ctrl+P"})
	for _, command := range agent.SlashCommands {
		items = append(items, popupItem{id: command.Name, label: command.Name, desc: command.Desc})
	}
	m.popup = &popupState{kind: "commands", items: items, title: "Slash Commands"}
	m.popup.applyFilter("")
	return m, nil
}

func (m *ChatModel) showSessionPopup() (tea.Model, tea.Cmd) {
	sessions, err := agent.ListSessions()
	if err != nil {
		m.statusHint = "could not load saved sessions"
		return m, nil
	}

	items := make([]popupItem, 0, len(sessions))
	for _, session := range sessions {
		if !sameWorkdir(session.Workdir, m.agent.Workdir) {
			continue
		}
		preview := ansi.Truncate(sessionPreview(session), 52, "…")
		model := session.Model
		if model == "" {
			model = "unknown model"
		}
		desc := fmt.Sprintf("%s · %s · %d messages · %s",
			session.UpdatedAt.Format("01-02 15:04"), model, len(session.Messages), session.ID)
		if session.ID == m.agent.SessionID {
			desc += " · current"
		}
		items = append(items, popupItem{id: session.ID, label: preview, desc: desc})
	}
	if len(items) == 0 {
		m.statusHint = "no saved sessions for this workspace"
		return m, nil
	}

	m.popup = &popupState{
		kind:          "sessions",
		items:         items,
		title:         "Sessions",
		subtitle:      filepath.Clean(m.agent.Workdir),
		preserveInput: true,
	}
	m.popup.applyFilter("")
	return m, nil
}

func sameWorkdir(left, right string) bool {
	leftPath, leftErr := filepath.Abs(filepath.Clean(left))
	rightPath, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		leftPath = filepath.Clean(left)
		rightPath = filepath.Clean(right)
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func (m *ChatModel) showModelPopup() (tea.Model, tea.Cmd) {
	models := agent.AvailableModels(m.agent.Config)
	if len(models) == 0 {
		m.lines = append(m.lines, chatDimStyle.Render("  no model list for provider: "+m.agent.Config.Provider))
		m.logDirty = true
		m.refreshViewport()
		return m, nil
	}
	items := make([]popupItem, 0, len(models))
	for _, model := range models {
		items = append(items, popupItem{id: model.ID, label: model.ID, desc: model.Label})
	}
	m.popup = &popupState{kind: "models", items: items, title: "Models (" + m.agent.Config.Provider + ")"}
	m.popup.applyFilter("")
	return m, nil
}

func (m *ChatModel) showModePopup() (tea.Model, tea.Cmd) {
	items := make([]popupItem, 0, 3)
	for _, mode := range []agent.Mode{agent.ModeBuild, agent.ModePlan, agent.ModeAsk} {
		desc := agent.ModeDescription(mode)
		if mode == m.mode {
			desc += " · current"
		}
		items = append(items, popupItem{id: string(mode), label: strings.ToUpper(string(mode)), desc: desc})
	}
	m.popup = &popupState{kind: "modes", items: items, title: "Agent Mode"}
	m.popup.applyFilter("")
	return m, nil
}

func (m *ChatModel) showFilePopup() (tea.Model, tea.Cmd) {
	files := m.scanFiles()
	if len(files) == 0 {
		return m, nil
	}
	items := make([]popupItem, 0, len(files))
	for _, file := range files {
		items = append(items, popupItem{id: file, label: file})
	}
	m.popup = &popupState{kind: "files", items: items, title: "Files (@)"}
	m.popup.applyFilter("")
	return m, nil
}

func (m *ChatModel) scanFiles() []string {
	var files []string
	const maxFiles = 2000
	root := m.agent.Workdir
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", ".boltcode":
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}
		if relative, relErr := filepath.Rel(root, path); relErr == nil {
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	return files
}

func (p *popupState) applyFilter(query string) {
	p.filter = query
	p.filtered = p.filtered[:0]
	lowerQuery := strings.ToLower(query)
	for index, item := range p.items {
		if lowerQuery == "" || fuzzyMatch(strings.ToLower(item.label), lowerQuery) || fuzzyMatch(strings.ToLower(item.desc), lowerQuery) {
			p.filtered = append(p.filtered, index)
		}
	}
	p.selected = 0
}

func fuzzyMatch(value, query string) bool {
	if query == "" {
		return true
	}
	queryRunes := []rune(query)
	queryIndex := 0
	for _, char := range value {
		if char == queryRunes[queryIndex] {
			queryIndex++
			if queryIndex == len(queryRunes) {
				return true
			}
		}
	}
	return false
}

func (p *popupState) currentSelection() (popupItem, bool) {
	if len(p.filtered) == 0 || p.selected < 0 || p.selected >= len(p.filtered) {
		return popupItem{}, false
	}
	return p.items[p.filtered[p.selected]], true
}

func (m *ChatModel) confirmPopup(command string) {
	m.popup = &popupState{
		kind:   "confirm",
		title:  "Run command?",
		prompt: command,
		items: []popupItem{
			{id: "yes", label: "Yes", desc: "run this command once"},
			{id: "allow", label: "Allow all", desc: "run all commands this session"},
			{id: "no", label: "No", desc: "skip this command"},
		},
	}
	m.popup.applyFilter("")
}

func (m *ChatModel) handlePopupKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	popup := m.popup
	switch message.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.popup = nil
		if popup.kind == "confirm" {
			m.confirmCh <- false
			return m, nil
		}
		if !popup.preserveInput {
			m.textarea.Reset()
		}
		return m, nil
	case tea.KeyCtrlP:
		if popup.kind == "sessions" {
			m.popup = nil
		}
		return m, nil
	case tea.KeyUp:
		if popup.selected > 0 {
			popup.selected--
		}
		return m, nil
	case tea.KeyDown:
		if popup.selected < len(popup.filtered)-1 {
			popup.selected++
		}
		return m, nil
	case tea.KeyBackspace:
		if popup.kind != "confirm" && len(popup.filter) > 0 {
			runes := []rune(popup.filter)
			popup.applyFilter(string(runes[:len(runes)-1]))
		}
		return m, nil
	case tea.KeyEnter:
		selection, ok := popup.currentSelection()
		if !ok {
			return m, nil
		}
		kind := popup.kind
		m.popup = nil
		if !popup.preserveInput {
			m.textarea.Reset()
		}
		return m.applyPopupSelection(kind, selection)
	case tea.KeyRunes, tea.KeySpace:
		if popup.kind != "confirm" {
			popup.applyFilter(popup.filter + message.String())
		}
		return m, nil
	}
	return m, nil
}

func (m *ChatModel) applyPopupSelection(kind string, selection popupItem) (tea.Model, tea.Cmd) {
	switch kind {
	case "models":
		m.agent.Config.Model = selection.id
		m.model = selection.id
		if len(m.lines) > 0 && m.lines[len(m.lines)-1] != "" {
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, chatGreenStyle.Render("  ✓ switched to: ")+selection.id)
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil
	case "modes":
		mode, err := agent.ParseMode(selection.id)
		if err != nil {
			m.statusHint = err.Error()
			return m, nil
		}
		return m.setChatMode(mode)
	case "commands":
		if selection.id == "/exit" {
			m.quitting = true
			return m, tea.Quit
		}
		return m.handleSlashCommand(selection.id)
	case "files":
		current := m.textarea.Value()
		if strings.HasSuffix(current, "@") {
			current = current[:len(current)-1]
		}
		m.textarea.SetValue(current + "@" + selection.id + " ")
		return m, nil
	case "sessions":
		if selection.id == m.agent.SessionID {
			m.statusHint = "already viewing this session"
			return m, nil
		}
		m.switchSessionID = selection.id
		m.agent.Cancel()
		m.quitting = true
		return m, tea.Quit
	case "confirm":
		switch selection.id {
		case "yes":
			m.confirmCh <- true
		case "allow":
			m.agent.AutoAllow = true
			m.confirmCh <- true
		default:
			m.confirmCh <- false
		}
		return m, nil
	}
	return m, nil
}

func (m *ChatModel) setChatMode(mode agent.Mode) (tea.Model, tea.Cmd) {
	m.agent.SetMode(mode)
	m.mode = m.agent.EffectiveMode()
	m.statusHint = "mode changed to " + string(m.mode)
	if len(m.lines) > 0 && m.lines[len(m.lines)-1] != "" {
		m.lines = append(m.lines, "")
	}
	m.lines = append(m.lines,
		chatPromptStyle.Render("  ◇ mode: ")+strings.ToUpper(string(m.mode))+chatDimStyle.Render(" · "+agent.ModeDescription(m.mode)),
	)
	m.logDirty = true
	m.refreshViewport()
	m.viewport.GotoBottom()
	m.textarea.Reset()
	return m, nil
}
