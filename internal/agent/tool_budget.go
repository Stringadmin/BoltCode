package agent

const (
	minToolResultRunes = 4_096
	maxToolResultRunes = 32_000
)

// toolResultRuneLimit reserves roughly five percent of the active model's
// context window for one tool result. The token estimator uses about three
// characters per token, hence window * 0.05 * 3 = window * 0.15 runes.
// Clamps keep small models usable and prevent a single result from dominating
// even a very large context window.
func (a *Agent) toolResultRuneLimit() int {
	limit := a.ContextWindow() * 3 / 20
	if limit < minToolResultRunes {
		return minToolResultRunes
	}
	if limit > maxToolResultRunes {
		return maxToolResultRunes
	}
	return limit
}

func (a *Agent) truncateToolOutput(value string) string {
	limit := a.toolResultRuneLimit()
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	marker := []rune("\n... (middle truncated to the active context budget) ...\n")
	available := limit - len(marker)
	if available < 2 {
		return string(runes[:limit])
	}
	// Most source and command output is understood from the beginning, while
	// exit codes and summaries are commonly at the end. Keep both.
	head := available * 3 / 4
	tail := available - head
	return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
}

// toolResultItemLimit converts the character budget into a conservative item
// count for searches and file listings. The final centralized rune truncation
// remains authoritative when real paths or matching lines are longer than the
// estimate.
func (a *Agent) toolResultItemLimit(estimatedRunes, minimum, maximum int) int {
	if estimatedRunes <= 0 {
		estimatedRunes = 1
	}
	limit := a.toolResultRuneLimit() / estimatedRunes
	if limit < minimum {
		return minimum
	}
	if limit > maximum {
		return maximum
	}
	return limit
}
