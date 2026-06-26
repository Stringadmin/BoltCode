package agent

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// typewrite prints text with a typing animation effect.
func typewrite(text string, delay time.Duration) {
	for _, ch := range text {
		fmt.Fprint(os.Stdout, string(ch))
		time.Sleep(delay)
	}
}

// AnimateSessionStart shows a brief animated intro when the agent starts.
func AnimateSessionStart(intent, model, worktree string) {
	fmt.Println()
	const width = 44

	// Animated top border, drawn left-to-right.
	top := "  ╭" + strings.Repeat("─", width) + "╮"
	for _, ch := range top {
		fmt.Print(string(ch))
		time.Sleep(4 * time.Millisecond)
	}
	fmt.Println()

	// Title line with typewriter.
	fmt.Print("  " + ansiDarkGray + "│" + ansiReset + " ")
	typewrite(ansiBold+ansiWhite+"⚡ BoltCode Agent "+ansiReset, 18*time.Millisecond)
	fmt.Print(ansiYellow + "(✧ω✧)" + ansiReset)
	// Pad to the right border. Visible content: " " + "⚡ BoltCode Agent " + "(✧ω✧)"
	titleVisible := 1 + len([]rune("⚡ BoltCode Agent ")) + len([]rune("(✧ω✧)"))
	pad := 44 - titleVisible
	if pad < 0 {
		pad = 0
	}
	fmt.Println(strings.Repeat(" ", pad) + ansiDarkGray + "│" + ansiReset)

	// Separator
	fmt.Println("  " + ansiDarkGray + "├" + strings.Repeat("─", width) + "┤" + ansiReset)

	// Info rows with aligned labels.
	infoRow("task", intent, ansiYellow, "⚡")
	infoRow("model", model, ansiMagenta, "🤖")
	infoRow("worktree", worktree, ansiGray, "📂")

	// Bottom border, drawn left-to-right.
	bot := "  ╰" + strings.Repeat("─", width) + "╯"
	for _, ch := range bot {
		fmt.Print(ansiDarkGray + string(ch) + ansiReset)
		time.Sleep(4 * time.Millisecond)
	}
	fmt.Println()
	fmt.Println()
}

// infoRow prints a labeled row inside the start panel, padding to the panel
// width and wrapping the right border.
func infoRow(label, value, color, icon string) {
	const inner = 44
	prefix := " " + icon + " " + color + label + ansiReset + ansiDarkGray + ":" + ansiReset + " "
	visible := len([]rune(stripAnsi(prefix))) + len([]rune(value))
	pad := inner - visible
	if pad < 0 {
		pad = 0
	}
	fmt.Printf("  %s│%s%s%s%s%s%s\n",
		ansiDarkGray, ansiReset, prefix, ansiWhite, value, ansiReset,
		strings.Repeat(" ", pad)+ansiDarkGray+"│"+ansiReset)
}

// AnimateSessionEnd shows a celebration when the agent finishes.
func AnimateSessionEnd() {
	fmt.Println()
	frames := []string{
		"  ✧",
		"  ✧ ･ﾟ",
		"  ✧ ･ﾟ: *",
		"  ✧ ･ﾟ: *✧",
		"  ✧ ･ﾟ: *✧ ･ﾟ:*",
		"  ✧ ･ﾟ: *✧ ･ﾟ:* done",
		"  ✧ ･ﾟ: *✧ ･ﾟ:* done ٩(◕‿◕｡)۶",
	}
	for _, f := range frames {
		fmt.Print("\r" + ansiYellow + f + ansiReset)
		time.Sleep(80 * time.Millisecond)
	}
	fmt.Println()
	fmt.Println("  " + ansiGreen + "✓ session complete — review your work, then submit" + ansiReset)
	fmt.Println()
}
