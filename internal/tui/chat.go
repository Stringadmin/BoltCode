package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// ── Styles ──────────────────────────────────────────────────────────────────

var (
	chatUserStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	chatAssistantStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24"))
	chatDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	chatGreenStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	chatErrorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	chatPromptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	chatTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	chatHeaderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24")).Bold(true)
	statusBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236"))
	inputBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	popupStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 2)
	popupSelStyle      = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("255")).Bold(true)
	popupNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
)

// ── Message types (tea.Msg) ─────────────────────────────────────────────────

type agentLineMsg struct{ text string }
type agentDoneMsg struct{}
type agentReadyMsg struct{}
type agentResultMsg struct{ err error }
type statusTickMsg struct{}
type confirmRequestMsg struct{ cmd string }
type agentEventMsg struct{ event agent.Event }

const (
	chatInputRows        = 2 // second row keeps Windows IME pre-edit text inside the border
	chatReadyPlaceholder = "Type a message, / for commands, or @ for files..."
	chatBusyPlaceholder  = "Agent is working — draft here, send when ready..."
	chatDonePlaceholder  = "Session ended — press Ctrl+C to return"
	chatErrorPlaceholder = "Agent stopped with an error — press Ctrl+C to return"
)

// ── ChatModel ───────────────────────────────────────────────────────────────

// ChatModel is the Bubble Tea model for the main agent chat session.
// Layout: message viewport (top, scrollable) + status bar + input box (bottom).
// The input box stays editable while the agent is working so the user can
// draft the next message, but Enter is enabled only when the agent is actually
// blocked waiting for input. This keeps rendered messages and model turns in
// sync instead of silently queueing multiple prompts.
type ChatModel struct {
	agent        *agent.Agent
	agentIn      chan string
	readyCh      chan struct{}
	agentDone    chan struct{}
	resultCh     chan error
	lineCh       chan string
	confirmCh    chan bool // TUI → agent: command confirmation answer
	confirmReqCh chan string
	eventCh      chan agent.Event

	width  int
	height int

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model
	ready    bool

	// Accumulated raw message log (ANSI-colored lines from the agent).
	lines       []string
	renderedLog string
	renderWidth int
	logDirty    bool

	// Status (read from agent.Stats)
	busy          bool
	model         string
	tokens        int
	inputTokens   int
	outputTokens  int
	contextTokens int
	cost          float64
	tools         int
	rounds        int
	// busy is true while the agent is generating or running tools. finished
	// prevents input after the agent goroutine has stopped. statusHint is a
	// short, transient explanation shown when a draft cannot be sent yet.
	finished       bool
	failed         bool
	streamDone     bool
	resultSeen     bool
	resultErr      error
	statusHint     string
	activity       string
	activityDetail string
	lastToolError  bool
	recovering     bool
	lastError      string
	busySince      time.Time
	imeCommitAt    time.Time

	toolTimeline  []toolTimelineEntry
	toolsExpanded bool
	planDone      int
	planTotal     int
	planCurrent   string

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
	ta.Placeholder = chatBusyPlaceholder
	// Reserve two rows for Windows IME composition, but draw the prompt only
	// on the first row. textarea.Prompt is repeated for every visible row.
	ta.Prompt = ""
	ta.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "❯ "
		}
		return "  "
	})
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(chatInputRows)
	ta.MaxHeight = chatInputRows
	ta.Focus()
	// Trim the default styling noise.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = chatPromptStyle
	workspace := filepath.Base(filepath.Clean(a.Workdir))
	if workspace == "." || workspace == string(filepath.Separator) || workspace == "" {
		workspace = "workspace"
	}
	welcome := []string{
		chatTitleStyle.Render("  ╭─ ⚡ BoltCode Agent"),
		chatDimStyle.Render(fmt.Sprintf("  │  %s · %s", workspace, a.Config.Model)),
		chatDimStyle.Render("  │"),
		"  │  Describe what you want to build, fix, or understand.",
		chatDimStyle.Render("  ╰─ / commands · @ files · PgUp/PgDn scroll · Ctrl+C interrupt · Ctrl+D exit"),
	}
	if len(a.Messages) == 0 && strings.TrimSpace(a.Intent) != "" {
		welcome = append(welcome, "", chatUserStyle.Render("  ◆ you"))
		for _, line := range strings.Split(strings.TrimSpace(a.Intent), "\n") {
			welcome = append(welcome, "    "+line)
		}
	}
	if history := renderResumeHistory(a.Messages); len(history) > 0 {
		welcome = append(welcome, "", chatDimStyle.Render(fmt.Sprintf("  ── resumed · %d saved messages ──", len(a.Messages))), "")
		welcome = append(welcome, history...)
	}

	model := &ChatModel{
		agent:        a,
		agentIn:      make(chan string, 1),
		readyCh:      make(chan struct{}, 1),
		agentDone:    make(chan struct{}, 1),
		resultCh:     make(chan error, 1),
		confirmCh:    make(chan bool),
		confirmReqCh: make(chan string),
		eventCh:      make(chan agent.Event, 128),
		textarea:     ta,
		spinner:      spin,
		lines:        welcome,
		logDirty:     true,
		model:        a.Config.Model,
		busy:         true,
		activity:     "Preparing project context",
		busySince:    time.Now(),
	}
	for _, item := range a.TodoList {
		model.planTotal++
		if item.Status == "completed" {
			model.planDone++
		}
		if item.Status == "in_progress" && model.planCurrent == "" {
			model.planCurrent = item.Content
		}
	}
	return model
}

// ── Bubble Tea ──────────────────────────────────────────────────────────────

func (m *ChatModel) Init() tea.Cmd {
	return tea.Batch(m.waitForAgentLine(), m.waitForAgentReady(), m.waitForAgentResult(), m.waitForAgentEvent(), m.statusTick(), m.waitForConfirm(), m.spinner.Tick, textarea.Blink)
}

func (m *ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case tea.MouseMsg:
		// The viewport owns wheel events. Bubble Tea only emits MouseMsg after
		// the runtime enables mouse cell motion; don't pass wheel input to the
		// textarea or it will look like the input box is swallowing scrolling.
		if m.popup == nil && (msg.Type == tea.MouseWheelUp || msg.Type == tea.MouseWheelDown || msg.Type == tea.MouseWheelLeft || msg.Type == tea.MouseWheelRight) {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentLineMsg:
		wasAtBottom := !m.ready || m.viewport.AtBottom()
		if msg.text != "" || len(m.lines) == 0 || m.lines[len(m.lines)-1] != "" {
			m.lines = append(m.lines, msg.text)
			m.logDirty = true
		}
		if activity := activityFromAgentLine(msg.text); activity != "" {
			m.activity = activity
		}
		m.refreshViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, m.waitForAgentLine())

	case agentDoneMsg:
		m.streamDone = true
		if m.resultSeen {
			m.finishAgent(m.resultErr)
		} else {
			m.activity = "Finalizing session"
			m.refreshViewport()
		}

	case agentResultMsg:
		m.resultSeen = true
		m.resultErr = msg.err
		if m.streamDone {
			m.finishAgent(msg.err)
		}

	case agentReadyMsg:
		if !m.finished {
			m.busy = false
			if m.recovering {
				m.statusHint = "request failed · /retry or type a new message"
				m.activity = "Waiting for recovery"
				m.textarea.Placeholder = "Request failed — type /retry, or send a new instruction..."
			} else {
				m.statusHint = ""
				m.activity = ""
				m.textarea.Placeholder = chatReadyPlaceholder
			}
			m.activityDetail = ""
			m.lastToolError = false
			m.refreshViewport()
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, m.waitForAgentReady())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.busy && m.ready {
			wasAtBottom := m.viewport.AtBottom()
			m.refreshViewport()
			if wasAtBottom {
				m.viewport.GotoBottom()
			}
		}

	case statusTickMsg:
		if m.agent.Stats != nil {
			in, out, contextTokens, tools, rounds, cost := m.agent.Stats.Snapshot()
			m.inputTokens = in
			m.outputTokens = out
			m.contextTokens = contextTokens
			m.tokens = in + out
			m.tools = tools
			m.rounds = rounds
			m.cost = cost
		}
		m.model = m.agent.Config.Model
		cmds = append(cmds, m.statusTick())

	case confirmRequestMsg:
		m.confirmPopup(msg.cmd)
		cmds = append(cmds, m.waitForConfirm())

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		cmds = append(cmds, m.waitForAgentEvent())
	}

	// Let the textarea handle its own key events (editing) when no popup.
	if m.popup == nil {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *ChatModel) handleAgentEvent(event agent.Event) {
	wasAtBottom := !m.ready || m.viewport.AtBottom()
	m.recordToolEvent(event)
	switch event.Kind {
	case agent.EventPhase:
		m.activity = event.Activity
		m.activityDetail = ""
	case agent.EventToolStarted:
		m.activity = event.Activity
		m.activityDetail = event.Detail
		m.lastToolError = false
	case agent.EventToolFinished:
		m.activity = event.Activity
		m.activityDetail = event.Detail
		m.lastToolError = event.IsError
	case agent.EventError:
		m.recovering = true
		m.busy = false
		m.lastError = event.Result
		m.statusHint = "request failed · /retry or type a new message"
		m.activity = "Waiting for recovery"
		m.activityDetail = ""
		m.textarea.Placeholder = "Request failed — type /retry, or send a new instruction..."
		m.lines = append(m.lines,
			"",
			chatErrorStyle.Render("  ╭─ ✕ Request failed"),
			chatErrorStyle.Render("  │  ")+event.Result,
			chatDimStyle.Render("  ╰─ /retry tries the same request · a new message changes direction"),
		)
		m.logDirty = true
	}
	m.refreshViewport()
	if wasAtBottom && m.ready {
		m.viewport.GotoBottom()
	}
}

func (m *ChatModel) finishAgent(err error) {
	if m.finished {
		return
	}
	m.busy = false
	m.finished = true
	m.activity = ""
	if err != nil {
		m.failed = true
		m.statusHint = "agent stopped with an error"
		m.textarea.Placeholder = chatErrorPlaceholder
		m.lines = append(m.lines,
			"",
			chatErrorStyle.Render("  ╭─ ✕ Agent stopped"),
			chatErrorStyle.Render("  │  ")+err.Error(),
			chatDimStyle.Render("  ╰─ Check the provider/network settings, then restart the session."),
		)
		m.logDirty = true
	} else {
		m.statusHint = "session ended"
		m.textarea.Placeholder = chatDonePlaceholder
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// layout sizes the viewport and textarea to the current terminal dimensions.
func (m *ChatModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	inputH := chatInputRows + 2 // textarea rows + top/bottom border
	headerH := 1
	statusH := 2 // primary status + session metrics
	vpH := m.height - inputH - statusH - headerH
	if vpH < 3 {
		vpH = 3
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, vpH)
		m.viewport.MouseWheelEnabled = true
		m.viewport.MouseWheelDelta = 4
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
	wrapWidth := m.width - 3
	if m.logDirty || m.renderWidth != wrapWidth {
		m.renderedLog = wrapChatContent(strings.Join(m.lines, "\n"), wrapWidth)
		m.renderWidth = wrapWidth
		m.logDirty = false
	}
	content := m.renderedLog
	if timeline := m.renderToolTimeline(); timeline != "" {
		if content != "" {
			content += "\n\n"
		}
		content += timeline
	}
	if m.busy && !m.finished {
		if content != "" {
			content += "\n\n"
		}
		content += m.renderActivityLine()
	}
	m.viewport.SetContent(content)
}

func (m *ChatModel) renderActivityLine() string {
	activity := m.activity
	if activity == "" {
		activity = "Thinking"
	}
	elapsed := time.Since(m.busySince).Round(100 * time.Millisecond)
	activityStyle := chatPromptStyle
	if m.lastToolError {
		activityStyle = chatErrorStyle
	}
	line := chatDimStyle.Render("  ╰─ ") + m.spinner.View() + " " + activityStyle.Render(activity)
	if m.activityDetail != "" {
		line += chatDimStyle.Render(" · " + m.activityDetail)
	}
	return line + chatDimStyle.Render(" · "+elapsed.String())
}

func wrapChatContent(content string, width int) string {
	if width < 20 {
		return content
	}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		bodyWidth := width - indent
		if bodyWidth < 1 {
			bodyWidth = width
		}
		wrapped := strings.Split(ansi.WrapWc(strings.TrimLeft(line, " "), bodyWidth, "/\\"), "\n")
		for _, part := range wrapped {
			out = append(out, strings.Repeat(" ", indent)+strings.TrimLeft(part, " "))
		}
	}
	return strings.Join(out, "\n")
}

func activityFromAgentLine(line string) string {
	plain := strings.TrimSpace(ansi.Strip(line))
	switch {
	case strings.Contains(plain, "✦ assistant"):
		return "Writing response"
	case strings.Contains(plain, "📖") && strings.Contains(plain, "Read"):
		return "Reading files"
	case strings.Contains(plain, "📁") && strings.Contains(plain, "List"):
		return "Exploring files"
	case strings.Contains(plain, "🔍") && strings.Contains(plain, "Search"):
		return "Searching code"
	case strings.Contains(plain, "⚙") && strings.Contains(plain, "Run"):
		return "Running command"
	case strings.Contains(plain, "✏") && (strings.Contains(plain, "Edit") || strings.Contains(plain, "MultiEdit")):
		return "Editing files"
	case strings.Contains(plain, "📝") && strings.Contains(plain, "Write"):
		return "Writing files"
	case strings.HasPrefix(plain, "╰─"):
		return "Analyzing tool results"
	default:
		return ""
	}
}

func (m *ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.popup != nil {
		return m.handlePopupKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.textarea.Value() != "" {
			m.textarea.Reset()
			m.statusHint = "draft cleared"
			return m, nil
		}
		if m.busy && !m.finished {
			m.agent.CancelRequest()
			m.statusHint = "interrupt requested"
			m.activity = "Stopping current action"
			m.activityDetail = ""
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlD:
		m.agent.Cancel()
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlO:
		if len(m.toolTimeline) == 0 {
			m.statusHint = "no tool calls yet"
			return m, nil
		}
		m.toolsExpanded = !m.toolsExpanded
		if m.toolsExpanded {
			m.statusHint = "tool details expanded"
		} else {
			m.statusHint = "tool details collapsed"
		}
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case tea.KeyEnter:
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		// Some Windows terminals emit Enter immediately after an IME commits
		// candidate text. Consume that Enter once so choosing “你好” does not
		// accidentally submit the whole chat message.
		if !m.imeCommitAt.IsZero() && time.Since(m.imeCommitAt) < 250*time.Millisecond {
			m.imeCommitAt = time.Time{}
			m.statusHint = "IME text committed — press Enter again to send"
			return m, nil
		}
		if m.finished {
			m.statusHint = "session ended"
			return m, nil
		}
		if m.busy {
			m.statusHint = "draft held — wait for the current reply"
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			return m.handleSlashCommand(text)
		}
		m.sendUserMessage(text, true)
		return m, nil

	case tea.KeyPgUp:
		m.viewport.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && !msg.Paste && containsNonASCII(msg.Runes) {
			m.imeCommitAt = time.Now()
		} else {
			m.imeCommitAt = time.Time{}
		}
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

func containsNonASCII(runes []rune) bool {
	for _, r := range runes {
		if r > 127 {
			return true
		}
	}
	return false
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
		m.logDirty = true
		m.refreshViewport()
		m.textarea.Reset()
		return m, nil
	case "/retry":
		if !m.recovering {
			m.statusHint = "nothing to retry"
			m.textarea.Reset()
			return m, nil
		}
		return m.retryLastRequest()
	default:
		m.sendUserMessage(text, false)
		return m, nil
	}
}

func (m *ChatModel) retryLastRequest() (tea.Model, tea.Cmd) {
	select {
	case m.agentIn <- "/retry":
		m.recovering = false
		m.busy = true
		m.statusHint = "retrying last request"
		m.activity = "Retrying request"
		m.activityDetail = ""
		m.busySince = time.Now()
		m.textarea.Placeholder = chatBusyPlaceholder
		m.lines = append(m.lines, "", chatPromptStyle.Render("  ↻ retrying last request"))
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return m, nil
	default:
		m.statusHint = "retry channel is not ready"
		return m, nil
	}
}

// sendUserMessage atomically mirrors one accepted prompt in the viewport and
// in the one-slot agent channel. It is called only after the ready handshake,
// so a second Enter cannot create a visible-but-not-yet-consumed message.
func (m *ChatModel) sendUserMessage(text string, leadingBlank bool) bool {
	select {
	case m.agentIn <- text:
		if leadingBlank {
			m.lines = append(m.lines, "")
		}
		m.busy = true
		m.recovering = false
		m.lastError = ""
		m.statusHint = ""
		m.activity = "Thinking"
		m.activityDetail = ""
		m.lastToolError = false
		m.busySince = time.Now()
		m.textarea.Placeholder = chatBusyPlaceholder
		m.lines = append(m.lines, chatUserStyle.Render("  ◆ you"))
		for _, line := range strings.Split(text, "\n") {
			m.lines = append(m.lines, "    "+line)
		}
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return true
	default:
		m.busy = true
		m.statusHint = "draft held — input channel is not ready"
		return false
	}
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
		m.renderSessionHeader(),
		m.viewport.View(),
		m.renderStatusBar(),
		m.renderInputBox(),
	)
}

func (m *ChatModel) renderSessionHeader() string {
	state := "ready"
	if m.failed {
		state = "failed"
	} else if m.finished {
		state = "stopped"
	} else if m.recovering {
		state = "recovery"
	} else if m.busy {
		state = "working"
	}
	workspace := filepath.Base(filepath.Clean(m.agent.Workdir))
	if workspace == "" || workspace == "." {
		workspace = "workspace"
	}
	scroll := 100
	if m.ready && !m.viewport.AtBottom() {
		scroll = int(m.viewport.ScrollPercent() * 100)
	}
	content := fmt.Sprintf(" ⚡ BoltCode  ·  %s  ·  %s  ·  %s  ·  ↕ %d%% ", workspace, m.model, state, scroll)
	if ansi.StringWidth(content) > m.width {
		content = ansi.Truncate(content, m.width, "…")
	}
	return chatHeaderStyle.Width(m.width).Render(content)
}

func (m *ChatModel) renderInputBox() string {
	style := inputBoxStyle
	switch {
	case m.failed || m.recovering:
		style = style.BorderForeground(lipgloss.Color("196"))
	case m.busy:
		style = style.BorderForeground(lipgloss.Color("75"))
	default:
		style = style.BorderForeground(lipgloss.Color("78"))
	}
	return style.Render(m.textarea.View())
}

func (m *ChatModel) renderStatusBar() string {
	left := ""
	if m.failed {
		left = chatErrorStyle.Render("● failed")
	} else if m.finished {
		left = chatDimStyle.Render("● stopped")
	} else if m.recovering {
		left = chatErrorStyle.Render("● recover")
	} else if m.busy {
		activity := m.activity
		if activity == "" {
			activity = "Thinking"
		}
		left = m.spinner.View() + " " + chatPromptStyle.Render(activity)
		if m.activityDetail != "" {
			left += chatDimStyle.Render(" · " + m.activityDetail)
		}
	} else {
		left = chatGreenStyle.Render("● ready")
	}
	if m.statusHint != "" {
		left += chatDimStyle.Render(" · " + m.statusHint)
	}
	permission := "confirm"
	if m.agent.AutoAllow {
		permission = "auto-allow"
	}
	scroll := 100
	if m.ready && !m.viewport.AtBottom() {
		scroll = int(m.viewport.ScrollPercent() * 100)
	}
	right := chatDimStyle.Render(fmt.Sprintf("%s · %s · scroll %d%%", m.model, permission, scroll))
	primary := fitStatusLine(left+"  │  "+right, m.width)

	window := m.agent.ContextWindow()
	contextPct := 0
	if window > 0 {
		contextPct = int(float64(m.contextTokens) / float64(window) * 100)
	}
	plan := "plan —"
	if m.planTotal > 0 {
		plan = fmt.Sprintf("plan %d/%d", m.planDone, m.planTotal)
		if m.planCurrent != "" {
			plan += " · " + m.planCurrent
		}
	}
	secondary := fmt.Sprintf("context %d%% · %s/%s  │  %s  │  $%.4f  │  %d tools · %d rounds  │  %s/%s in/out  │  wheel/PgUp scroll · Ctrl+O details",
		contextPct, formatMetricTokens(m.contextTokens), formatMetricTokens(window), plan, m.cost, m.tools, m.rounds,
		formatMetricTokens(m.inputTokens), formatMetricTokens(m.outputTokens))
	secondary = fitStatusLine(chatDimStyle.Render(secondary), m.width)
	return lipgloss.JoinVertical(lipgloss.Left,
		statusBarStyle.Width(m.width).Render(primary),
		chatDimStyle.Background(lipgloss.Color("234")).Width(m.width).Render(secondary),
	)
}

func formatMetricTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	}
	if tokens >= 10_000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func fitStatusLine(content string, width int) string {
	if width > 0 && ansi.StringWidth(content) > width {
		content = ansi.Truncate(content, width, "…")
	}
	if pad := width - lipgloss.Width(content); pad > 0 {
		content += strings.Repeat(" ", pad)
	}
	return content
}

func normalizeAgentLine(line string) (clean string, drop bool) {
	clean = stripCR(line)
	drop = strings.TrimSpace(ansi.Strip(clean)) == "" && (strings.Contains(line, "\r") || strings.Contains(line, "\033[K"))
	return clean, drop
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

// stripCR removes carriage returns and the ANSI clear-line sequence so spinner
// artifacts don't register as content lines.
func stripCR(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\033[K", "")
	return s
}
