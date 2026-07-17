package agent

import (
	"fmt"
	"strings"
	"time"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/theme"
)

// ANSI color constants for terminal rendering. A curated palette rather than
// raw escapes scattered through the code — keeps the look consistent and easy
// to retune.
const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiItalic    = "\033[3m"
	ansiUnderline = "\033[4m"
	ansiCyan      = theme.ANSIPrimary
	ansiGreen     = theme.ANSISuccess
	ansiYellow    = theme.ANSIWarning
	ansiGray      = theme.ANSISubtleText
	ansiDarkGray  = theme.ANSIMutedText
	ansiWhite     = theme.ANSIText
	ansiRed       = theme.ANSIDanger
	ansiMagenta   = theme.ANSIAccent
	ansiBlue      = theme.ANSIBlue
	ansiOrange    = theme.ANSIOrange
	ansiBgGray    = theme.ANSISurface
	ansiBgBlue    = theme.ANSIPanel
)

// divider prints a soft horizontal rule the width of the content area.
func divider() string {
	return ansiDarkGray + "  " + strings.Repeat("─", 44) + ansiReset
}

// renderMarkdown applies terminal formatting to markdown text.
// Handles: ```code blocks``` (with optional language label), headers, lists,
// numbered lists, blockquotes, and inline **bold** / `code`.
func renderMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inCodeBlock := false
	var codeLang string

	flushCode := func(body []string) {
		// Left-rail code block: a short top marker with the language label,
		// each body line prefixed by a guiding "│", and a short bottom marker.
		// No right border and no fixed width — this adapts to any line length
		// without padding waste or broken alignment on long lines.
		label := strings.TrimSpace(codeLang)
		if label == "" {
			label = "code"
		}
		out = append(out, ansiDarkGray+"  ┌── "+ansiCyan+label+ansiDarkGray+" ──────────────────────────"+ansiReset)
		for _, l := range body {
			out = append(out, ansiDarkGray+"  │ "+ansiReset+ansiGreen+l+ansiReset)
		}
		out = append(out, ansiDarkGray+"  └──────────────────────────────────────"+ansiReset)
	}

	var codeBody []string
	// inTable tracks whether we're accumulating markdown table rows, buffered
	// until the table ends (a blank/non-pipe line) so we can render it aligned.
	var tableRows []string
	inTable := false

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		rendered := renderTable(tableRows)
		out = append(out, rendered...)
		tableRows = tableRows[:0]
	}

	// headerBlank adds a blank line before a header (except at the very start)
	// so headers breathe instead of butting against the previous paragraph.
	headerBlank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}

	for _, line := range lines {
		// Fenced code block toggle. The text after ``` is the language hint.
		if strings.HasPrefix(line, "```") {
			flushTable()
			inTable = false
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimPrefix(line, "```")
				codeBody = codeBody[:0]
			} else {
				inCodeBlock = false
				flushCode(codeBody)
			}
			continue
		}
		if inCodeBlock {
			codeBody = append(codeBody, line)
			continue
		}

		// Markdown table: a line whose trimmed form starts and ends with '|'.
		// Buffer rows (including the |---| separator) until the table ends.
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "|") && strings.HasSuffix(trim, "|") && strings.Contains(trim, "|") {
			inTable = true
			tableRows = append(tableRows, trim)
			continue
		}
		if inTable {
			flushTable()
			inTable = false
			// fall through to handle this line normally
		}

		// Blockquote — use a distinct rail (▎) so it doesn't collide with code
		// block's │.
		if strings.HasPrefix(line, "> ") {
			out = append(out, ansiDarkGray+"  ▎ "+ansiItalic+ansiGray+renderInline(strings.TrimPrefix(line, "> "))+ansiReset)
			continue
		}

		// Headers — each level gets a distinct accent + marker. Order matters:
		// check the more specific (longer) prefixes first so "#### " isn't
		// swallowed by "## ". Each case MUST continue, else the matched line
		// also falls through to the "normal text" branch and gets printed twice.
		switch {
		case strings.HasPrefix(line, "#### "):
			headerBlank()
			out = append(out, "  "+ansiBold+ansiGray+"· "+line[5:]+ansiReset)
			continue
		case strings.HasPrefix(line, "### "):
			headerBlank()
			out = append(out, "  "+ansiBold+ansiCyan+"▸ "+line[4:]+ansiReset)
			continue
		case strings.HasPrefix(line, "## "):
			headerBlank()
			out = append(out, "  "+ansiBold+ansiBlue+"■ "+line[3:]+ansiReset)
			continue
		case strings.HasPrefix(line, "# "):
			headerBlank()
			out = append(out, "  "+ansiBold+ansiMagenta+"◆ "+line[2:]+ansiReset)
			continue
		}

		// Bullet list (support indentation)
		trimmed := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(trimmed)]
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			out = append(out, indent+"  "+ansiCyan+"•"+ansiReset+" "+renderInline(trimmed[2:]))
			continue
		}
		// Numbered list — support multi-digit numbers like "10."
		if num, rest, ok := splitNumbered(trimmed); ok {
			out = append(out, indent+"  "+ansiYellow+num+ansiReset+" "+renderInline(rest))
			continue
		}

		// Empty line
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}

		// Normal text with inline formatting
		out = append(out, "  "+renderInline(line))
	}
	flushTable() // table at end of input

	// Unterminated code block (model forgot closing fence) — still render it.
	if inCodeBlock {
		flushCode(codeBody)
	}
	return strings.Join(out, "\n")
}

// RenderMarkdownForTUI exposes the same terminal markdown renderer to the
// resume UI so saved assistant messages look identical to live responses.
func RenderMarkdownForTUI(text string) string {
	return renderMarkdown(text)
}

// renderInline handles **bold**, `code`, and *italic* within a line.
func renderInline(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		// **bold**
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "**")
			if end >= 0 {
				result.WriteString(ansiBold + s[i+2:i+2+end] + ansiReset)
				i = i + 2 + end + 2
				continue
			}
		}
		// `inline code` — padded with thin spaces so the highlight reads as a
		// pill without breaking word spacing.
		if s[i] == '`' {
			end := strings.Index(s[i+1:], "`")
			if end >= 0 {
				result.WriteString(ansiBgGray + ansiGreen + s[i+1:i+1+end] + ansiReset)
				i = i + 1 + end + 1
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// splitNumbered checks if s starts with a number followed by '.' or ')', like
// "10." or "3)". Returns the label (e.g. "10."), the remaining text, and ok.
func splitNumbered(s string) (label, rest string, ok bool) {
	j := 0
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == 0 || j >= len(s) {
		return "", "", false
	}
	if s[j] != '.' && s[j] != ')' {
		return "", "", false
	}
	label = s[:j+1]
	rest = strings.TrimLeft(s[j+1:], " ")
	return label, rest, true
}

const maxTerminalTableWidth = 76

// renderTable turns buffered markdown rows into a bounded, bordered terminal
// table. Cells wrap inside their columns, so the chat viewport never has to
// hard-wrap a completed table row and separate content from its borders.
func renderTable(rows []string) []string {
	if len(rows) == 0 {
		return nil
	}
	// Split each row into cells. Row 1 = header, row 2 = separator, rest = data.
	var grid [][]string
	for _, r := range rows {
		r = strings.Trim(r, "|")
		cells := strings.Split(r, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		grid = append(grid, cells)
	}
	// Drop the separator row (all dashes/colons).
	if len(grid) >= 2 && isSeparatorRow(grid[1]) {
		grid = append(grid[:1], grid[2:]...)
	}
	if len(grid) == 0 {
		return nil
	}
	ncol := 0
	for _, row := range grid {
		if len(row) > ncol {
			ncol = len(row)
		}
	}
	if ncol == 0 {
		return nil
	}
	for rowIndex := range grid {
		for len(grid[rowIndex]) < ncol {
			grid[rowIndex] = append(grid[rowIndex], "")
		}
		for column := range grid[rowIndex] {
			grid[rowIndex][column] = cleanTableCell(grid[rowIndex][column])
		}
	}

	width := make([]int, ncol)
	for _, row := range grid {
		for i, c := range row {
			if w := displayWidth(c); w > width[i] {
				width[i] = w
			}
		}
	}
	// Visible table overhead: "  │ " + inter-column " │ " + final " │".
	contentBudget := maxTerminalTableWidth - 6 - 3*(ncol-1)
	if contentBudget < ncol*4 {
		contentBudget = ncol * 4
	}
	for column := range width {
		if width[column] < 6 {
			width[column] = 6
		}
		if width[column] > 40 {
			width[column] = 40
		}
	}
	shrinkTableColumns(width, contentBudget, 6)

	border := func(left, middle, right string) string {
		var b strings.Builder
		b.WriteString(ansiDarkGray + "  " + left + "─")
		for column, columnWidth := range width {
			b.WriteString(strings.Repeat("─", columnWidth))
			if column < ncol-1 {
				b.WriteString("─" + middle + "─")
			}
		}
		b.WriteString("─" + right + ansiReset)
		return b.String()
	}
	pad := func(value string, target int) string {
		return value + strings.Repeat(" ", max(0, target-displayWidth(value)))
	}

	out := []string{border("╭", "┬", "╮")}
	for rowIndex, row := range grid {
		wrapped := make([][]string, ncol)
		rowHeight := 1
		for column := 0; column < ncol; column++ {
			wrapped[column] = wrapTableCell(row[column], width[column])
			if len(wrapped[column]) > rowHeight {
				rowHeight = len(wrapped[column])
			}
		}
		for visualLine := 0; visualLine < rowHeight; visualLine++ {
			var b strings.Builder
			b.WriteString(ansiDarkGray + "  │ " + ansiReset)
			for column := 0; column < ncol; column++ {
				cell := ""
				if visualLine < len(wrapped[column]) {
					cell = wrapped[column][visualLine]
				}
				cell = pad(cell, width[column])
				if rowIndex == 0 {
					b.WriteString(ansiBold + ansiWhite + cell + ansiReset)
				} else {
					b.WriteString(ansiGray + cell + ansiReset)
				}
				if column < ncol-1 {
					b.WriteString(ansiDarkGray + " │ " + ansiReset)
				}
			}
			b.WriteString(ansiDarkGray + " │" + ansiReset)
			out = append(out, b.String())
		}
		switch {
		case rowIndex == 0:
			out = append(out, border("├", "┼", "┤"))
		case rowIndex < len(grid)-1:
			out = append(out, border("├", "┼", "┤"))
		}
	}
	out = append(out, border("╰", "┴", "╯"))
	return out
}

func cleanTableCell(value string) string {
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "**", "")
	return strings.Join(strings.Fields(value), " ")
}

func shrinkTableColumns(widths []int, budget, minimum int) {
	for totalInts(widths) > budget {
		largest := -1
		for index, width := range widths {
			if width > minimum && (largest < 0 || width > widths[largest]) {
				largest = index
			}
		}
		if largest < 0 {
			return
		}
		widths[largest]--
	}
}

func totalInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func wrapTableCell(value string, width int) []string {
	if value == "" {
		return []string{""}
	}
	wrapped := strings.Split(xansi.WrapWc(value, width, "/\\_-"), "\n")
	for index := range wrapped {
		wrapped[index] = strings.TrimSpace(wrapped[index])
		if displayWidth(wrapped[index]) > width {
			wrapped[index] = xansi.Truncate(wrapped[index], width, "")
		}
	}
	return wrapped
}

// isSeparatorRow reports whether a row is the markdown table alignment row
// (cells containing only -, :, spaces).
func isSeparatorRow(row []string) bool {
	for _, c := range row {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		for _, r := range c {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}
	}
	return true
}

// displayWidth returns the terminal column width of s: CJK and other wide
// (fullwidth/wide) runes count as 2, everything else as 1. Good enough for
// table alignment — we don't handle combining marks or zero-width chars.
func displayWidth(s string) int {
	return xansi.StringWidth(s)
}

// isWide reports whether r is a double-width rune (CJK ideographs, fullwidth
// forms, etc.) per common Unicode ranges.
func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // Hangul Jamo
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK radicals, Kangxi
		return true
	case r >= 0x3040 && r <= 0x33FF: // Hiragana, Katakana, CJK symbols
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Ext A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0xA000 && r <= 0xA4CF: // Yi
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // Hangul Syllables
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	case r >= 0xFE30 && r <= 0xFE4F: // CJK Compatibility Forms
		return true
	case r >= 0xFF00 && r <= 0xFF60: // Fullwidth Forms
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // Fullwidth signs
		return true
	case r >= 0x1F300 && r <= 0x1FAFF: // Emoji / symbols
		return true
	}
	return false
}

// printAssistantMessage renders the AI's text output with a role badge and
// markdown formatting, framed to stand apart from tool output.
func printAssistantMessage(text string) {
	fmt.Println()
	// Role badge — a small colored pill.
	fmt.Println("  " + ansiBgBlue + ansiBold + ansiWhite + " ✦ assistant " + ansiReset)
	fmt.Println()
	fmt.Println(renderMarkdown(text))
	fmt.Println()
}

// printUserPrompt shows the styled user input prompt with a session footer.
func printUserPrompt(elapsed string, tokens int) {
	fmt.Println()
	fmt.Println(divider())
	fmt.Printf("  "+ansiDarkGray+"⏱ %s  ·  ◆ %d tokens"+ansiReset+"\n", elapsed, tokens)
	fmt.Println()
	fmt.Print("  " + ansiBold + ansiYellow + "❯ " + ansiReset)
}

// toolIcon returns the emoji + accent color for a tool action.
func toolIcon(action string) (icon, color string) {
	switch action {
	case "Read":
		return "📖", ansiCyan
	case "Write":
		return "📝", ansiGreen
	case "Edit":
		return "✏️", ansiYellow
	case "Search":
		return "🔍", ansiMagenta
	case "Run":
		return "⚙", ansiOrange
	case "List":
		return "📁", ansiBlue
	case "Memory":
		return "🧠", ansiMagenta
	default:
		return "•", ansiGray
	}
}

// printToolHeader shows a tool call in a compact bordered format with an
// icon and accent color matched to the tool kind.
func printToolHeader(icon, action, detail string) {
	_, color := toolIcon(action)
	fmt.Printf("\n    "+ansiDarkGray+"╭─"+ansiReset+" %s "+color+ansiBold+"%s"+ansiReset+"\n", icon, action)
	if strings.TrimSpace(detail) != "" {
		fmt.Printf("    "+ansiDarkGray+"│"+ansiReset+"  "+ansiGray+"%s"+ansiReset+"\n", detail)
	}
}

// printToolFooter shows tool result status with a colored glyph.
func (a *Agent) printToolFooter(success bool, msg string) {
	if success {
		fmt.Printf("    "+ansiDarkGray+"╰─"+ansiReset+" "+ansiGreen+"✓"+ansiReset+" "+ansiGray+"%s"+ansiReset+"\n", msg)
	} else {
		fmt.Printf("    "+ansiDarkGray+"╰─"+ansiReset+" "+ansiRed+"✗"+ansiReset+" "+ansiRed+"%s"+ansiReset+"\n", msg)
	}
	if a.UIMode {
		fmt.Println()
	} else {
		fmt.Print("  " + ansiCyan + "❯" + ansiReset + "\n") // persistent prompt (shell mode only)
	}
}

// toolDurationLabel renders an elapsed duration compactly for tool footers.
func toolDurationLabel(d time.Duration) string {
	if d < time.Millisecond {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
