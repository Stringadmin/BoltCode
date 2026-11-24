package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *Agent) toolWebFetch(url string) (string, bool) {
	if url == "" {
		return "url is required", true
	}
	client := &http.Client{Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(a.context(), 15*time.Second)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Sprintf("invalid url: %v", err), true
	}
	req.Header.Set("User-Agent", "BoltCode/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("error fetching %s: %v", url, err), true
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Sprintf("HTTP %d fetching %s", resp.StatusCode, url), true
	}
	// Limit to 500KB to avoid blowing the context window.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500<<10))
	if err != nil {
		return fmt.Sprintf("error reading response: %v", err), true
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/html") {
		// Strip basic HTML for readability. A simple approach: remove tags.
		text := stripHTML(string(body))
		return fmt.Sprintf("URL: %s\nContent-Type: %s\n\n%s", url, contentType, text), false
	}
	text := string(body)
	return fmt.Sprintf("URL: %s\nContent-Type: %s\n\n%s", url, contentType, text), false
}

// stripHTML removes HTML tags for basic readability of fetched pages.
func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	// Collapse multiple whitespace runs.
	result := out.String()
	lines := strings.Split(result, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

// toolUpdateMemory lets the AI persist project knowledge across sessions.
// key: one of "architecture", "build_cmd", "test_cmd", "language", "framework",
//
//	"file:<path>" (maps a file to its description), "preference", "decision"
//
// value: the information to remember
func (a *Agent) toolUpdateMemory(key, value string) (string, bool) {
	if a.WorkspaceMemory != "" && (key == "build_cmd" || key == "test_cmd") {
		return "Build and test commands are tracked from the current workspace manifest.", false
	}
	mem := a.LoadMemory()

	switch {
	case key == "architecture":
		mem.Architecture = value
	case key == "build_cmd":
		mem.BuildCmd = value
	case key == "test_cmd":
		mem.TestCmd = value
	case key == "language":
		mem.Language = value
	case key == "framework":
		mem.Framework = value
	case strings.HasPrefix(key, "file:"):
		file := strings.TrimPrefix(key, "file:")
		mem.FileMap[file] = value
	case key == "preference":
		mem.Preferences = append(mem.Preferences, value)
	case key == "decision":
		// value is expected as "topic: choice". Split on the first colon so
		// the decision is recorded with a distinct topic and choice rather
		// than duplicating the whole string into both fields.
		topic, choice := value, value
		if idx := strings.Index(value, ":"); idx >= 0 {
			topic = strings.TrimSpace(value[:idx])
			choice = strings.TrimSpace(value[idx+1:])
		}
		mem.Decisions = append(mem.Decisions, Decision{
			Topic: topic, Choice: choice, Date: mem.UpdatedAt.Format("2006-01-02"),
		})
	default:
		return "unknown memory key: " + key, true
	}

	if err := a.SaveMemory(mem); err != nil {
		return fmt.Sprintf("error saving memory: %v", err), true
	}
	return "memory updated: " + key, false
}

// toolTodoWrite replaces the session task plan. Each item has content + status
// (pending/in_progress/completed). An empty list clears it.
func (a *Agent) toolTodoWrite(toolUseID string, raw json.RawMessage) ToolResult {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("malformed todo input: %v", err), IsError: true}
	}
	// Validate statuses.
	for i, t := range in.Todos {
		switch t.Status {
		case "pending", "in_progress", "completed":
		default:
			in.Todos[i].Status = "pending"
		}
	}
	a.TodoList = in.Todos
	if len(a.TodoList) == 0 {
		return ToolResult{ToolUseID: toolUseID, Content: "todo list cleared", IsError: false}
	}
	done := 0
	for _, t := range a.TodoList {
		if t.Status == "completed" {
			done++
		}
	}
	return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("todo list updated: %d/%d completed", done, len(a.TodoList)), IsError: false}
}
