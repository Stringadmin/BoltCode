package agent

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// modelContextWindow returns the context window size (in tokens) for a model.
// Falls back to a conservative default if unknown.
//
// The built-in table covers common models, but new models ship constantly. To
// override for any model (or a custom gateway model not listed), set the
// BOLTCODE_CONTEXT_WINDOW environment variable to a token count, e.g.
//
//	BOLTCODE_CONTEXT_WINDOW=256000
//
// This takes precedence over the table so users are never stuck with a wrong
// default while waiting for a BoltCode update.
func modelContextWindow(model string) int {
	if v := strings.TrimSpace(os.Getenv("BOLTCODE_CONTEXT_WINDOW")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude-sonnet-4"), strings.Contains(m, "claude-3-5-sonnet"):
		return 200_000
	case strings.Contains(m, "claude-opus-4"), strings.Contains(m, "claude-3-opus"):
		return 200_000
	case strings.Contains(m, "claude-haiku"):
		return 200_000
	case strings.Contains(m, "glm-5"), strings.Contains(m, "glm5"):
		return 128_000
	case strings.Contains(m, "glm-4"), strings.Contains(m, "gpt-4"):
		return 128_000
	case strings.Contains(m, "gpt-3.5"):
		return 16_000
	case strings.Contains(m, "deepseek"):
		return 64_000
	case strings.Contains(m, "qwen"):
		return 32_000
	}
	return 128_000 // conservative default
}

// ModelContextWindow exposes the configured context size to frontends without
// duplicating provider/model heuristics in the UI package.
func ModelContextWindow(model string) int {
	return modelContextWindow(model)
}

// compactThreshold is the fraction of the context window at which auto-compact
// kicks in. 0.8 = compact when usage reaches 80%.
const compactThreshold = 0.8

// warnThreshold is the fraction at which the progress bar turns red.
const warnThreshold = 0.9

// estimateTokens gives a rough token count for a message slice without calling
// the API. Used for the live preview bar between API calls (the authoritative
// count comes back from each API response's input_tokens). Heuristic: ~4 chars
// per token for English, ~1.5 chars per token for CJK — blended to ~3 chars/token.
func estimateTokens(msgs []Message) int {
	chars := 0
	for _, m := range msgs {
		chars += messageCharLen(m)
	}
	if chars == 0 {
		return 0
	}
	return chars / 3
}

// messageCharLen returns the visible character length of a message's content,
// which may be a string or a slice of content blocks (tool results etc.).
func messageCharLen(m Message) int {
	switch v := m.Content.(type) {
	case string:
		return len([]rune(v))
	case []interface{}:
		n := 0
		for _, item := range v {
			if mm, ok := item.(map[string]interface{}); ok {
				if s, ok := mm["content"].(string); ok {
					n += len([]rune(s))
				}
				if s, ok := mm["text"].(string); ok {
					n += len([]rune(s))
				}
			}
		}
		return n
	}
	return 0
}

// contextBar renders a progress bar showing current context usage.
// used is the current token count (from the last API response's input_tokens,
// or an estimate between calls); window is the model's context window.
func contextBar(used, window int) string {
	if window <= 0 {
		window = 128_000
	}
	pct := float64(used) / float64(window)
	if pct > 1 {
		pct = 1
	}
	const width = 16
	filled := int(pct * width)
	color := ansiGreen
	switch {
	case pct >= warnThreshold:
		color = ansiRed
	case pct >= compactThreshold:
		color = ansiYellow
	}
	bar := color + strings.Repeat("█", filled) + ansiDarkGray + strings.Repeat("░", width-filled) + ansiReset
	return fmt.Sprintf("%s %s%d%%%s · %s/%s tokens",
		bar, color, int(pct*100), ansiReset,
		formatTokens(used), formatTokens(window))
}

// formatTokens renders a token count compactly: 172000 -> "172k".
func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// shouldCompact reports whether the current usage has crossed the auto-compact
// threshold for the given window.
func shouldCompact(used, window int) bool {
	if window <= 0 {
		return false
	}
	return float64(used)/float64(window) >= compactThreshold
}
