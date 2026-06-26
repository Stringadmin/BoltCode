// Package agent implements BoltCode's built-in AI coding agent. It calls the
// LLM API directly (Claude/OpenAI), executes tools (read/write/run), and
// streams output to the terminal. No external AI CLI tool needed.
package agent

import (
	"encoding/json"
	"time"
)

// Config holds the API configuration for the AI agent.
type Config struct {
	Provider string `json:"provider"` // "anthropic" or "openai"
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
}

// Profile is one named, complete API configuration. Users can keep several
// (e.g. "anthropic", "火山方舟", "local") and switch between them with
// /profile <name>. The active profile is whatever Current points to.
type Profile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
}

// ToConfig materializes a profile into the Config the agent runtime uses.
func (p Profile) ToConfig() Config {
	return Config{
		Provider: p.Provider,
		APIKey:   p.APIKey,
		Model:    p.Model,
		BaseURL:  p.BaseURL,
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
	AutoAllow bool   // session-level "allow all commands" flag
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
	// the todo_write tool. Rendered in the footer so progress stays visible.
	TodoList []TodoItem
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
}
