package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

type toolTimelineEntry struct {
	id       string
	name     string
	activity string
	detail   string
	result   string
	running  bool
	failed   bool
	duration time.Duration
}

func (m *ChatModel) recordToolEvent(event agent.Event) {
	// todo_write already has a dedicated visual plan panel. Keeping it in the
	// persistent tool timeline produced a misleading "Plan · task list" row
	// after the active plan had been retired.
	if event.ToolName != "todo_write" {
		switch event.Kind {
		case agent.EventToolStarted:
			m.toolTimeline = append(m.toolTimeline, toolTimelineEntry{
				id:       event.ToolID,
				name:     event.ToolName,
				activity: event.Activity,
				detail:   event.Detail,
				running:  true,
			})
			if len(m.toolTimeline) > 100 {
				m.toolTimeline = append([]toolTimelineEntry(nil), m.toolTimeline[len(m.toolTimeline)-100:]...)
			}
		case agent.EventToolFinished:
			index := m.toolEntryIndex(event.ToolID)
			if index < 0 {
				m.toolTimeline = append(m.toolTimeline, toolTimelineEntry{id: event.ToolID, name: event.ToolName})
				index = len(m.toolTimeline) - 1
			}
			entry := &m.toolTimeline[index]
			entry.running = false
			entry.failed = event.IsError
			entry.duration = event.Duration
			entry.result = truncateTimelineResult(event.Result, 6000)
			if event.Detail != "" {
				entry.detail = event.Detail
			}
			if event.Activity != "" {
				entry.activity = event.Activity
			}
		}
	}
	if event.PlanItems != nil || event.ToolName == "todo_write" {
		// Ready and tool events are consumed by independent Bubble Tea commands.
		// A completed todo_write event can therefore arrive after agentReadyMsg.
		// Never let that late event resurrect a plan already retired at ready.
		if !m.busy && planItemsCompleted(event.PlanItems) {
			m.setPlanItems(nil)
			if m.agent != nil {
				m.agent.TodoList = nil
			}
		} else {
			m.setPlanItems(event.PlanItems)
		}
	} else if event.PlanTotal > 0 {
		// Compatibility with callers/tests that only populate the compact plan
		// counters rather than the structured snapshot.
		m.planDone = event.PlanDone
		m.planTotal = event.PlanTotal
		m.planCurrent = event.PlanCurrent
	}
}

func (m *ChatModel) toolEntryIndex(id string) int {
	for index := len(m.toolTimeline) - 1; index >= 0; index-- {
		if m.toolTimeline[index].id == id {
			return index
		}
	}
	return -1
}

func (m *ChatModel) renderToolTimeline() string {
	if len(m.toolTimeline) == 0 {
		return ""
	}
	mode := "Ctrl+O expand results"
	if m.toolsExpanded {
		mode = "Ctrl+O collapse results"
	}
	lines := []string{chatDimStyle.Render(fmt.Sprintf("  ╭─ tools · %d · %s", len(m.toolTimeline), mode))}
	start := 0
	if !m.toolsExpanded && len(m.toolTimeline) > 6 {
		start = len(m.toolTimeline) - 6
		lines = append(lines, chatDimStyle.Render(fmt.Sprintf("  │  … %d earlier tool calls", start)))
	}
	for _, entry := range m.toolTimeline[start:] {
		glyph := chatGreenStyle.Render("✓")
		if entry.running {
			glyph = chatPromptStyle.Render("◌")
		} else if entry.failed {
			glyph = chatErrorStyle.Render("✕")
		}
		label := toolDisplayName(entry.name, entry.activity)
		line := "  │  " + glyph + " " + label
		if entry.detail != "" {
			line += chatDimStyle.Render(" · " + entry.detail)
		}
		if !entry.running && entry.duration > 0 {
			line += chatDimStyle.Render(" · " + compactDuration(entry.duration))
		}
		lines = append(lines, line)
		if m.toolsExpanded && strings.TrimSpace(entry.result) != "" {
			preview := strings.Split(strings.TrimSpace(entry.result), "\n")
			more := 0
			if len(preview) > 8 {
				more = len(preview) - 8
				preview = preview[:8]
			}
			for _, resultLine := range preview {
				resultLine = ansi.Truncate(strings.ReplaceAll(resultLine, "\t", "    "), maxTimelineWidth(m.width), "…")
				lines = append(lines, chatDimStyle.Render("  │     "+resultLine))
			}
			if more > 0 {
				lines = append(lines, chatDimStyle.Render(fmt.Sprintf("  │     … %d more lines", more)))
			}
		}
	}
	lines = append(lines, chatDimStyle.Render("  ╰─"))
	return strings.Join(lines, "\n")
}

func toolDisplayName(name, fallback string) string {
	switch name {
	case "read_file":
		return "Read"
	case "write_file":
		return "Write"
	case "edit_file":
		return "Edit"
	case "multi_edit":
		return "Multi-edit"
	case "search_code":
		return "Search"
	case "run_command":
		return "Run"
	case "list_files":
		return "List"
	case "glob":
		return "Glob"
	case "todo_write":
		return "Plan"
	case "update_memory":
		return "Memory"
	}
	if name != "" {
		return name
	}
	return fallback
}

func compactDuration(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", duration.Seconds())
}

func maxTimelineWidth(width int) int {
	if width <= 16 {
		return 20
	}
	return width - 12
}

func truncateTimelineResult(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n… result truncated"
}
