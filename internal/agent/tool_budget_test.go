package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolResultBudgetScalesWithContextWindow(t *testing.T) {
	tests := []struct {
		window int
		want   int
	}{
		{16_000, minToolResultRunes},
		{64_000, 9_600},
		{128_000, 19_200},
		{1_000_000, maxToolResultRunes},
	}
	for _, test := range tests {
		a := &Agent{Config: Config{ContextWindow: test.window}}
		if got := a.toolResultRuneLimit(); got != test.want {
			t.Errorf("window %d: result budget = %d, want %d", test.window, got, test.want)
		}
	}
}

func TestToolResultTruncationIsUnicodeSafe(t *testing.T) {
	a := &Agent{Config: Config{ContextWindow: 16_000}}
	result := a.truncateToolOutput(strings.Repeat("中", minToolResultRunes+20))
	if !utf8.ValidString(result) {
		t.Fatal("tool result truncation split a UTF-8 character")
	}
	if !strings.Contains(result, "active context budget") {
		t.Fatalf("truncation marker missing: %q", result[len(result)-80:])
	}
	if got := len([]rune(result)); got != minToolResultRunes {
		t.Fatalf("result runes = %d, want %d", got, minToolResultRunes)
	}
	if !strings.HasPrefix(result, strings.Repeat("中", 100)) || !strings.HasSuffix(result, strings.Repeat("中", 100)) {
		t.Fatal("context truncation did not preserve both the beginning and end")
	}
}

func TestToolResultTruncationPreservesCommandTail(t *testing.T) {
	a := &Agent{Config: Config{ContextWindow: 16_000}}
	value := strings.Repeat("build output\n", 1_000) + "exit code: 1"
	result := a.truncateToolOutput(value)
	if !strings.HasSuffix(result, "exit code: 1") {
		t.Fatalf("command tail was lost: %q", result[len(result)-80:])
	}
}
