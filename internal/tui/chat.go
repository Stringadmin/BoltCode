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

// ── Message types (tea.Msg) ─────────────────────────────────────────────────

type agentLineMsg struct{ text string }
type agentDoneMsg struct{}
type agentReadyMsg struct{}
type agentResultMsg struct{ err error }
type statusTickMsg struct{}
type confirmRequestMsg struct{ cmd string }
type agentEventMsg struct{ event agent.Event }

const (
	chatInputRows           = 2 // second row keeps Windows IME pre-edit text inside the border
	chatConversationGapRows = 1 // breathing room between message history and bottom chrome
	chatReadyPlaceholder    = "Type a message, / for commands, or @ for files..."
	chatBusyPlaceholder     = "Agent is working — draft here, send when ready..."
	chatDonePlaceholder     = "Session ended — press Ctrl+C to return"
	chatErrorPlaceholder    = "Agent stopped with an error — press Ctrl+C to return"
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
	mode          agent.Mode
	tokens        int
	inputTokens   int
	outputTokens  int
	contextTokens int
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
	planItems     []agent.TodoItem
	planDone      int
	planTotal     int
	planCurrent   string

	// Mouse selection is implemented inside the TUI because enabling mouse
	// tracking for viewport scrolling prevents the terminal from performing
	// its native drag selection and right-click copy behavior.
	selection chatSelection
	copyText  func(string) error

	// Popup
	popup *popupState

	switchSessionID string
	quitting        bool
}

type popupState struct {
	kind     string // "commands" | "models" | "modes" | "files" | "sessions" | "confirm"
	items    []popupItem
	filtered []int // indices into items after filtering
	selected int   // index into filtered
	title    string
	subtitle string
	filter   string // fuzzy filter query
	prompt   string // for confirm popup: the command being confirmed
	// Session browsing is opened independently of the textarea. Closing it
	// should restore a draft instead of clearing what the user was typing.
	preserveInput bool
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
		chatDimStyle.Render("  ╰─ / commands · Shift+Tab mode · Ctrl+P sessions · Ctrl+O tools · drag select/right-click copy · Ctrl+D exit"),
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
		mode:         a.EffectiveMode(),
		busy:         true,
		activity:     "Preparing project context",
		busySince:    time.Now(),
	}
	model.setPlanItems(a.TodoList)
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
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case clipboardResultMsg:
		if msg.err != nil {
			m.statusHint = "copy failed: " + msg.err.Error()
			return m, nil
		}
		m.clearSelection()
		m.statusHint = fmt.Sprintf("copied %d characters", msg.characters)
		return m, nil

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
			in, out, contextTokens, tools, rounds, _ := m.agent.Stats.Snapshot()
			m.inputTokens = in
			m.outputTokens = out
			m.contextTokens = contextTokens
			m.tools = tools
			m.rounds = rounds
		}
		m.model = m.agent.Config.Model
		m.mode = m.agent.EffectiveMode()
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

func normalizeAgentLine(line string) (clean string, drop bool) {
	clean = stripCR(line)
	drop = strings.TrimSpace(ansi.Strip(clean)) == "" && (strings.Contains(line, "\r") || strings.Contains(line, "\033[K"))
	return clean, drop
}

// stripCR removes carriage returns and the ANSI clear-line sequence so spinner
// artifacts don't register as content lines.
func stripCR(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\033[K", "")
	return s
}
