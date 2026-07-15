// Package banner renders the BoltCode startup logo.
package banner

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	yellow = "\033[38;5;220m"
	white  = "\033[97m"
	green  = "\033[38;5;78m"
	cyan   = "\033[38;5;75m"
	gray   = "\033[38;5;242m"
)

const w = 59

func Print(version string) {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		printPlain(version)
		return
	}
	printColor(version)
}

func p(s string) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// visibleWidth returns the display width of s, ignoring ANSI escape sequences.
// Used to pad lines that contain color codes so borders line up correctly.
func visibleWidth(s string) int {
	n := 0
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
		n++
	}
	return n
}

func printColor(version string) {
	b := cyan
	l := yellow + bold
	g := green
	d := gray
	r := reset

	row := func(content, color string) {
		// Pad the visible width (color codes excluded) so the right border
		// lines up even when content is colored.
		vis := visibleWidth(content)
		pad := w - vis
		if pad < 0 {
			pad = 0
		}
		fmt.Println(b + "│" + color + content + strings.Repeat(" ", pad) + r + b + "│" + r)
	}
	top := b + "╭" + strings.Repeat("─", w) + "╮" + r
	bot := b + "╰" + strings.Repeat("─", w) + "╯" + r

	fmt.Println()
	fmt.Println(top)
	row("", r)
	// Classic figlet-style wordmark (Standard font) — retro DOS/CLI feel.
	// Double-line block chars (╗╔╝║) throughout, like an old BBS banner.
	// Each line is exactly 34 visible chars so the right edge lines up.
	row("  ██████╗  ██████╗ ██╗     ████████╗", l)
	row("  ██╔══██╗██╔═══██╗██║     ╚══██╔══╝", l)
	row("  ██████╔╝██║   ██║██║        ██║   ", l)
	row("  ██╔══██╗██║   ██║██║        ██║   ", l)
	row("  ██████╔╝╚██████╔╝███████╗   ██║   ", l)
	row("  ╚═════╝  ╚═════╝ ╚══════╝   ╚═╝   ", l)
	row("", r)
	// Retro sci-fi info panel (Hermes-style bordered dashboard). Each content
	// line is padded to a fixed inner width so the right border lines up.
	const inner = 42
	// panelLine wraps a content string in the inner box and pads it out to the
	// outer frame width (w) so the outer right border lines up on every row.
	panelLine := func(content string) {
		// content already includes its own coloring; pad the visible width.
		vis := visibleWidth(content)
		pad := w - 2 - vis // w - "  " prefix - content
		if pad < 0 {
			pad = 0
		}
		fmt.Println(b + "│" + content + strings.Repeat(" ", pad) + b + "│" + r)
	}
	topLine := "  ╭" + strings.Repeat("─", inner+2) + "╮"
	midLine := func(text, color string) string {
		vis := len([]rune(text))
		pad := inner - vis
		if pad < 0 {
			pad = 0
		}
		return d + "  │ " + color + text + r + strings.Repeat(" ", pad) + d + " │" + r
	}
	botLine := "  ╰" + strings.Repeat("─", inner+2) + "╯"
	panelLine(d + topLine + r)
	panelLine(midLine("> collaborative AI agent programming", g))
	panelLine(midLine("  human-in-the-loop · git-native · ⚡", d))
	if version != "" {
		panelLine(midLine("  v"+version+"  ·  type 'help' for commands", d))
	} else {
		panelLine(midLine("  type 'help' for commands", d))
	}
	panelLine(d + botLine + r)
	row("", r)
	fmt.Println(bot)
	fmt.Println()
	_ = d
}

func printPlain(version string) {
	fmt.Println()
	fmt.Println("╭" + strings.Repeat("─", w) + "╮")
	fmt.Println("│" + p("  BOLTCODE") + "│")
	fmt.Println("│" + p("  collaborative AI agent programming") + "│")
	if version != "" {
		fmt.Println("│" + p("  v"+version) + "│")
	}
	fmt.Println("╰" + strings.Repeat("─", w) + "╯")
	fmt.Println()
}
