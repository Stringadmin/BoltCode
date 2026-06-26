package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Stats tracks token usage and timing for the agent session.
type Stats struct {
	mu            sync.Mutex
	InputTokens   int // cumulative across rounds
	OutputTokens  int // cumulative
	LastInput     int // most recent round's input tokens = current context size
	TotalCost     float64
	StartTime     time.Time
	Rounds        int
	ToolCalls     int
}

func NewStats() *Stats {
	return &Stats{StartTime: time.Now()}
}

func (s *Stats) Add(input, output int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InputTokens += input
	s.OutputTokens += output
	s.LastInput = input
	s.Rounds++
}

func (s *Stats) AddToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ToolCalls++
}

func (s *Stats) Elapsed() time.Duration {
	return time.Since(s.StartTime)
}

// Display renders a compact session summary panel.
func (s *Stats) Display() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	elapsed := time.Since(s.StartTime)
	total := s.InputTokens + s.OutputTokens

	var b strings.Builder
	b.WriteString(ansiDarkGray + "  ╭" + strings.Repeat("─", 40) + "╮" + ansiReset + "\n")
	b.WriteString(fmt.Sprintf("  %s│%s %s session summary%s%s\n",
		ansiDarkGray, ansiReset, ansiBold+ansiWhite, strings.Repeat(" ", 22), ansiReset+ansiDarkGray+"│"+ansiReset))
	b.WriteString(ansiDarkGray + "  ├" + strings.Repeat("─", 40) + "┤" + ansiReset + "\n")
	b.WriteString(statRow("⏱  time", formatDuration(elapsed)))
	b.WriteString(statRow("◆  tokens", fmt.Sprintf("%d in · %d out · %d total", s.InputTokens, s.OutputTokens, total)))
	b.WriteString(statRow("🔄 rounds", fmt.Sprintf("%d", s.Rounds)))
	b.WriteString(statRow("🔧 tools", fmt.Sprintf("%d", s.ToolCalls)))
	b.WriteString(ansiDarkGray + "  ╰" + strings.Repeat("─", 40) + "╯" + ansiReset)
	return b.String()
}

// statRow formats one label/value line inside the summary box, right-aligned
// padding so the panel edges line up. label is dimmed, value is bright.
func statRow(label, value string) string {
	const inner = 40
	// visible content between the borders: " " + label + "  " + value + padding
	content := " " + label + "  " + value
	pad := inner - visibleWidth(content)
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("  %s│%s %s%s%s  %s%s%s%s\n",
		ansiDarkGray, ansiReset,
		ansiGray, label, ansiReset,
		ansiWhite, value, ansiReset,
		strings.Repeat(" ", pad)+ansiDarkGray+"│"+ansiReset)
}

// visibleWidth returns the display width of s, ignoring ANSI escape sequences.
// Good enough for our fixed-width panel — it counts runes after stripping
// escapes, which is accurate for the ASCII labels/values we use here.
func visibleWidth(s string) int {
	cleaned := stripAnsi(s)
	return len([]rune(cleaned))
}

func stripAnsi(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}
