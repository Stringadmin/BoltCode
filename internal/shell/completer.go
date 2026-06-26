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

