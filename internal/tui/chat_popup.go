package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func (m *ChatModel) showCommandPopup() (tea.Model, tea.Cmd) {
	items := make([]popupItem, 0, len(agent.SlashCommands))
	for _, command := range agent.SlashCommands {
		items = append(items, popupItem{id: command.Name, label: command.Name, desc: command.Desc})
	}
	m.popup = &popupState{kind: "commands", items: items, title: "Slash Commands"}
	m.popup.applyFilter("")
	return m, nil
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
		m.textarea.Reset()
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
		m.textarea.Reset()
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
		m.lines = append(m.lines, chatGreenStyle.Render("  ✓ switched to: ")+selection.id)
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil
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
