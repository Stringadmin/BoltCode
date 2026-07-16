package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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
- File tools should use paths relative to the working directory (for example main.go or internal/agent), never repeat the absolute working-directory prefix
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
- Task: {{TASK}}
- Working directory: {{WORKDIR}}
- Your code will be reviewed by teammates. Write accordingly.`

// Run starts the agent loop with all features: spinner, stats, slash commands.
// If a.Messages is pre-populated (a resumed session), the initial project-scan
// greeting is skipped and the conversation continues from where it left off.
func (a *Agent) Run(inputFn func() string) error {
	sessionCtx := a.beginSession()
	defer a.endSession()

	// Build the system prompt. Using strings.NewReplacer (not fmt.Sprintf)
	// means a literal "%" in the 100-line prompt — e.g. a future "100% width"
	// CSS note — won't be misread as a format verb and garble the prompt.
	system := strings.NewReplacer(
		"{{TASK}}", a.Intent,
		"{{WORKDIR}}", a.Workdir,
	).Replace(codeSystemPrompt)
	a.Stats = NewStats()
	a.InputFn = inputFn // expose to slash commands needing follow-up input

	resuming := len(a.Messages) > 0
	var initParts []string
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
		if memCtx != "" {
			initParts = append(initParts, "## Memory (from past sessions)\n"+memCtx)
		}
		if projectCtx != "" {
			initParts = append(initParts, "## Current Project\n"+projectCtx)
		}
	} else {
		if !a.UIMode {
			fmt.Println()
			fmt.Printf("  "+ansiDarkGray+"↻ resumed session"+ansiReset+" %s"+ansiDarkGray+" · %d messages · continue where you left off"+ansiReset+"\n\n",
				a.SessionID, len(a.Messages))
		}
		// Auto-compact if the resumed session is large enough to risk
		// overflowing the context window on the first API call.
		window := modelContextWindow(a.Config.Model)
		est := estimateTokens(a.Messages)
		if est > 0 && shouldCompact(est, window) {
			fmt.Printf("  "+ansiYellow+"⚡ context ~%s — compacting before resume..."+ansiReset+"\n",
				formatTokens(est))
			if err := a.compactMessages(8); err != nil {
				fmt.Printf("  "+ansiRed+"✗ compaction failed: %v"+ansiReset+"\n", err)
			} else {
				fmt.Printf("  "+ansiGreen+"✓ compacted to %d messages"+ansiReset+"\n", len(a.Messages))
			}
		}
	}

	if a.UIMode {
		// The TUI renders its own local welcome panel. Wait for the first real
		// instruction instead of spending an API round on a generated greeting;
		// this also makes the UI's ready handshake authoritative from turn one.
		firstInput := ""
		ok := true
		if !resuming && strings.TrimSpace(a.Intent) != "" {
			firstInput = strings.TrimSpace(a.Intent)
		} else {
			firstInput, ok = a.waitForUserInput(inputFn)
		}
		if !ok {
			return nil
		}
		if resuming {
			a.Messages = append(a.Messages, Message{Role: "user", Content: firstInput})
		} else {
			a.Messages = []Message{{Role: "user", Content: composeInitialUserMessage(initParts, firstInput)}}
		}
	} else if !resuming {
		// Preserve the classic shell greeting outside the full-screen TUI.
		initMsg := "Hi."
		if len(initParts) > 0 {
			initMsg = "Hi. Here is the project context:\n\n" + strings.Join(initParts, "\n\n") + "\n\nPlease greet me briefly and ask what I need."
		}
		a.Messages = []Message{{Role: "user", Content: initMsg}}
	}

	var sessionErr error
	for round := 0; round < 100; round++ {
		if sessionCtx.Err() != nil {
			break
		}
		if a.consumeInterrupt() {
			a.emitPhase("Interrupted")
			input, ok := a.waitForUserInput(inputFn)
			if !ok {
				break
			}
			a.Messages = append(a.Messages, Message{Role: "user", Content: input})
			continue
		}
		// Show spinner while waiting for API
		var spin *Spinner
		if !a.NoSpinner {
			spin = NewSpinner("thinking...")
			spin.Start()
		}
		a.emitPhase("Thinking")

		resp, usage, err := a.callAPI(system)

		if spin != nil {
			spin.Stop()
		}
		if !a.UIMode {
			fmt.Print("  ❯\n") // prompt stays visible after spinner (shell mode)
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				if sessionCtx.Err() != nil {
					break
				}
				a.consumeInterrupt()
				// User pressed Ctrl+C — don't kill the session, just abort this
				// round and return to the input prompt.
				fmt.Println("  " + ansiYellow + "⚡ interrupted — type to continue, or 'exit' to quit" + ansiReset)
				a.ToolErrorCount = 0
				a.emitPhase("Interrupted")
				input, ok := a.waitForUserInput(inputFn)
				if !ok {
					break
				}
				a.Messages = append(a.Messages, Message{Role: "user", Content: input})
				continue
			}
			recoveryErr := fmt.Errorf("API error: %w", err)
			a.emit(Event{Kind: EventError, Activity: "Request failed", Result: recoveryErr.Error(), IsError: true})
			if !a.UIMode {
				fmt.Println("  " + ansiRed + "✗ " + recoveryErr.Error() + ansiReset)
				fmt.Println("  " + ansiDarkGray + "type /retry to try again, or enter a new instruction" + ansiReset)
			}
			input, retry, ok := a.waitForRecoveryInput(inputFn)
			if !ok {
				break
			}
			if retry {
				a.emitPhase("Retrying request")
				continue
			}
			a.Messages = append(a.Messages, Message{Role: "user", Content: input})
			a.ToolErrorCount = 0
			continue
		}

		// Track token usage. LastInput (from the most recent API response) is
		// the authoritative current context size.
		a.Stats.Add(a.Config.Model, usage.InputTokens, usage.OutputTokens)

		toolCalls := a.processResponse(resp)

		if len(toolCalls) == 0 {
			// AI finished speaking — auto-compact if context is near the window,
			// then show a session footer with stats + a context-usage bar.
			window := modelContextWindow(a.Config.Model)
			if shouldCompact(a.Stats.LastInput, window) {
				a.autoCompact()
			}

			// In TUI mode the status bar shows tokens/tools/rounds/context, so
			// skip the inline footer to avoid duplicate noise.
			if !a.UIMode {
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
			}

			input, ok := a.waitForUserInput(inputFn)
			if !ok {
				break
			}
			a.Messages = append(a.Messages, Message{Role: "user", Content: input})
			a.ToolErrorCount = 0 // user is steering in a new direction — reset retries
			continue
		}

		// Execute tools with self-repair logic
		var results []map[string]interface{}
		hasError := false
		interrupted := false
		for index, tc := range toolCalls {
			a.Stats.AddToolCall()
			started := a.toolEvent(tc)
			a.emit(started)
			a.printToolUse(tc)
			start := time.Now()
			result := a.ExecTool(tc)
			elapsed := time.Since(start)
			a.printToolResult(tc, result, elapsed)
			finishedEvent := Event{
				Kind:     EventToolFinished,
				Activity: "Reviewing tool result",
				ToolID:   tc.ID,
				ToolName: tc.Name,
				Detail:   started.Detail,
				Result:   result.Content,
				IsError:  result.IsError,
				Duration: elapsed,
			}
			a.addPlanState(&finishedEvent)
			a.emit(finishedEvent)
			if result.IsError {
				hasError = true
			}
			results = append(results, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": result.ToolUseID,
				"content":     result.Content,
				"is_error":    result.IsError,
			})
			interrupted = a.consumeInterrupt()
			if interrupted || sessionCtx.Err() != nil {
				for _, skipped := range toolCalls[index+1:] {
					results = append(results, map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": skipped.ID,
						"content":     "skipped because the session was interrupted",
						"is_error":    true,
					})
				}
				break
			}
		}
		a.Messages = append(a.Messages, Message{Role: "user", Content: results})
		if sessionCtx.Err() != nil {
			break
		}
		if interrupted {
			a.ToolErrorCount = 0
			a.emitPhase("Interrupted")
			input, ok := a.waitForUserInput(inputFn)
			if !ok {
				break
			}
			a.Messages = append(a.Messages, Message{Role: "user", Content: input})
			continue
		}

		// Self-repair: if tools failed, inject a nudge for the AI to fix it.
		// Track consecutive failure rounds so we stop nudging after 3 attempts
		// (matching the "max 3 retries" advertised in the system prompt).
		if hasError {
			a.ToolErrorCount++
			if a.ToolErrorCount <= 3 {
				a.Messages = append(a.Messages, Message{
					Role:    "user",
					Content: fmt.Sprintf("[System] One or more tool calls failed (attempt %d/3). Please analyze the error above and fix it. If a build/test failed, read the error message carefully and correct your code.", a.ToolErrorCount),
				})
			} else {
				// Stop automatic self-repair, but keep the conversation alive so
				// the user can retry or steer the agent instead of losing the TUI.
				recoveryErr := fmt.Errorf("tool retries exhausted after 3 failed repair attempts")
				a.emit(Event{Kind: EventError, Activity: "Tool repair stopped", Result: recoveryErr.Error(), IsError: true})
				if !a.UIMode {
					fmt.Println()
					fmt.Printf("  "+ansiRed+"✗ %v"+ansiReset+"\n", recoveryErr)
				}
				input, retry, ok := a.waitForRecoveryInput(inputFn)
				if !ok {
					break
				}
				a.ToolErrorCount = 0
				if retry {
					continue
				}
				a.Messages = append(a.Messages, Message{Role: "user", Content: input})
				continue
			}
		} else {
			a.ToolErrorCount = 0
		}
	}

	// Save cumulative stats across sessions
	a.saveCumulativeStats()

	// Show session stats and end animation (skipped in TUI mode — the TUI
	// draws its own exit UI).
	if !a.UIMode {
		fmt.Println()
		fmt.Println(a.Stats.Display())
		AnimateSessionEnd()
	}

	// Auto-save the conversation so it can be resumed, Claude-Code style.
	// Skip saving if the session was empty (just opened and exited).
	if len(a.Messages) > 1 {
		if id, err := SaveSession(a); err == nil && !a.UIMode {
			fmt.Println()
			fmt.Printf("  "+ansiDarkGray+"✓ session saved — resume with:"+ansiReset+" boltcode --resume %s\n", id)
		}
	}
	return sessionErr
}

// waitForRecoveryInput is used after an API failure. /retry repeats the exact
// same model request without mutating history; a normal message is appended as
// new steering, while other slash commands remain available.
func (a *Agent) waitForRecoveryInput(inputFn func() string) (input string, retry bool, ok bool) {
	for {
		if a.ShouldExit || a.context().Err() != nil {
			return "", false, false
		}
		value := strings.TrimSpace(inputFn())
		switch value {
		case "":
			continue
		case "/retry", "retry":
			return "", true, true
		case "exit", "done", "quit", "/exit", "/quit":
			return "", false, false
		}
		if strings.HasPrefix(value, "/") {
			if a.handleSlashCommand(value) {
				continue
			}
			fmt.Println("  " + ansiYellow + "unknown command: " + value + ansiReset)
			continue
		}
		return value, false, true
	}
}

// waitForUserInput filters empty lines and slash commands until it receives a
// real prompt. The bool is false when the session should exit.
func (a *Agent) waitForUserInput(inputFn func() string) (string, bool) {
	for {
		if a.ShouldExit {
			return "", false
		}
		input := strings.TrimSpace(inputFn())
		if input == "" {
			continue
		}
		if strings.HasPrefix(input, "/") {
			if a.handleSlashCommand(input) {
				continue
			}
			fmt.Println("  " + ansiYellow + "unknown command: " + input + ansiReset)
			fmt.Println("  " + ansiDarkGray + "type /help for the command list" + ansiReset)
			continue
		}
		if input == "exit" || input == "done" || input == "quit" {
			return "", false
		}
		return input, true
	}
}

func composeInitialUserMessage(contextParts []string, input string) string {
	if len(contextParts) == 0 {
		return input
	}
	return strings.Join(contextParts, "\n\n") + "\n\n## User request\n" + input
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
	fmt.Print("  \033[38;5;75m❯\033[0m\n")
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
	hasText := false

	for _, block := range blocks {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			content = append(content, block)
			hasText = true
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input, _ := json.Marshal(block["input"])
			content = append(content, block)
			toolCalls = append(toolCalls, ToolCall{ID: id, Name: name, Input: input})
		case "thinking", "redacted_thinking":
			// Preserve required Anthropic reasoning metadata in history without
			// rendering private thinking text in the chat UI.
			content = append(content, block)
		}
	}

	a.Messages = append(a.Messages, Message{Role: "assistant", Content: content})
	if hasText {
		a.emitPhase("Writing response")
	}
	return toolCalls
}

// printToolUse displays what tool the agent is about to use (Claude Code style).
func (a *Agent) printToolUse(tc ToolCall) {
	if a.UIMode && a.EventFn != nil {
		return
	}
	var input map[string]string
	json.Unmarshal(tc.Input, &input)

	switch tc.Name {
	case "read_file":
		printToolHeader("📖", "Read", a.displayToolPath(input["path"]))
	case "write_file":
		printToolHeader("📝", "Write", a.displayToolPath(input["path"]))
	case "edit_file":
		printToolHeader("✏️", "Edit", a.displayToolPath(input["path"]))
	case "search_code":
		printToolHeader("🔍", "Search", truncate(input["pattern"], 40))
	case "run_command":
		printToolHeader("⚙", "Run", truncate(input["command"], 50))
	case "list_files":
		path := input["path"]
		if path == "" {
			path = "."
		}
		printToolHeader("📁", "List", a.displayToolPath(path))
	case "update_memory":
		printToolHeader("🧠", "Memory", input["key"])
	case "todo_write":
		printToolHeader("📋", "Plan", "update task list")
	case "multi_edit":
		printToolHeader("✏️", "MultiEdit", a.displayToolPath(input["path"]))
	case "glob":
		printToolHeader("🔎", "Glob", truncate(input["pattern"], 40))
	}
}

// printToolResult shows the outcome of a tool execution. For commands and
// searches it previews a few lines of output; the footer carries a status
// glyph, a line/file count, and the elapsed time.
func (a *Agent) printToolResult(tc ToolCall, result ToolResult, elapsed time.Duration) {
	if a.UIMode && a.EventFn != nil {
		return
	}
	dur := toolDurationLabel(elapsed)

	if result.IsError {
		for _, line := range wrapToolError(result.Content, 88, 4) {
			fmt.Printf("    "+ansiDarkGray+"│"+ansiReset+"  "+ansiRed+"%s"+ansiReset+"\n", line)
		}
		footer := "failed"
		if dur != "" {
			footer += " · " + dur
		}
		a.printToolFooter(false, footer)
		return
	}

	// Preview multi-line output (commands, searches, lists) inline.
	switch tc.Name {
	case "read_file":
		fmt.Printf("    " + ansiDarkGray + "│" + ansiReset + "  " + ansiDim + "content collapsed · full file sent to AI" + ansiReset + "\n")
	case "run_command", "search_code", "list_files":
		lines := strings.Split(strings.TrimRight(result.Content, "\n"), "\n")
		preview := lines
		more := 0
		if len(preview) > 5 {
			preview = lines[:5]
			more = len(lines) - 5
		}
		for _, l := range preview {
			fmt.Printf("    "+ansiDarkGray+"│"+ansiReset+"  "+ansiGray+"%s"+ansiReset+"\n", l)
		}
		if more > 0 {
			fmt.Printf("    "+ansiDarkGray+"│"+ansiReset+"  "+ansiDim+"▸ %d more lines collapsed · full output sent to AI"+ansiReset+"\n", more)
		}
	case "edit_file", "multi_edit":
		// Show a compact diff preview if the tool populated one.
		if result.Diff != "" {
			for _, l := range strings.Split(result.Diff, "\n") {
				fmt.Println(l)
			}
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
	a.printToolFooter(true, footer)
}

func (a *Agent) displayToolPath(path string) string {
	if path == "" {
		return "."
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) && a.Workdir != "" {
		if rel, err := filepath.Rel(a.Workdir, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			clean = rel
		}
	}
	return truncate(filepath.ToSlash(clean), 88)
}

func wrapToolError(text string, width, maxLines int) []string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r", ""))
	if text == "" {
		return []string{"unknown error"}
	}
	var lines []string
	for _, logical := range strings.Split(text, "\n") {
		lines = append(lines, wrapToolErrorLine(logical, width)...)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimSpace(lines[maxLines-1]) + " …"
	}
	return lines
}

func wrapToolErrorLine(text string, width int) []string {
	remaining := []rune(strings.TrimSpace(text))
	var lines []string
	for len(remaining) > 0 {
		used := 0
		cut := 0
		lastSpace := -1
		for i, r := range remaining {
			runeWidth := 1
			if isWide(r) {
				runeWidth = 2
			}
			if used+runeWidth > width {
				break
			}
			used += runeWidth
			cut = i + 1
			if r == ' ' || r == '\t' {
				lastSpace = i
			}
		}
		if cut == len(remaining) {
			lines = append(lines, strings.TrimSpace(string(remaining)))
			break
		}
		if lastSpace > 0 {
			cut = lastSpace
		}
		if cut == 0 {
			cut = 1
		}
		lines = append(lines, strings.TrimSpace(string(remaining[:cut])))
		remaining = []rune(strings.TrimSpace(string(remaining[cut:])))
	}
	return lines
}

// truncate shortens s to max runes (newline-flattened) with a "…" indicator.
// It delegates to the shared truncateStr so the rune-slicing logic lives in one
// place; the newline replacement is specific to inline display contexts.
func truncate(s string, max int) string {
	return truncateStr(strings.ReplaceAll(s, "\n", " "), max, "...")
}
