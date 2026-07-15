package shell

import (
	"testing"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func TestSlashCompleterNoSlash(t *testing.T) {
	c := slashCompleter{}
	matches, pos := c.Do([]rune("hello"), 5)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for non-slash input, got %d", len(matches))
	}
	if pos != 5 {
		t.Errorf("expected pos 5, got %d", pos)
	}
}

func TestSlashCompleterEmpty(t *testing.T) {
	c := slashCompleter{}
	matches, _ := c.Do([]rune("/"), 1)
	if len(matches) == 0 {
		t.Error("expected at least one match for '/'")
	}
}

func TestSlashCompleterFilters(t *testing.T) {
	c := slashCompleter{}
	// "/he" should match "/help" (and nothing else starting with "he")
	matches, _ := c.Do([]rune("/he"), 3)
	found := false
	for _, m := range matches {
		if string(m) == "lp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected '/he' to complete to '/help', got %q", matches)
	}
}

func TestSlashCompleterNoMatch(t *testing.T) {
	c := slashCompleter{}
	matches, _ := c.Do([]rune("/zzz"), 4)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for unknown prefix, got %d", len(matches))
	}
}

func TestSlashCommandListNotEmpty(t *testing.T) {
	if len(agent.SlashCommands) == 0 {
		t.Fatal("SlashCommands list is empty")
	}
	for _, cmd := range agent.SlashCommands {
		if cmd.Name == "" {
			t.Error("found SlashCommand with empty Name")
		}
	}
}

func TestNewSlashCompleter(t *testing.T) {
	c := newSlashCompleter()
	if c == nil {
		t.Fatal("newSlashCompleter returned nil")
	}
}
