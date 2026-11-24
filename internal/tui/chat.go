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
	"github.com/Stringadmin/BoltCode/internal/theme"
)

// ── Message types (tea.Msg) ─────────────────────────────────────────────────

type agentLineMsg struct{ text string }
type agentDoneMsg struct{}
type agentReadyMsg struct{}
type agentResultMsg struct{ err error }
type statusTickMsg struct{}
type confirmRequestMsg struct{ cmd string }
type agentEventMsg struct{ event agent.Event }

// Async popup loads. seq guards against stale results when the user closed the
// popup or opened another one while the load was in flight.
type filesScannedMsg struct {
	seq   int
	files []string
}
type sessionsListedMsg struct {
	seq      int
	sessions []agent.Session
	err      error
}

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
	// wrappedLines[i] is the wrapped rendering of lines[i]; appends wrap only
	// the new tail instead of re-wrapping the whole history on every line.
	lines        []string
	wrappedLines []string
	renderWidth  int

	// Render caches, invalidated at their mutation points, so spinner and
	// status ticks neither re-render panels nor re-split the viewport content.
	viewportContent string
	timelineCache   string
	timelineDirty   bool
	planCache       string
	planDirty       bool
	workspace       string // cached basename of the agent workdir

	// Status (read from agent.Stats)
	busy  bool
	model string
	mode  agent.Mode
	// busyTicksRunning tracks whether the spinner/status tick chains are
	// alive. They stop propagating while idle so an inactive chat renders no
	// frames, and are kicked again on the idle→busy transition.
	busyTicksRunning bool
	tokens           int
	inputTokens      int
	outputTokens     int
	contextTokens    int
	tools            int
	rounds           int
	// busy is true while the agent is generating or running tools. finished
	// prevents input after the agent goroutine has stopped. statusHint is a
	// short, transient explanation shown when a draft cannot be sent yet.
	finished       bool
	failed         bool
	streamDone     bool
	resultSeen     bool
	resultErr      error
	statusHint     string
	newBelow       bool // new output arrived while the user was scrolled up
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

	// Sent prompts can be recalled with ↑/↓ when the cursor is on the first
	// input row. historyIndex == len(promptHistory) means "not browsing".
	promptHistory []string
	historyIndex  int
	historyDraft  string

	// Popup
	popup    *popupState
	popupSeq int // invalidates in-flight async popup loads

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
	// Trim the default styling noise, then tint the prompt glyph and
	// placeholder so the input box matches the palette.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorPrimary)).Bold(true)
	ta.FocusedStyle.Placeholder = chatDimStyle
	// Enter submits; Ctrl+J / Alt+Enter insert a newline (shift+enter does not
	// reach most Windows terminals).
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	spin.Style = chatPromptStyle
	workspace := filepath.Base(filepath.Clean(a.Workdir))
	if workspace == "." || workspace == string(filepath.Separator) || workspace == "" {
		workspace = "workspace"
	}
	mode := a.EffectiveMode()
	card := welcomeCardStyle.Render(
		chatTitleStyle.Render("⚡ BoltCode Agent") + "\n" +
			chatDimStyle.Render(workspace+" · "+a.Config.Model+" · "+string(mode)) + "\n\n" +
			"Describe what you want to build, fix, or understand.",
	)
	welcome := strings.Split(card, "\n")
	welcome = append(welcome, chatDimStyle.Render("  / commands · Ctrl+J newline · Shift+Tab mode · Ctrl+P sessions · Ctrl+O tools · drag select/right-click copy · Ctrl+D exit"))
	if len(a.Messages) == 0 && strings.TrimSpace(a.Intent) != "" {
		welcome = append(welcome, "")
		welcome = append(welcome, userMessageLines(strings.TrimSpace(a.Intent))...)
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
		model:        a.Config.Model,
		mode:         a.EffectiveMode(),
		busy:         true,
		activity:     "Preparing project context",
		busySince:    time.Now(),
		workspace:    workspace,
		// Init() starts both tick chains.
		busyTicksRunning: true,
	}
	model.setPlanItems(a.TodoList)
	model.clearCompletedPlanForNewTurn()
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
		result, cmd := m.handleMouse(msg)
		m.syncNewBelow()
		return result, cmd

	case tea.KeyMsg:
		result, cmd := m.handleKey(msg)
		m.syncNewBelow()
		return result, cmd

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
		}
		if activity := activityFromAgentLine(msg.text); activity != "" {
			m.activity = activity
		}
		m.refreshViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		} else if m.ready {
			m.newBelow = true
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
			m.busyTicksRunning = false
			m.clearCompletedPlanForNewTurn()
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
		// The chain only propagates while busy: the spinner lives on the
		// activity row, which is hidden when idle, so ticking then would be
		// pure redraw churn.
		if m.busy && !m.finished {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
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
		// Stats only move while the agent works; let the chain die when idle.
		if m.busy && !m.finished {
			cmds = append(cmds, m.statusTick())
		}

	case confirmRequestMsg:
		m.confirmPopup(msg.cmd)
		cmds = append(cmds, m.waitForConfirm())

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		cmds = append(cmds, m.waitForAgentEvent())

	case filesScannedMsg:
		if msg.seq == m.popupSeq && m.popup != nil && m.popup.kind == "files" {
			if len(msg.files) == 0 {
				m.popup = nil
			} else {
				m.popup.title = "Files (@)"
				for _, file := range msg.files {
					m.popup.items = append(m.popup.items, popupItem{id: file, label: file})
				}
				m.popup.applyFilter(m.popup.filter)
			}
		}

	case sessionsListedMsg:
		if msg.seq == m.popupSeq && m.popup != nil && m.popup.kind == "sessions" {
			items := m.sessionPopupItems(msg.sessions)
			if msg.err != nil || len(items) == 0 {
				m.popup = nil
				if msg.err != nil {
					m.statusHint = "could not load saved sessions"
				} else {
					m.statusHint = "no saved sessions for this workspace"
				}
			} else {
				m.popup.subtitle = filepath.Clean(m.agent.Workdir)
				m.popup.items = items
				m.popup.applyFilter(m.popup.filter)
			}
		}
	}

	// Let the textarea handle its own messages (cursor blink) when no popup
	// is open. Agent/status messages are not input; feeding them to the
	// textarea is wasted work per message.
	if m.popup == nil {
		switch msg.(type) {
		case agentLineMsg, agentDoneMsg, agentReadyMsg, agentResultMsg,
			statusTickMsg, confirmRequestMsg, agentEventMsg, spinner.TickMsg,
			filesScannedMsg, sessionsListedMsg:
		default:
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	if m.newBelow && m.ready && m.viewport.AtBottom() {
		m.newBelow = false
	}
	// Key messages return early, so busy-entry points (sendUserMessage,
	// /retry) kick the tick chains themselves. This covers the rest, e.g. a
	// busy start reported through agent events.
	cmds = append(cmds, m.kickBusyTicks())
	return m, tea.Batch(cmds...)
}

// syncNewBelow clears the "new messages below" indicator once the user has
// caught up with the bottom. Key and mouse messages return early from Update,
// so they call this on their own path.
func (m *ChatModel) syncNewBelow() {
	if m.newBelow && m.ready && m.viewport.AtBottom() {
		m.newBelow = false
	}
}

// kickBusyTicks (re)starts the spinner and status tick chains on the idle→busy
// transition. It is idempotent: while the chains are alive it returns nil.
func (m *ChatModel) kickBusyTicks() tea.Cmd {
	if m.busy && !m.finished && !m.busyTicksRunning {
		m.busyTicksRunning = true
		return tea.Batch(m.spinner.Tick, m.statusTick())
	}
	if !m.busy || m.finished {
		m.busyTicksRunning = false
	}
	return nil
}

func (m *ChatModel) handleAgentEvent(event agent.Event) {
	wasAtBottom := !m.ready || m.viewport.AtBottom()
	m.recordToolEvent(event)
	switch event.Kind {
	case agent.EventPhase:
		m.activity = event.Activity
		m.activityDetail = event.Detail
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
		m.busyTicksRunning = false
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
	}
	m.refreshViewport()
	if wasAtBottom && m.ready {
		m.viewport.GotoBottom()
	} else if m.ready {
		m.newBelow = true
	}
}

func (m *ChatModel) finishAgent(err error) {
	if m.finished {
		return
	}
	m.busy = false
	m.busyTicksRunning = false
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
	} else {
		m.statusHint = "session ended"
		m.textarea.Placeholder = chatDonePlaceholder
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// userMessageLines renders one user prompt as a left-railed block — the same
// visual language as the agent's status and error boxes.
func userMessageLines(text string) []string {
	lines := []string{chatUserStyle.Render("  ╭─ ◆ you")}
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, chatDimStyle.Render("  │  ")+line)
	}
	return append(lines, chatDimStyle.Render("  ╰─"))
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
