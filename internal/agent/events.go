package agent

import (
	"encoding/json"
	"time"
)

// EventKind identifies structured progress emitted by the agent runtime.
// Keeping this small and transport-agnostic lets terminal, TUI, and future GUI
// frontends render the same execution lifecycle without parsing stdout.
type EventKind string

const (
	EventPhase        EventKind = "phase"
	EventToolStarted  EventKind = "tool_started"
	EventToolFinished EventKind = "tool_finished"
	EventPlanUpdated  EventKind = "plan_updated"
	EventError        EventKind = "error"
)

// Event describes one user-visible runtime transition.
type Event struct {
	Kind        EventKind
	Activity    string
	ToolID      string
	ToolName    string
	Detail      string
	Result      string
	IsError     bool
	Duration    time.Duration
	PlanDone    int
	PlanTotal   int
	PlanCurrent string
	PlanItems   []TodoItem
}

func (a *Agent) addPlanState(event *Event) {
	if event == nil {
		return
	}
	event.PlanItems = append([]TodoItem(nil), a.TodoList...)
	for _, item := range a.TodoList {
		event.PlanTotal++
		if item.Status == "completed" {
			event.PlanDone++
		}
		if item.Status == "in_progress" && event.PlanCurrent == "" {
			event.PlanCurrent = truncate(item.Content, 48)
		}
	}
}

func (a *Agent) emit(event Event) {
	if a.EventFn != nil {
		a.EventFn(event)
	}
}

func (a *Agent) emitPhase(activity string) {
	a.emit(Event{Kind: EventPhase, Activity: activity})
}

// clearCompletedPlanForNewTurn retires a finished plan when the user starts a
// new conversational turn. In-progress plans remain visible because a user
// response may be answering a clarification within the same task.
func (a *Agent) clearCompletedPlanForNewTurn() bool {
	if len(a.TodoList) == 0 {
		return false
	}
	for _, item := range a.TodoList {
		if item.Status != "completed" {
			return false
		}
	}
	a.TodoList = nil
	// A non-nil empty slice distinguishes "clear the plan" from an event that
	// simply has no plan payload.
	a.emit(Event{Kind: EventPlanUpdated, PlanItems: []TodoItem{}})
	return true
}

func (a *Agent) toolEvent(call ToolCall) Event {
	activity := "Using tool"
	detail := a.displayToolPath(toolInputValue(call, "path"))
	switch call.Name {
	case "read_file":
		activity = "Reading file"
	case "write_file":
		activity = "Writing file"
	case "edit_file", "multi_edit":
		activity = "Editing file"
	case "search_code":
		activity = "Searching code"
		detail = toolInputValue(call, "pattern")
	case "run_command":
		activity = "Running command"
		detail = toolInputValue(call, "command")
	case "list_files":
		activity = "Listing files"
		if detail == "" {
			detail = "."
		}
	case "glob":
		activity = "Matching files"
		detail = toolInputValue(call, "pattern")
	case "todo_write":
		activity = "Updating plan"
		detail = "task list"
	case "update_memory":
		activity = "Updating project memory"
		detail = toolInputValue(call, "key")
	}
	return Event{
		Kind:     EventToolStarted,
		Activity: activity,
		ToolID:   call.ID,
		ToolName: call.Name,
		Detail:   truncate(detail, 96),
	}
}

func toolInputValue(call ToolCall, key string) string {
	var input map[string]interface{}
	if jsonErr := json.Unmarshal(call.Input, &input); jsonErr != nil {
		return ""
	}
	value, _ := input[key].(string)
	return value
}
