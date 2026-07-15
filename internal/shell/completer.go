package shell

import (
	"fmt"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"

	"github.com/chzyer/readline"
)

// slashCompleter provides Tab completion for agent slash commands during an
// agent session. It swaps in for the shell's normal command completer so that
// typing "/" + Tab lists/highlights the available slash commands.
type slashCompleter struct{}

func (slashCompleter) Do(line []rune, pos int) (newLine [][]rune, newPos int) {
	// Only complete when the line starts with "/".
	text := string(line[:pos])
	if !strings.HasPrefix(text, "/") {
		return nil, pos
	}
	// Complete the word currently being typed (the part after the last space).
	wordStart := strings.LastIndex(text, " ") + 1
	word := text[wordStart:]
	var matches [][]rune
	for _, cmd := range agent.SlashCommands {
		if strings.HasPrefix(cmd.Name, word) {
			// Return the completion suffix (without the already-typed prefix).
			suffix := cmd.Name[len(word):]
			matches = append(matches, []rune(suffix))
		}
	}
	return matches, pos
}

// newSlashCompleter builds the readline PrefixCompleter tree for slash commands,
// which gives nicer tab-completion display (grouped list) than the raw Do func.
func newSlashCompleter() *readline.PrefixCompleter {
	items := []readline.PrefixCompleterInterface{}
	for _, cmd := range agent.SlashCommands {
		items = append(items, readline.PcItem(cmd.Name))
	}
	return readline.NewPrefixCompleter(items...)
}

// printSlashCommandPreview shows the available slash commands as a highlighted
// list, triggered when the user types just "/" + Enter. This gives a visible
// "did I type it right?" cue that plain readline lacks (it can't show live
// hints while typing).
func printSlashCommandPreview() {
	fmt.Println()
	fmt.Println("  " + infoStyle.Render("slash commands (Tab to complete):"))
	// Compute column width for alignment.
	maxLen := 0
	for _, cmd := range agent.SlashCommands {
		if len(cmd.Name) > maxLen {
			maxLen = len(cmd.Name)
		}
	}
	for _, cmd := range agent.SlashCommands {
		pad := maxLen - len(cmd.Name)
		fmt.Printf("  %s%s%s  %s%s%s\n",
			"\033[38;5;75m", cmd.Name, "\033[0m",
			strings.Repeat(" ", pad),
			"\033[38;5;241m", cmd.Desc+"\033[0m")
	}
	fmt.Println()
}

// ansi256Replacements maps the 256-color escape sequences used by the agent
// package to basic ANSI equivalents. The chzyer/readline Windows ANSI writer
// has an 8-entry color table and panics (index out of range) on 38;5;N codes
// where N >= 8. This map covers every 256-color code used in render.go.
var ansi256Replacements = []struct{ from, to string }{
	{"\033[38;5;78m", "\033[32m"},  // green
	{"\033[38;5;220m", "\033[33m"}, // yellow
	{"\033[38;5;245m", "\033[37m"}, // gray / white
	{"\033[38;5;241m", "\033[90m"}, // dark gray
	{"\033[38;5;196m", "\033[31m"}, // red
	{"\033[38;5;75m", "\033[36m"},  // cyan/blue
}

// sanitizeANSIForReadline replaces 256-color ANSI codes with basic ANSI
// equivalents so the text can safely pass through readline's stdout writer
// on Windows without triggering a panic.
func sanitizeANSIForReadline(s string) string {
	for _, r := range ansi256Replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}
