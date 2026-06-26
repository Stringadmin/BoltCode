package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const defaultAnthropicURL = "https://api.anthropic.com/v1/messages"

const codeSystemPrompt = `You are BoltCode Agent, a senior full-stack engineer with deep expertise in frontend, backend, security, and architecture. Your code will be reviewed by humans before merging into a team project.

## Core Identity
You are NOT a chatbot. You are an expert engineer who:
- Writes production-ready, review-ready code
- Has pixel-level attention to frontend detail
- Understands full backend request lifecycle
- Thinks about security at every layer
- Maintains precise mental models of architecture

## Self-Learning (CRITICAL)
You MUST actively build project knowledge using update_memory:
- First session: map out the entire project (language, framework, architecture, file purposes)
- Every session: update memory when you discover new patterns, conventions, or decisions
- Call update_memory for: language, framework, build_cmd, test_cmd, architecture, file:<path>
- This is NOT optional. A good engineer remembers the codebase.

## Frontend Standards (Pixel-Level Precision)
- CSS must be exact: use specific px/rem values, not approximate
- Responsive: test breakpoints (mobile 375px, tablet 768px, desktop 1024px+)
- Spacing system: maintain consistent spacing scale (4px/8px/12px/16px/24px/32px)
- Typography: explicit font-size, line-height, letter-spacing, font-weight
- Colors: use design tokens/variables, never hardcode hex in components
- Layout: prefer CSS Grid/Flexbox, avoid absolute positioning unless necessary
- Animations: use transform/opacity for performance, specify duration/easing
- Accessibility: semantic HTML, ARIA labels, keyboard navigation, contrast ratios
- After writing CSS: verify by reading back and checking values are intentional

## Backend Standards (Clear Request Lifecycle)
- Route → Middleware → Validation → Handler → Service → Repository → Response
- Every endpoint must have: input validation, error handling, proper HTTP status codes
- Database: use parameterized queries, handle connection pooling, migrations
- API design: RESTful conventions, consistent response format, pagination
- Dev server: know the port, hot-reload config, proxy settings, env variables
- Logging: structured logs at appropriate levels (debug/info/warn/error)
- After writing an endpoint: trace the full request path mentally, verify each step

## Security Awareness (Every Layer)
- Input: validate and sanitize ALL user input (XSS, SQL injection, command injection)
- Auth: never store plaintext passwords, use bcrypt/argon2, implement proper sessions/JWT
- Secrets: NEVER hardcode API keys, passwords, tokens. Use env vars or secret managers
- Headers: set security headers (CORS, CSP, X-Frame-Options, HSTS)
- Dependencies: be aware of known vulnerabilities in packages
- File uploads: validate type, size, sanitize filenames
- Rate limiting: protect sensitive endpoints
- After writing security-sensitive code: review it as an attacker would

## Architecture Cognition (Precise Mental Model)
- Understand the dependency graph: what imports what, what calls what
- Separation of concerns: UI / Business Logic / Data Access / Infrastructure
- Design patterns: use appropriate patterns (Repository, Factory, Observer, etc.)
- Module boundaries: respect existing boundaries, don't create circular deps
- State management: understand where state lives and how it flows
- Error propagation: errors should bubble up with context, not be swallowed
- After making architectural decisions: document them with update_memory

## Workflow
1. START: scan project, load memory, understand what exists
2. PLAN: propose approach, wait for user confirmation
3. IMPLEMENT: small steps, verify after each change
4. VERIFY: build → test → manual check if needed
5. REPAIR: if anything fails, analyze → fix → verify (max 3 attempts)
6. LEARN: update_memory with new knowledge gained

## Task Planning (todo_write)
For any multi-step task (3+ steps), create a todo list FIRST with todo_write, marking the first step in_progress. Update statuses as you complete each step. This keeps complex work on track — never lose the thread.
- Break work into concrete, verifiable steps
- Only ONE item in_progress at a time
- Mark completed immediately after finishing each step

## Editing Files
- Use multi_edit (not repeated edit_file) when modifying the same file several times — it's atomic: either all edits apply or none do
- old_string must be UNIQUE in the file; include surrounding context to disambiguate

## Verification Rules
- After ANY file change: run build command immediately
- After adding a feature: run tests
- After CSS changes: mentally verify spacing/sizing values are intentional
- After API changes: trace the full request lifecycle
- After security-related changes: review as an attacker
- If build fails: read error → fix → rebuild (max 3 retries)

## Communication
- Respond in the user's language
- Use numbered options for choices: 1) 2) 3)
- Be concise but precise
- Personality: you're a friendly senior engineer, not a robot. Use kaomoji occasionally to express emotion:
  - Greeting/happy: (◕‿◕) ╰(*°▽°*)╯ (✧ω✧)
  - Thinking/working: (•_•)و ᕙ(⇀‸↼)ᕗ
  - Success: (ﾉ◕ヮ◕)ﾉ*:✧ ٩(◕‿◕｡)۶
  - Error/oops: (╥﹏╥) (>_<)
  - Don't overdo it — one per message max, at natural points
- After completing work: summarize files changed and what was done

## Context
- Task: %s
- Working directory: %s
- Your code will be reviewed by teammates. Write accordingly.`

// Run starts the agent loop with all features: spinner, stats, slash commands.
// If a.Messages is pre-populated (a resumed session), the initial project-scan
// greeting is skipped and the conversation continues from where it left off.
func (a *Agent) Run(inputFn func() string) error {
	system := fmt.Sprintf(codeSystemPrompt, a.Intent, a.Workdir)
	a.Stats = NewStats()
	a.InputFn = inputFn // expose to slash commands needing follow-up input

	resuming := len(a.Messages) > 0
	if !resuming {
		// Record the session start time so resume-of-resume keeps the original.
		now := time.Now()
		a.SessionCreatedAt = &now

		// Load persistent memory (project understanding from past sessions)
		mem := a.LoadMemory()
		memCtx := mem.Context()

		// Project scan: gather context for the AI
		projectCtx := a.scanProject()

		// Build initial context message
		var initParts []string
		if memCtx != "" {
			initParts = append(initParts, "## Memory (from past sessions)\n"+memCtx)
		}
		if projectCtx != "" {
			initParts = append(initParts, "## Current Project\n"+projectCtx)
		}
		initMsg := "Hi."
		if len(initParts) > 0 {
			initMsg = "Hi. Here is the project context:\n\n" + strings.Join(initParts, "\n\n") + "\n\nPlease greet me briefly and ask what I need."
		}

		a.Messages = []Message{
			{Role: "user", Content: initMsg},
		}
	} else {
		fmt.Println()
		fmt.Printf("  "+ansiDarkGray+"↻ resumed session"+ansiReset+" %s"+ansiDarkGray+" · %d messages · continue where you left off"+ansiReset+"\n\n",
			a.SessionID, len(a.Messages))
	}

	for round := 0; round < 100; round++ {
		// Show spinner while waiting for API
		spin := NewSpinner("thinking...")
		spin.Start()

		resp, usage, err := a.callAPI(system)

		spin.Stop()

		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		// Track token usage. LastInput (from the most recent API response) is
		// the authoritative current context size.
		a.Stats.Add(usage.InputTokens, usage.OutputTokens)

		toolCalls := a.processResponse(resp)

		if len(toolCalls) == 0 {
			// AI finished speaking — auto-compact if context is near the window,
			// then show a session footer with stats + a context-usage bar.
			window := modelContextWindow(a.Config.Model)
			if shouldCompact(a.Stats.LastInput, window) {
				a.autoCompact()
			}

			fmt.Println()
			fmt.Println(divider())
			fmt.Printf("  "+ansiDarkGray+"⏱ %-8s  ◆ %d tokens  🔧 %d tools  🔄 %d rounds"+ansiReset+"\n",
				formatDuration(a.Stats.Elapsed()),
				a.Stats.InputTokens+a.Stats.OutputTokens,
				a.Stats.ToolCalls, a.Stats.Rounds)
			fmt.Printf("  %s\n", contextBar(a.currentContextTokens(), window))
			if todoLine := a.todoProgressLine(); todoLine != "" {
				fmt.Printf("  %s\n", todoLine)
			}
			input := inputFn()

			// Empty input = just pressed Enter, keep waiting
			if input == "" {
				continue
			}

			// Handle slash commands. Anything starting with "/" is a command —
			// an unknown one must NOT be sent to the AI as a prompt (that was a
			// bug: /typo got answered by the model). Show a hint instead.
			if strings.HasPrefix(input, "/") {
				if a.handleSlashCommand(input) {
					continue
				}
				fmt.Println("  " + ansiYellow + "unknown command: " + input + ansiReset)
				fmt.Println("  " + ansiDarkGray + "type /help for the command list" + ansiReset)
				continue
			}

			if input == "exit" || input == "done" || input == "quit" {
				break
			}
			a.Messages = append(a.Messages, Message{Role: "user", Content: input})
			continue
		}

		// Execute tools with self-repair logic
		var results []map[string]interface{}
		hasError := false
		for _, tc := range toolCalls {
			a.Stats.AddToolCall()
			a.printToolUse(tc)
			start := time.Now()
			result := a.ExecTool(tc)
			elapsed := time.Since(start)
			a.printToolResult(tc, result, elapsed)
			if result.IsError {
				hasError = true
			}
			results = append(results, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": result.ToolUseID,
				"content":     result.Content,
				"is_error":    result.IsError,
			})
		}
		a.Messages = append(a.Messages, Message{Role: "user", Content: results})

		// Self-repair: if tools failed, inject a nudge for the AI to fix it
		if hasError {
			a.Messages = append(a.Messages, Message{
				Role:    "user",
				Content: "[System] One or more tool calls failed. Please analyze the error above and fix it. If a build/test failed, read the error message carefully and correct your code. You have up to 3 attempts.",
			})
		}
	}

	// Show session stats and end animation
	fmt.Println()
	fmt.Println(a.Stats.Display())
	AnimateSessionEnd()

	// Auto-save the conversation so it can be resumed, Claude-Code style.
	// Skip saving if the session was empty (just opened and exited).
	if len(a.Messages) > 1 {
		if id, err := SaveSession(a); err == nil {
			fmt.Println()
			fmt.Printf("  "+ansiDarkGray+"✓ session saved — resume with:"+ansiReset+" boltcode --resume %s\n", id)
		}
	}
	return nil
}

// currentContextTokens returns the best estimate of current context size: the
// last API response's input_tokens if available, else a heuristic estimate.
func (a *Agent) currentContextTokens() int {
	if a.Stats != nil && a.Stats.LastInput > 0 {
		return a.Stats.LastInput
	}
	return estimateTokens(a.Messages)
}

// todoProgressLine renders a one-line summary of the session task plan, e.g.
// "📋 2/5 · ▸ implement auth · ◦ write tests". Empty if no plan is set.
func (a *Agent) todoProgressLine() string {
	if len(a.TodoList) == 0 {
		return ""
	}
	done := 0
	current := ""
	for _, t := range a.TodoList {
		if t.Status == "completed" {
			done++
		}
		if t.Status == "in_progress" && current == "" {
			current = truncate(t.Content, 32)
		}
	}
	out := fmt.Sprintf("%s📋 %d/%d%s", ansiDarkGray, done, len(a.TodoList), ansiReset)
	if current != "" {
		out += ansiDarkGray + " · " + ansiReset + ansiYellow + "▸ " + current + ansiReset
	}
	return out
}

// autoCompact summarizes the older conversation history to reclaim context.
// Announces itself so the user sees why the conversation just got shorter.
func (a *Agent) autoCompact() {
	window := modelContextWindow(a.Config.Model)
	fmt.Println()
	fmt.Printf("  "+ansiYellow+"⚡ context at %d%% — compacting..."+ansiReset+"\n",
		int(float64(a.Stats.LastInput)/float64(window)*100))
	if err := a.compactMessages(6); err != nil {
		fmt.Printf("  "+ansiRed+"✗ compaction failed: %v"+ansiReset+"\n", err)
		return
	}
	// Reset LastInput to the estimate of the now-smaller history so the bar
	// reflects reality until the next API call refreshes it.
	a.Stats.LastInput = estimateTokens(a.Messages)
	fmt.Printf("  "+ansiGreen+"✓ compacted — context now ~%s tokens"+ansiReset+"\n",
		formatTokens(a.Stats.LastInput))
}

// processResponse parses the API response content blocks and extracts tool calls.
func (a *Agent) processResponse(resp []byte) []ToolCall {
	var blocks []map[string]interface{}
	if err := json.Unmarshal(resp, &blocks); err != nil {
		fmt.Printf("  "+ansiDarkGray+"warning: could not parse response blocks: %v"+ansiReset+"\n", err)
		return nil
	}

	var content []interface{}
	var toolCalls []ToolCall

	for _, block := range blocks {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			content = append(content, block)
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input, _ := json.Marshal(block["input"])
			content = append(content, block)
			toolCalls = append(toolCalls, ToolCall{ID: id, Name: name, Input: input})
		}
	}

	a.Messages = append(a.Messages, Message{Role: "assistant", Content: content})
	return toolCalls
}

// printToolUse displays what tool the agent is about to use (Claude Code style).
func (a *Agent) printToolUse(tc ToolCall) {
	var input map[string]string
	json.Unmarshal(tc.Input, &input)

	switch tc.Name {
	case "read_file":
		printToolHeader("📖", "Read", input["path"])
	case "write_file":
		printToolHeader("📝", "Write", input["path"])
	case "edit_file":
		printToolHeader("✏️", "Edit", input["path"])
	case "search_code":
		printToolHeader("🔍", "Search", truncate(input["pattern"], 40))
	case "run_command":
		printToolHeader("⚙", "Run", truncate(input["command"], 50))
	case "list_files":
		path := input["path"]
		if path == "" {
			path = "."
		}
		printToolHeader("📁", "List", path)
	case "update_memory":
		printToolHeader("🧠", "Memory", input["key"])
	case "todo_write":
		printToolHeader("📋", "Plan", "update task list")
	case "multi_edit":
		printToolHeader("✏️", "MultiEdit", input["path"])
	}
}

// printToolResult shows the outcome of a tool execution. For commands and
// searches it previews a few lines of output; the footer carries a status
// glyph, a line/file count, and the elapsed time.
func (a *Agent) printToolResult(tc ToolCall, result ToolResult, elapsed time.Duration) {
	dur := toolDurationLabel(elapsed)

	if result.IsError {
		printToolFooter(false, truncate(result.Content, 70))
		return
	}

	// Preview multi-line output (commands, searches, lists) inline.
	switch tc.Name {
	case "run_command", "search_code", "list_files":
		lines := strings.Split(strings.TrimRight(result.Content, "\n"), "\n")
		preview := lines
		more := 0
		if len(preview) > 5 {
			preview = lines[:5]
			more = len(lines) - 5
		}
		for _, l := range preview {
			fmt.Printf("  "+ansiDarkGray+"│"+ansiReset+" "+ansiGray+"%s"+ansiReset+"\n", l)
		}
		if more > 0 {
			fmt.Printf("  "+ansiDarkGray+"│"+ansiReset+" "+ansiDim+"… %d more lines"+ansiReset+"\n", more)
		}
	}

	// Build a descriptive footer with stats where meaningful.
	footer := ""
	switch tc.Name {
	case "read_file":
		footer = fmt.Sprintf("%d bytes · %s", len(result.Content), dur)
	case "write_file":
		footer = dur
	case "edit_file":
		footer = dur
	case "search_code":
		if n := strings.Count(result.Content, "\n") + 1; result.Content != "" && result.Content != "no matches found" {
			footer = fmt.Sprintf("%d matches · %s", n, dur)
		} else {
			footer = dur
		}
	case "list_files":
		if n := strings.Count(result.Content, "\n") + 1; result.Content != "" {
			footer = fmt.Sprintf("%d files · %s", n, dur)
		} else {
			footer = dur
		}
	default:
		footer = dur
	}
	printToolFooter(true, footer)
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	// Truncate by rune count so multi-byte characters (CJK, emoji) aren't
	// split mid-glyph, which would leave garbled output.
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}
