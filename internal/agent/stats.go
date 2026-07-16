package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// modelPricing holds per‑1K‑token costs for input and output. Prices are in
// USD and approximate — they change periodically. The table covers common
// models; unrecognised models default to a conservative mid‑range estimate.
type modelPricing struct {
	Input  float64 // $ per 1K input tokens
	Output float64 // $ per 1K output tokens
}

var modelPrices = map[string]modelPricing{
	// Anthropic Claude 4
	"claude-sonnet-4-20250514": {Input: 0.015, Output: 0.075},
	"claude-opus-4":            {Input: 0.030, Output: 0.150},
	// Anthropic Claude 3.5
	"claude-3-5-sonnet": {Input: 0.003, Output: 0.015},
	"claude-3-opus":     {Input: 0.015, Output: 0.075},
	"claude-3-haiku":    {Input: 0.00025, Output: 0.00125},
	// GPT-4
	"gpt-4":         {Input: 0.030, Output: 0.060},
	"gpt-4-turbo":   {Input: 0.010, Output: 0.030},
	"gpt-4o":        {Input: 0.005, Output: 0.015},
	"gpt-4o-mini":   {Input: 0.00015, Output: 0.0006},
	"gpt-3.5-turbo": {Input: 0.0015, Output: 0.002},
	// DeepSeek
	"deepseek-chat": {Input: 0.0002, Output: 0.0008},
	// GLM
	"glm-4":  {Input: 0.001, Output: 0.001},
	"glm-4v": {Input: 0.001, Output: 0.001},
	"glm-5":  {Input: 0.002, Output: 0.002},
	// Qwen
	"qwen-turbo": {Input: 0.0004, Output: 0.0012},
	"qwen-plus":  {Input: 0.001, Output: 0.002},
	"qwen-max":   {Input: 0.002, Output: 0.004},
}

// estimateCost returns the approximate USD cost for (input, output) tokens
// given a model name. Unrecognised models use a conservative default.
func estimateCost(model string, input, output int) float64 {
	m := strings.ToLower(model)
	p, ok := modelPrices[m]
	if !ok {
		// Check substring matches for versioned model names.
		for key, val := range modelPrices {
			if strings.Contains(m, key) {
				p = val
				ok = true
				break
			}
		}
	}
	if !ok {
		p = modelPricing{Input: 0.003, Output: 0.015} // conservative default
	}
	return (float64(input)/1000)*p.Input + (float64(output)/1000)*p.Output
}

// Stats tracks token usage and timing for the agent session.
type Stats struct {
	mu           sync.Mutex
	InputTokens  int // cumulative across rounds
	OutputTokens int // cumulative
	LastInput    int // most recent round's input tokens = current context size
	TotalCost    float64
	StartTime    time.Time
	Rounds       int
	ToolCalls    int
	cumulative   *CumulativeUsage // cross-session totals, set before Display
}

func NewStats() *Stats {
	return &Stats{StartTime: time.Now()}
}

// SetCumulative attaches cross-session usage data for the display panel.
func (s *Stats) SetCumulative(cu *CumulativeUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cumulative = cu
}

func (s *Stats) Add(model string, input, output int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InputTokens += input
	s.OutputTokens += output
	s.LastInput = input
	s.Rounds++
	s.TotalCost += estimateCost(model, input, output)
}

func (s *Stats) AddToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ToolCalls++
}

func (s *Stats) Elapsed() time.Duration {
	return time.Since(s.StartTime)
}

// Snapshot returns a consistent copy of the counters used by interactive
// frontends. Reading fields individually would race with the API goroutine.
func (s *Stats) Snapshot() (inTok, outTok, lastInput, tools, rounds int, cost float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.InputTokens, s.OutputTokens, s.LastInput, s.ToolCalls, s.Rounds, s.TotalCost
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
	if s.TotalCost > 0 {
		b.WriteString(statRow("💰 cost", fmt.Sprintf("$%.4f", s.TotalCost)))
	}
	if s.cumulative != nil {
		b.WriteString(statRow("📊 all sessions", fmt.Sprintf("%d sessions · $%.4f", s.cumulative.SessionCount, s.cumulative.TotalCost)))
	}
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
