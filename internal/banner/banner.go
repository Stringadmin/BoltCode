// Package banner renders the BoltCode startup logo.
package banner

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/theme"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	yellow = theme.ANSIWarning
	white  = theme.ANSIText
	green  = theme.ANSISuccess
	cyan   = theme.ANSIPrimary
	gray   = theme.ANSIBannerMuted
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
	n := visibleWidth(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// visibleWidth returns terminal cell width, ignoring ANSI escape sequences and
// correctly accounting for wide glyphs such as emoji and CJK characters.
func visibleWidth(s string) int {
	return ansi.StringWidth(s)
}

func printColor(version string) {
	fmt.Println()
	for _, line := range colorBannerLines(version) {
		fmt.Println(line)
	}
	fmt.Println()
}

func colorBannerLines(version string) []string {
	b := cyan
	l := yellow + bold
	g := green
	d := gray
	r := reset

	row := func(content, color string) string {
		// Pad the visible width (color codes excluded) so the right border
		// lines up even when content is colored.
		vis := visibleWidth(content)
		pad := w - vis
		if pad < 0 {
			pad = 0
		}
		return b + "│" + color + content + strings.Repeat(" ", pad) + r + b + "│" + r
	}
	top := b + "╭" + strings.Repeat("─", w) + "╮" + r
	bot := b + "╰" + strings.Repeat("─", w) + "╯" + r

	lines := []string{top, row("", r)}
	// Classic figlet-style wordmark (Standard font) — retro DOS/CLI feel.
	// Double-line block chars (╗╔╝║) throughout, like an old BBS banner.
	// Each line is exactly 34 visible chars so the right edge lines up.
	lines = append(lines,
		row("  ██████╗  ██████╗ ██╗     ████████╗", l),
		row("  ██╔══██╗██╔═══██╗██║     ╚══██╔══╝", l),
		row("  ██████╔╝██║   ██║██║        ██║   ", l),
		row("  ██╔══██╗██║   ██║██║        ██║   ", l),
		row("  ██████╔╝╚██████╔╝███████╗   ██║   ", l),
		row("  ╚═════╝  ╚═════╝ ╚══════╝   ╚═╝   ", l),
		row("", r),
	)
	// Retro sci-fi info panel (Hermes-style bordered dashboard). Each content
	// line is padded to a fixed inner width so the right border lines up.
	const inner = 42
	// panelLine wraps a content string in the inner box and pads it out to the
	// outer frame width (w) so the outer right border lines up on every row.
	panelLine := func(content string) string {
		// content already includes its own coloring; pad the visible width.
		vis := visibleWidth(content)
		pad := w - vis
		if pad < 0 {
			pad = 0
		}
		return b + "│" + content + strings.Repeat(" ", pad) + b + "│" + r
	}
	topLine := "  ╭" + strings.Repeat("─", inner+2) + "╮"
	midLine := func(text, color string) string {
		text = ansi.Truncate(text, inner, "…")
		vis := visibleWidth(text)
		pad := inner - vis
		if pad < 0 {
			pad = 0
		}
		return d + "  │ " + color + text + r + strings.Repeat(" ", pad) + d + " │" + r
	}
	botLine := "  ╰" + strings.Repeat("─", inner+2) + "╯"
	lines = append(lines,
		panelLine(d+topLine+r),
		panelLine(midLine("> collaborative AI agent programming", g)),
		panelLine(midLine("  human-in-the-loop · git-native · ⚡", d)),
	)
	if version != "" {
		lines = append(lines, panelLine(midLine("  v"+version+"  ·  type 'help' for commands", d)))
	} else {
		lines = append(lines, panelLine(midLine("  type 'help' for commands", d)))
	}
	lines = append(lines, panelLine(d+botLine+r), row("", r), bot)
	return lines
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
