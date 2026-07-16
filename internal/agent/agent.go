// Package agent implements BoltCode's built-in AI coding agent. It calls the
// LLM API directly (Claude/OpenAI), executes tools (read/write/run), and
// streams output to the terminal. No external AI CLI tool needed.
package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// DefaultModel is the model used when a profile/config doesn't specify one.
// Centralized here so a model bump is a one-line change rather than a hunt
// across api.go, commands.go, and agent_config.go.
const DefaultModel = "claude-sonnet-4-20250514"

// Config holds the API configuration for the AI agent.
type Config struct {
	Provider      string `json:"provider"` // "anthropic" or "openai"
	APIKey        string `json:"api_key"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// Profile is one named, complete API configuration. Users can keep several
// (e.g. "anthropic", "火山方舟", "local") and switch between them with
// /profile <name>. The active profile is whatever Current points to.
type Profile struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	APIKey        string `json:"api_key"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// ToConfig materializes a profile into the Config the agent runtime uses.
func (p Profile) ToConfig() Config {
	return Config{
		Provider:      p.Provider,
		APIKey:        p.APIKey,
		Model:         p.Model,
		BaseURL:       p.BaseURL,
		ContextWindow: p.ContextWindow,
	}
}

// Agent is the built-in AI coding engine.
type Agent struct {
	Config    Config
	Workdir   string // the task worktree (or repo root in solo mode)
	RepoRoot  string // the main repository root — the /triage repo scope
	Intent    string // what the user wants done
	Messages  []Message
	Stats     *Stats
	AutoAllow bool                  // session-level "allow all commands" flag
	ConfirmFn func(cmd string) bool // ask user before running commands
	// InputFn reads a line of user input. Set by Run so slash commands that
	// need follow-up input (like /profile's interactive picker) can read it.
	InputFn func() string
	// PromptFn prints a multi-line prompt via the readline-backed writer (so it
	// stays in sync with the readline line buffer) and reads one line. Used by
	// interactive slash commands where plain fmt.Print would desync readline
	// and cause Readline to return immediately with empty input.
	PromptFn func(prompt string) string

	// Session persistence: when non-empty, the conversation auto-saves to this
	// id on exit and can be resumed. SessionCreatedAt preserves the original
	// start time across resume-of-resume.
	SessionID        string
	SessionCreatedAt *time.Time

	// TodoList is the agent's task plan for the current session, managed via
	// the todo_write tool. Structured frontends render snapshots as a checklist
	// while terminal mode keeps the compact footer summary.
	TodoList []TodoItem
	// ToolErrorCount tracks consecutive tool-failure rounds for self-repair. The
	// system prompt advertises "max 3 retries"; after reaching this limit, the
	// loop stops injecting the nudge and the AI's next response is passed back
	// to the user without another retry attempt. Reset on any error-free round
	// or when the user provides new input.
	ToolErrorCount int

	// ShouldExit is set by /exit (via the slash-command picker) to signal the
	// agent loop to terminate. The loop checks this at the top of its inner
	// input-wait cycle.
	ShouldExit bool

	// NoSpinner disables the animated spinner. Used by the TUI which has its
	// own status display.
	NoSpinner bool
	// UIMode tells the runtime that a structured/full-screen UI owns the
	// surrounding chrome. It is session-local so two agents cannot leak display
	// state into one another through package globals.
	UIMode bool
	// EventFn receives structured runtime events (tool start/result, phase
	// changes). Terminal-only callers can leave it nil and keep the classic
	// stdout rendering.
	EventFn func(Event)

	runtimeMu     sync.Mutex
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	cancelFn      context.CancelFunc
	interrupted   bool
}

// CancelRequest interrupts the currently active API request or long-running
// tool, without ending the whole conversation.
func (a *Agent) CancelRequest() {
	a.runtimeMu.Lock()
	cancel := a.cancelFn
	a.interrupted = true
	a.runtimeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Cancel ends the whole agent session and interrupts any active operation.
// It is safe to call more than once and from the Bubble Tea goroutine.
func (a *Agent) Cancel() {
	a.runtimeMu.Lock()
	sessionCancel := a.sessionCancel
	currentCancel := a.cancelFn
	a.runtimeMu.Unlock()
	if currentCancel != nil {
		currentCancel()
	}
	if sessionCancel != nil {
		sessionCancel()
	}
}

// Done is closed when the current session is cancelled. A nil channel means
// Run has not started yet and is intentionally ignored by select statements.
func (a *Agent) Done() <-chan struct{} {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.sessionCtx == nil {
		return nil
	}
	return a.sessionCtx.Done()
}

func (a *Agent) beginSession() context.Context {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	a.sessionCtx = ctx
	a.sessionCancel = cancel
	a.cancelFn = nil
	a.interrupted = false
	return ctx
}

func (a *Agent) endSession() {
	a.runtimeMu.Lock()
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	a.sessionCtx = nil
	a.sessionCancel = nil
	a.cancelFn = nil
	a.runtimeMu.Unlock()
}

func (a *Agent) context() context.Context {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.sessionCtx != nil {
		return a.sessionCtx
	}
	return context.Background()
}

func (a *Agent) setCurrentCancel(cancel context.CancelFunc) {
	a.runtimeMu.Lock()
	a.cancelFn = cancel
	a.runtimeMu.Unlock()
}

func (a *Agent) clearCurrentCancel() {
	a.runtimeMu.Lock()
	a.cancelFn = nil
	a.runtimeMu.Unlock()
}

func (a *Agent) consumeInterrupt() bool {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	interrupted := a.interrupted
	a.interrupted = false
	return interrupted
}

// TodoItem is one step in the agent's session task plan.
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}

// Message represents a conversation message.
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ToolCall represents a tool the AI wants to use.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult is what we send back after executing a tool.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
	// Diff carries a compact before→after preview for edit operations. It is
	// NOT sent to the AI (json:"-"); it's only used by printToolResult to give
	// the user immediate visual feedback of what changed.
	Diff string `json:"-"`
}
