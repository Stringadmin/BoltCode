package agent

import "testing"

// TestUnknownSlashCommandReturnsFalse verifies that an unrecognized "/" input
// is NOT handled (returns false), so the loop can show a hint instead of
// sending it to the AI. This is the regression guard for the bug where /typo
// got answered by the model.
func TestUnknownSlashCommandReturnsFalse(t *testing.T) {
	a := &Agent{Stats: NewStats(), Messages: []Message{{Role: "user", Content: "hi"}}}
	if a.handleSlashCommand("/totally-not-a-command") {
		t.Fatal("unknown slash command should return false (not handled)")
	}
}

func TestKnownSlashCommandsHandled(t *testing.T) {
	// /model and /profile now launch interactive pickers that call readWith.
	// Provide a mock InputFn that returns "q" (cancel) so they complete.
	a := &Agent{
		Stats:    NewStats(),
		Messages: []Message{{Role: "user", Content: "hi"}},
		InputFn:  func() string { return "q" },
	}
	known := []string{"/help", "/tokens", "/time", "/model", "/context", "/sessions", "/files", "/diff", "/clear"}
	for _, cmd := range known {
		if !a.handleSlashCommand(cmd) {
			t.Errorf("known command %q was not handled", cmd)
		}
	}
}

func TestSlashCommandsListPopulated(t *testing.T) {
	if len(SlashCommands) == 0 {
		t.Fatal("SlashCommands list is empty")
	}
	// Every entry must start with "/".
	for _, c := range SlashCommands {
		if len(c.Name) == 0 || c.Name[0] != '/' {
			t.Errorf("bad command name %q", c.Name)
		}
	}
}
