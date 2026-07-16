package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolEventCarriesExactActivityAndDetail(t *testing.T) {
	a := &Agent{Workdir: `F:\BoltCode`}
	input, _ := json.Marshal(map[string]string{"path": "internal/tui/chat.go"})
	event := a.toolEvent(ToolCall{ID: "call-1", Name: "read_file", Input: input})
	if event.Kind != EventToolStarted || event.Activity != "Reading file" {
		t.Fatalf("event = %+v", event)
	}
	if event.Detail != "internal/tui/chat.go" || event.ToolID != "call-1" {
		t.Fatalf("event detail = %+v", event)
	}
}

func TestCancelClosesSessionAndInterruptsCurrentOperation(t *testing.T) {
	a := &Agent{}
	a.beginSession()
	defer a.endSession()

	currentCancelled := make(chan struct{})
	a.setCurrentCancel(func() { close(currentCancelled) })
	done := a.Done()
	a.Cancel()

	select {
	case <-currentCancelled:
	case <-time.After(time.Second):
		t.Fatal("current operation was not cancelled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session context was not cancelled")
	}
}
