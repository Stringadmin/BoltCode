package agent

import (
	"strings"
	"testing"
)

func TestRenderInlineBold(t *testing.T) {
	got := renderInline("hello **world** end")
	if !strings.Contains(got, ansiBold+"world"+ansiReset) {
		t.Errorf("expected bold span, got %q", got)
	}
}

func TestRenderInlineCode(t *testing.T) {
	got := renderInline("see `foo` here")
	// Compact pill: no surrounding spaces so word spacing stays natural.
	if !strings.Contains(got, ansiGreen+"foo"+ansiReset) {
		t.Errorf("expected inline code span, got %q", got)
	}
}

func TestRenderMarkdownHeaders(t *testing.T) {
	out := renderMarkdown("# Title\n## Sub\n### Minor")
	// Each header level should render without dropping the text.
	for _, want := range []string{"Title", "Sub", "Minor"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderMarkdown dropped header %q in: %q", want, out)
		}
	}
}

// TestRenderMarkdownHeadersNotDuplicated verifies header lines don't get
// printed twice — once by the header case and again as "normal text".
// The raw markdown prefix ("# ", "## ", etc.) must not appear.
func TestRenderMarkdownHeadersNotDuplicated(t *testing.T) {
	out := renderMarkdown("## Project Overview\n正文内容")
	// The rendered marker (e.g. ■) should appear, not the raw "## ".
	if strings.Contains(out, "## ") {
		t.Errorf("raw '## ' leaked into rendered output:\n%s", out)
	}
	// "Project Overview" must appear exactly once.
	if strings.Count(out, "Project Overview") != 1 {
		t.Errorf("header text count = %d, want 1:\n%s", strings.Count(out, "Project Overview"), out)
	}
	if strings.Count(out, "正文内容") != 1 {
		t.Errorf("body text count = %d, want 1:\n%s", strings.Count(out, "正文内容"), out)
	}
	// h1, h4 too.
	out2 := renderMarkdown("# Top\n#### Minor\nbody")
	if strings.Contains(out2, "# Top") || strings.Contains(out2, "# Minor") {
		t.Errorf("raw '#' leaked: %q", out2)
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	out := renderMarkdown("```go\nfmt.Println(\"hi\")\n```")
	if !strings.Contains(out, "go") {
		t.Errorf("code block language label missing: %q", out)
	}
	if !strings.Contains(out, "fmt.Println") {
		t.Errorf("code block body missing: %q", out)
	}
	// Left-rail style: top marker "┌──", body "│", bottom "└──".
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
		t.Errorf("code block markers missing: %q", out)
	}
	if !strings.Contains(out, "│ ") {
		t.Errorf("code body should be prefixed with left rail: %q", out)
	}
}

// TestRenderMarkdownCodeBlockLongLine verifies long code lines aren't clipped
// or broken by a fixed-width border — the left-rail style adapts to any width.
func TestRenderMarkdownCodeBlockLongLine(t *testing.T) {
	long := "this is a very long line of code that would exceed any fixed border width and must not be truncated"
	out := renderMarkdown("```ts\n" + long + "\n```")
	if !strings.Contains(out, long) {
		t.Errorf("long code line was truncated: %q", out)
	}
}

func TestRenderMarkdownTable(t *testing.T) {
	out := renderMarkdown("| A | B |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |")
	// Header and data cells present, separator row consumed.
	for _, want := range []string{"A", "B", "1", "2", "3", "4"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q: %s", want, out)
		}
	}
	// Should have a border between header and data.
	if !strings.Contains(out, "┼") {
		t.Errorf("table missing column separator: %s", out)
	}
	// The markdown separator dashes must NOT leak through as a data row.
	if strings.Contains(out, "│ ---") || strings.Contains(out, "--- │") {
		t.Errorf("separator row leaked: %s", out)
	}
}

func TestRenderMarkdownNumberedMultiDigit(t *testing.T) {
	out := renderMarkdown("10. 第十\n11. 第十一")
	if !strings.Contains(out, "10.") || !strings.Contains(out, "11.") {
		t.Errorf("multi-digit numbered list wrong: %s", out)
	}
}

func TestDisplayWidthCJK(t *testing.T) {
	if displayWidth("ab") != 2 {
		t.Errorf("ascii width wrong")
	}
	if displayWidth("中文") != 4 {
		t.Errorf("CJK width = %d, want 4", displayWidth("中文"))
	}
	if displayWidth("a中") != 3 {
		t.Errorf("mixed width = %d, want 3", displayWidth("a中"))
	}
}

func TestRenderMarkdownUnterminatedCodeBlock(t *testing.T) {
	// A missing closing fence must still render the accumulated body, not panic.
	out := renderMarkdown("```py\nprint(1)\n")
	if !strings.Contains(out, "print(1)") {
		t.Errorf("unterminated code block body lost: %q", out)
	}
}

func TestStripAnsi(t *testing.T) {
	in := "\033[38;5;75mhello\033[0m world"
	if got := stripAnsi(in); got != "hello world" {
		t.Errorf("stripAnsi = %q, want %q", got, "hello world")
	}
}

func TestVisibleWidth(t *testing.T) {
	if w := visibleWidth(ansiCyan + "abc" + ansiReset); w != 3 {
		t.Errorf("visibleWidth with escapes = %d, want 3", w)
	}
}

func TestStatRowDoesNotPanicAndAligns(t *testing.T) {
	// Just ensure the row builds and ends with a closing border rune.
	row := statRow("tokens", "100 in · 50 out")
	if !strings.Contains(row, "│") {
		t.Errorf("statRow missing border: %q", row)
	}
}

func TestStatsDisplayContainsAllMetrics(t *testing.T) {
	s := NewStats()
	s.Add("claude-sonnet-4-20250514", 100, 50)
	s.AddToolCall()
	s.AddToolCall()
	out := s.Display()
	for _, want := range []string{"session summary", "tokens", "rounds", "tools", "150"} {
		if !strings.Contains(out, want) {
			t.Errorf("Display missing %q in: %q", want, out)
		}
	}
	if !strings.Contains(out, "cost") {
		t.Errorf("Display should include cost for known model, got: %q", out)
	}
}

func TestStatsDisplayAlignsLargeTokenCounts(t *testing.T) {
	s := NewStats()
	s.InputTokens = 764805
	s.OutputTokens = 5629
	s.Rounds = 13
	s.ToolCalls = 55
	s.TotalCost = 2.3789
	s.SetCumulative(&CumulativeUsage{SessionCount: 5, TotalCost: 2.7126})

	lines := strings.Split(s.Display(), "\n")
	wantWidth := visibleWidth(lines[0])
	for i, line := range lines[1:] {
		if got := visibleWidth(line); got != wantWidth {
			t.Errorf("line %d width = %d, want %d: %q", i+2, got, wantWidth, stripAnsi(line))
		}
	}
}
