package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func TestSessionPickerRendersUsefulRows(t *testing.T) {
	m := &sessionPickerModel{
		sessions: []agent.Session{{
			ID:        "e1c3b82f",
			Model:     "deepseek-chat",
			UpdatedAt: time.Date(2026, 7, 16, 14, 0, 0, 0, time.Local),
			Intent:    "检查整个项目架构并修复输入问题",
		}},
		width:  100,
		height: 24,
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Resume a BoltCode session", "e1c3b82f", "deepseek-chat", "检查整个项目架构", "Enter resume"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q:\n%s", want, view)
		}
	}
}

func TestSessionPickerEnterAndEscape(t *testing.T) {
	m := &sessionPickerModel{sessions: []agent.Session{{ID: "a"}, {ID: "b"}}, width: 80, height: 20}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.(*sessionPickerModel).selected != 1 {
		t.Fatal("down did not move selection")
	}
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.(*sessionPickerModel).chosen || cmd == nil {
		t.Fatal("enter did not choose session")
	}
	m = &sessionPickerModel{sessions: []agent.Session{{ID: "a"}}, width: 80, height: 20}
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !model.(*sessionPickerModel).cancelled || cmd == nil {
		t.Fatal("escape did not cancel picker")
	}
}
