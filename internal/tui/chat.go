package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// ── Styles ──────────────────────────────────────────────────────────────────

var (
	chatUserStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	chatDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	chatGreenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	chatPromptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	chatTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	statusBarStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236"))
	inputBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	popupStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 2)
	popupSelStyle    = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("255")).Bold(true)
	popupNormalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
)

// ── Message types (tea.Msg) ─────────────────────────────────────────────────

type agentLineMsg struct{ text string }
type agentDoneMsg struct{}
type statusTickMsg struct{}
type confirmRequestMsg struct{ cmd string }

// ── ChatModel ───────────────────────────────────────────────────────────────

// ChatModel is the Bubble Tea model for the main agent chat session.
// Layout: message viewport (top, scrollable) + status bar + input box (bottom).
// The input box (a textarea) is always visible and editable — even while the
// agent is working.
type ChatModel struct {
	agent     *agent.Agent
	agentIn   chan string
	agentDone chan struct{}
	lineCh    chan string
	confirmCh chan bool // TUI → agent: command confirmation answer

	width  int
	height int

	viewport viewport.Model
	textarea textarea.Model
	ready    bool

	// Accumulated raw message log (ANSI-colored lines from the agent).
	lines []string

	// Status (read from agent.Stats)
	busy   bool
	model  string
	tokens int
	tools  int
	rounds int

	// Popup
	popup *popupState

	quitting bool
}

type popupState struct {
	kind     string // "commands" | "models" | "files" | "confirm"
	items    []popupItem
	filtered []int // indices into items after filtering
	selected int   // index into filtered
	title    string
	filter   string // fuzzy filter query
	prompt   string // for confirm popup: the command being confirmed
}

type popupItem struct {
	id    string
	label string
	desc  string
}

// ── Constructor ─────────────────────────────────────────────────────────────

func NewChat(a *agent.Agent) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message, / for commands, or @ for files..."
	ta.Prompt = "❯ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.Focus()
	// Trim the default styling noise.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()

	return &ChatModel{
		agent:     a,
		agentIn:   make(chan string, 16),
		agentDone: make(chan struct{}, 1),
		confirmCh: make(chan bool),
		textarea:  ta,
		model:     a.Config.Model,
	}
}

// ── Bubble Tea ──────────────────────────────────────────────────────────────

func (m *ChatModel) Init() tea.Cmd {
	return tea.Batch(m.waitForAgentLine(), m.statusTick(), m.waitForConfirm(), textarea.Blink)
}

func (m *ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentLineMsg:
		m.lines = append(m.lines, msg.text)
		m.refreshViewport()
		m.viewport.GotoBottom()
		cmds = append(cmds, m.waitForAgentLine())

	case agentDoneMsg:
		m.busy = false

	case statusTickMsg:
		if m.agent.Stats != nil {
			in, out, tools, rounds := m.agent.Stats.Snapshot()
			m.tokens = in + out
			m.tools = tools
			m.rounds = rounds
		}
		m.model = m.agent.Config.Model
		cmds = append(cmds, m.statusTick())

	case confirmRequestMsg:
		m.confirmPopup(msg.cmd)
		cmds = append(cmds, m.waitForConfirm())
	}

	// Let the textarea handle its own key events (editing) when no popup.
	if m.popup == nil {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// layout sizes the viewport and textarea to the current terminal dimensions.
func (m *ChatModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	inputH := 3  // textarea border (2) + 1 line
	statusH := 1 // status bar
	vpH := m.height - inputH - statusH
	if vpH < 3 {
		vpH = 3
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, vpH)
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpH
	}
	m.textarea.SetWidth(m.width - 2)
	m.refreshViewport()
}

// refreshViewport re-renders the message log into the viewport, word-wrapping
// to the current width.
func (m *ChatModel) refreshViewport() {
	if !m.ready {
		return
	}
	content := strings.Join(m.lines, "\n")
	m.viewport.SetContent(content)
}

func (m *ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.popup != nil {
		return m.handlePopupKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.textarea.Value() == "" {
			m.quitting = true
			return m, tea.Quit
		}
		m.textarea.Reset()
		return m, nil

	case tea.KeyCtrlD:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			return m.handleSlashCommand(text)
		}
		m.lines = append(m.lines, "")
		m.lines = append(m.lines, chatUserStyle.Render("  you › ")+text)
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.agentIn <- text
		m.busy = true
		m.textarea.Reset()
		return m, nil

	case tea.KeyPgUp:
		m.viewport.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		return m, nil

	default:
		// Let the textarea handle editing (arrows, backspace, runes, etc.)
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		val := m.textarea.Value()
		// Instant popup triggers.
		if val == "/" {
			return m.showCommandPopup()
		}
		// "@" opens the file picker when it's the last typed char.
		if strings.HasSuffix(val, "@") {
			return m.showFilePopup()
		}
		return m, cmd
	}
}

func (m *ChatModel) handleSlashCommand(text string) (tea.Model, tea.Cmd) {
	switch text {
	case "/":
		return m.showCommandPopup()
	case "/model":
		m.textarea.Reset()
		return m.showModelPopup()
	case "/exit", "/quit", "/q":
		m.quitting = true
		return m, tea.Quit
	case "/clear":
		m.lines = nil
		m.refreshViewport()
		m.textarea.Reset()
		return m, nil
	default:
		m.lines = append(m.lines, chatUserStyle.Render("  you › ")+text)
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.agentIn <- text
		m.busy = true
		m.textarea.Reset()
		return m, nil
	}
}

// ── Popup ───────────────────────────────────────────────────────────────────

func (m *ChatModel) showCommandPopup() (tea.Model, tea.Cmd) {
	items := make([]popupItem, 0, len(agent.SlashCommands))
	for _, sc := range agent.SlashCommands {
		items = append(items, popupItem{id: sc.Name, label: sc.Name, desc: sc.Desc})
	}
	m.popup = &popupState{kind: "commands", items: items, title: "Slash Commands"}
	m.popup.applyFilter("")
	return m, nil
}

func (m *ChatModel) showModelPopup() (tea.Model, tea.Cmd) {
	models := agent.ModelsForProvider(m.agent.Config.Provider)
	if len(models) == 0 {
		m.lines = append(m.lines, chatDimStyle.Render("  no model list for provider: "+m.agent.Config.Provider))
		m.refreshViewport()
		return m, nil
	}
	items := make([]popupItem, 0, len(models))
	for _, mo := range models {
		items = append(items, popupItem{id: mo.ID, label: mo.ID, desc: mo.Label})
	}
	m.popup = &popupState{kind: "models", items: items, title: "Models (" + m.agent.Config.Provider + ")"}
	m.popup.applyFilter("")
	return m, nil
}

// showFilePopup scans the working directory for files and opens a fuzzy picker.
func (m *ChatModel) showFilePopup() (tea.Model, tea.Cmd) {
	files := m.scanFiles()
	if len(files) == 0 {
		return m, nil
	}
	items := make([]popupItem, 0, len(files))
	for _, f := range files {
		items = append(items, popupItem{id: f, label: f})
	}
	m.popup = &popupState{kind: "files", items: items, title: "Files (@)"}
	m.popup.applyFilter("")
	return m, nil
}

// scanFiles walks the agent workdir collecting relative file paths (capped),
// skipping .git, node_modules, and .boltcode.
func (m *ChatModel) scanFiles() []string {
	var out []string
	const max = 2000
	root := m.agent.Workdir
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || n == ".boltcode" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= max {
			return filepath.SkipAll
		}
		rel, e := filepath.Rel(root, p)
		if e == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// ── Fuzzy filtering ─────────────────────────────────────────────────────────

// applyFilter recomputes the filtered index list for the given query using a
// subsequence match, and resets the selection.
func (p *popupState) applyFilter(query string) {
	p.filter = query
	p.filtered = p.filtered[:0]
	q := strings.ToLower(query)
	for i, it := range p.items {
		if q == "" || fuzzyMatch(strings.ToLower(it.label), q) || fuzzyMatch(strings.ToLower(it.desc), q) {
			p.filtered = append(p.filtered, i)
		}
	}
	p.selected = 0
}

// fuzzyMatch reports whether all runes of query appear in s in order
// (subsequence match). Both should already be lowercased.
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	qi := 0
	qr := []rune(query)
	for _, r := range s {
		if r == qr[qi] {
			qi++
			if qi == len(qr) {
				return true
			}
		}
	}
	return false
}

// currentSelection returns the popupItem currently highlighted, or false if
// the filtered list is empty.
func (p *popupState) currentSelection() (popupItem, bool) {
	if len(p.filtered) == 0 || p.selected < 0 || p.selected >= len(p.filtered) {
		return popupItem{}, false
	}
	return p.items[p.filtered[p.selected]], true
}

// confirmPopup shows a yes/no/allow confirmation for a shell command.
func (m *ChatModel) confirmPopup(cmd string) {
	m.popup = &popupState{
		kind:   "confirm",
		title:  "Run command?",
		prompt: cmd,
		items: []popupItem{
			{id: "yes", label: "Yes", desc: "run this command once"},
			{id: "allow", label: "Allow all", desc: "run all commands this session"},
			{id: "no", label: "No", desc: "skip this command"},
		},
	}
	m.popup.applyFilter("")
}

func (m *ChatModel) handlePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.popup
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		// A confirm popup can't be dismissed with Esc — that would leave the
		// agent goroutine blocked waiting for an answer. Treat Esc as "no".
		if p.kind == "confirm" {
			m.popup = nil
			m.confirmCh <- false
			return m, nil
		}
		m.popup = nil
		m.textarea.Reset()
		return m, nil

	case tea.KeyUp:
		if p.selected > 0 {
			p.selected--
		}
		return m, nil

	case tea.KeyDown:
		if p.selected < len(p.filtered)-1 {
			p.selected++
		}
		return m, nil

	case tea.KeyBackspace:
		if p.kind != "confirm" && len(p.filter) > 0 {
			r := []rune(p.filter)
			p.applyFilter(string(r[:len(r)-1]))
		}
		return m, nil

	case tea.KeyEnter:
		sel, ok := p.currentSelection()
		if !ok {
			return m, nil
		}
		kind := p.kind
		m.popup = nil
		m.textarea.Reset()
		return m.applyPopupSelection(kind, sel)

	case tea.KeyRunes, tea.KeySpace:
		if p.kind == "confirm" {
			return m, nil // confirm popup uses arrows only
		}
		p.applyFilter(p.filter + msg.String())
		return m, nil
	}
	return m, nil
}

// applyPopupSelection executes the action for a chosen popup item.
func (m *ChatModel) applyPopupSelection(kind string, sel popupItem) (tea.Model, tea.Cmd) {
	switch kind {
	case "models":
		m.agent.Config.Model = sel.id
		m.model = sel.id
		m.lines = append(m.lines, chatGreenStyle.Render("  ✓ switched to: ")+sel.id)
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case "commands":
		if sel.id == "/exit" {
			m.quitting = true
			return m, tea.Quit
		}
		return m.handleSlashCommand(sel.id)

	case "files":
		// Insert the "@path" reference into the textarea.
		cur := m.textarea.Value()
		if strings.HasSuffix(cur, "@") {
			cur = cur[:len(cur)-1]
		}
		m.textarea.SetValue(cur + "@" + sel.id + " ")
		return m, nil

	case "confirm":
		switch sel.id {
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

// ── Channel listeners ───────────────────────────────────────────────────────

// confirmReqCh carries command-confirmation requests from the agent goroutine
// to the TUI. Buffered so the agent's ConfirmFn doesn't block on send before
// the TUI's listener is ready.
var confirmReqCh chan string

func (m *ChatModel) waitForConfirm() tea.Cmd {
	return func() tea.Msg {
		cmd, ok := <-confirmReqCh
		if !ok {
			return nil
		}
		return confirmRequestMsg{cmd: cmd}
	}
}

func (m *ChatModel) waitForAgentLine() tea.Cmd {
	ch := m.lineCh
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentLineMsg{text: line}
	}
}

func (m *ChatModel) statusTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return statusTickMsg{}
	})
}

// ── View ────────────────────────────────────────────────────────────────────

func (m *ChatModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Starting BoltCode..."
	}

	if m.popup != nil {
		return m.renderPopupOverlay()
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		m.renderStatusBar(),
		inputBoxStyle.Render(m.textarea.View()),
	)
}

func (m *ChatModel) renderStatusBar() string {
	left := ""
	if m.busy {
		left = chatPromptStyle.Render("● working")
	} else {
		left = chatGreenStyle.Render("● ready")
	}
	mid := chatDimStyle.Render(fmt.Sprintf("%d tokens · %d tools · %d rounds", m.tokens, m.tools, m.rounds))
	right := chatDimStyle.Render(m.model)
	content := left + "  │  " + mid + "  │  " + right
	// Pad to width.
	w := lipgloss.Width(content)
	if w < m.width {
		content += strings.Repeat(" ", m.width-w)
	}
	return statusBarStyle.Width(m.width).Render(content)
}

func (m *ChatModel) renderPopupOverlay() string {
	p := m.popup
	var items []string
	items = append(items, chatTitleStyle.Render(p.title))

	// Confirm popup: show the command being confirmed.
	if p.kind == "confirm" {
		items = append(items, "")
		items = append(items, chatPromptStyle.Render("  "+p.prompt))
		items = append(items, "")
		for i, it := range p.items {
			if i == p.selected {
				items = append(items, popupSelStyle.Render("▶ "+it.label+"  "+it.desc))
			} else {
				items = append(items, popupNormalStyle.Render("  "+it.label)+chatDimStyle.Render("  "+it.desc))
			}
		}
		items = append(items, "")
		items = append(items, chatDimStyle.Render("↑↓ navigate · Enter select"))
		popup := popupStyle.Render(strings.Join(items, "\n"))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
	}

	// Filterable popup: show the live filter query.
	if p.filter != "" {
		items = append(items, chatDimStyle.Render("  filter: ")+p.filter)
	}
	items = append(items, "")

	if len(p.filtered) == 0 {
		items = append(items, chatDimStyle.Render("  (no matches)"))
	}
	// Cap visible rows so long lists don't overflow the screen.
	const maxRows = 12
	start := 0
	if p.selected >= maxRows {
		start = p.selected - maxRows + 1
	}
	end := start + maxRows
	if end > len(p.filtered) {
		end = len(p.filtered)
	}
	for vi := start; vi < end; vi++ {
		it := p.items[p.filtered[vi]]
		if vi == p.selected {
			items = append(items, popupSelStyle.Render("▶ "+it.label+"  "+it.desc))
		} else {
			items = append(items, popupNormalStyle.Render("  "+it.label)+chatDimStyle.Render("  "+it.desc))
		}
	}
	if len(p.filtered) > maxRows {
		items = append(items, chatDimStyle.Render(fmt.Sprintf("  … %d/%d", p.selected+1, len(p.filtered))))
	}
	items = append(items, "")
	items = append(items, chatDimStyle.Render("type to filter · ↑↓ navigate · Enter select · Esc cancel"))
	popup := popupStyle.Render(strings.Join(items, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

// ── Run ─────────────────────────────────────────────────────────────────────

// RunChat starts the Bubble Tea chat TUI. The agent runs in a goroutine with
// its stdout captured to a pipe; ANSI-colored lines are forwarded to the TUI
// via a channel and rendered in the scrollable viewport. Bubble Tea renders to
// the real stdout (via tea.WithOutput). The input box stays visible and
// editable at all times.
func RunChat(a *agent.Agent) error {
	agent.SetNoSpinner(true)
	agent.SetTUIMode(true)
	defer func() {
		agent.SetNoSpinner(false)
		agent.SetTUIMode(false)
	}()

	w, h, _ := term.GetSize(os.Stdout.Fd())

	model := NewChat(a)
	model.lineCh = make(chan string, 512)
	if w > 0 {
		model.width = w
	}
	if h > 0 {
		model.height = h
	}
	model.layout()

	// Wire command confirmation through the TUI: the agent's ConfirmFn sends
	// the command to the TUI (which pops a confirm dialog) and blocks on the
	// answer. Skipped when AutoAllow is already set.
	confirmReqCh = make(chan string)
	a.ConfirmFn = func(cmd string) bool {
		confirmReqCh <- cmd
		return <-model.confirmCh
	}

	realStdout := os.Stdout

	r, wPipe, err := os.Pipe()
	if err != nil {
		return err
	}
	os.Stdout = wPipe

	// Pipe reader: forward each agent output line (colors preserved) to the TUI.
	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 1<<20), 4<<20)
		for scanner.Scan() {
			line := scanner.Text()
			// Drop pure carriage-return / clear-line spinner artifacts.
			if strings.TrimSpace(stripCR(line)) == "" {
				continue
			}
			select {
			case model.lineCh <- line:
			default:
			}
		}
		close(model.lineCh)
	}()

	// Agent goroutine.
	go func() {
		inputFn := func() string {
			line, ok := <-model.agentIn
			if !ok {
				return "exit"
			}
			return line
		}
		_ = a.Run(inputFn)
		wPipe.Close()
	}()

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(realStdout))
	_, err = p.Run()

	os.Stdout = realStdout
	wPipe.Close()
	return err
}

// stripCR removes carriage returns and the ANSI clear-line sequence so spinner
// artifacts don't register as content lines.
func stripCR(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\033[K", "")
	return s
}
