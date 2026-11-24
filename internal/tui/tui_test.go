package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/store"
)

func initApp(t *testing.T) *command.App {
	t.Helper()
	dir, err := os.MkdirTemp("", "tuicmd-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// Init a bare-bones git repo so Repo.Run works.
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "user.name", "tester")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	st, err := store.NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &command.App{
		Repo:  &gitx.Repo{Root: dir},
		Store: st,
	}
}

func TestNewModel(t *testing.T) {
	_, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestInitialView(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	view := m.View()
	if view == "" {
		t.Error("empty initial view")
	}
}

func TestQuitMessage(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if !m2.quitting {
		t.Error("expected quitting=true after Ctrl-C")
	}
}

func TestEmptyListDoesNotPanic(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, key := range []string{"up", "down", "enter", "a", "m", "r"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on key %q: %v", key, r)
				}
			}()
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}()
	}
}

func TestRemoveAt(t *testing.T) {
	props := []*model.Proposal{
		{ID: "p_001"}, {ID: "p_002"}, {ID: "p_003"},
	}
	got := removeAt(props, 1)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].ID != "p_001" || got[1].ID != "p_003" {
		t.Errorf("wrong elements after remove: %v", got)
	}
}

func TestRemoveAtFirst(t *testing.T) {
	props := []*model.Proposal{{ID: "p_001"}, {ID: "p_002"}}
	got := removeAt(props, 0)
	if len(got) != 1 || got[0].ID != "p_002" {
		t.Errorf("remove first: %v", got)
	}
}

func TestRemoveAtLast(t *testing.T) {
	props := []*model.Proposal{{ID: "p_001"}, {ID: "p_002"}}
	got := removeAt(props, 1)
	if len(got) != 1 || got[0].ID != "p_001" {
		t.Errorf("remove last: %v", got)
	}
}

func TestDiffViewWrapsLongLinesAndColorsHeaders(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	m := Model{
		proposals: []*model.Proposal{{ID: "p_1", Agent: "agent", Summary: "s"}},
		width:     40,
		height:    15,
		author:    "tester",
	}
	long := "+" + strings.Repeat("x", 100)
	m.diff = strings.Join([]string{"--- a/f", "+++ b/f", "@@ -1 +1 @@", "-old", "new", long}, "\n")
	m.diffLines = wrapDiff(m.diff, m.width)
	if got, want := len(m.diffLines), 5+3; got < want {
		t.Fatalf("long diff line was not wrapped: %d lines, want at least %d", got, want)
	}
	for i, line := range m.diffLines {
		if w := ansi.StringWidth(line); w > m.width {
			t.Fatalf("wrapped line %d overflows: %d > %d", i, w, m.width)
		}
	}

	m.current = viewDiff
	view := m.viewDiff()
	if !strings.Contains(view, diffHeadStyle.Render("--- a/f")) {
		t.Fatal("file header not rendered with the header style")
	}
	if strings.Contains(view, diffDelStyle.Render("--- a/f")) {
		t.Fatal("file header mis-colored as a deletion")
	}
	if !strings.Contains(view, diffAddStyle.Render("+"+strings.Repeat("x", 39))) {
		t.Fatal("added line lost its color after wrapping")
	}
}

func TestProposalListScrollsInWindow(t *testing.T) {
	proposals := make([]*model.Proposal, 0, 30)
	for i := 0; i < 30; i++ {
		proposals = append(proposals, &model.Proposal{ID: fmt.Sprintf("p_%02d", i)})
	}
	m := Model{proposals: proposals, width: 80, height: 12, author: "t"} // 6 visible rows
	step := func(msg tea.KeyMsg) {
		result, _ := m.Update(msg)
		m = result.(Model)
	}

	for i := 0; i < 10; i++ {
		step(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != 10 {
		t.Fatalf("cursor = %d, want 10", m.cursor)
	}
	if m.listOffset == 0 {
		t.Fatal("list window did not follow the cursor")
	}
	if view := m.viewList(); strings.Contains(view, "p_00") {
		t.Fatalf("scrolled-out proposal still rendered:\n%s", view)
	}

	step(tea.KeyMsg{Type: tea.KeyHome})
	if m.cursor != 0 || m.listOffset != 0 {
		t.Fatalf("home: cursor=%d offset=%d, want 0/0", m.cursor, m.listOffset)
	}
	step(tea.KeyMsg{Type: tea.KeyEnd})
	if m.cursor != 29 {
		t.Fatalf("end: cursor = %d, want 29", m.cursor)
	}
	if view := m.viewList(); !strings.Contains(view, "30/30") {
		t.Fatalf("position indicator missing:\n%s", view)
	}
}

func TestDiffScrollKeysAndPercent(t *testing.T) {
	m := Model{proposals: []*model.Proposal{{ID: "p_1"}}, width: 40, height: 12, author: "t"}
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf(" line %d", i))
	}
	m.diff = strings.Join(lines, "\n")
	m.diffLines = wrapDiff(m.diff, m.width)
	m.current = viewDiff
	step := func(msg tea.KeyMsg) {
		result, _ := m.updateDiff(msg)
		m = result.(Model)
	}

	page := m.diffVisibleRows()
	step(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.scroll != page {
		t.Fatalf("pgdown scroll = %d, want %d", m.scroll, page)
	}
	step(tea.KeyMsg{Type: tea.KeyEnd})
	if m.scroll != m.maxDiffScroll() {
		t.Fatalf("end scroll = %d, want %d", m.scroll, m.maxDiffScroll())
	}
	if view := m.viewDiff(); !strings.Contains(view, "100%") {
		t.Fatalf("scroll percent missing at the bottom:\n%s", view)
	}
	step(tea.KeyMsg{Type: tea.KeyHome})
	if m.scroll != 0 {
		t.Fatalf("home scroll = %d, want 0", m.scroll)
	}
	step(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.scroll != 0 {
		t.Fatalf("pgup above the top: scroll = %d, want 0", m.scroll)
	}
}

func TestDiffStatsCountsContentLines(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/x.go b/x.go",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -1,3 +1,4 @@",
		" context",
		"-old line",
		"+new line",
		"+another addition",
		"diff --git a/y.go b/y.go",
		"--- a/y.go",
		"+++ b/y.go",
		"@@ -5 +5 @@",
		"-gone",
	}, "\n")
	adds, dels, files := diffStats(diff)
	if adds != 2 || dels != 2 || files != 2 {
		t.Fatalf("diffStats = +%d -%d %d files, want +2 -2 2 files", adds, dels, files)
	}
}

func TestSelectedProposalRowFillsWidth(t *testing.T) {
	m := Model{
		proposals: []*model.Proposal{{ID: "p_1", Agent: "builtin", Summary: "short"}},
		width:     60,
		height:    12,
		author:    "tester",
	}
	for _, line := range strings.Split(m.viewList(), "\n") {
		if w := ansi.StringWidth(line); w > m.width {
			t.Fatalf("list row overflows: %d > %d", w, m.width)
		}
	}
	// The cursor row is padded to the full width for a solid selection bar.
	found := false
	for _, line := range strings.Split(m.viewList(), "\n") {
		if strings.Contains(line, "p_1") && ansi.StringWidth(line) == m.width {
			found = true
		}
	}
	if !found {
		t.Fatal("selected proposal row is not padded to full width")
	}
}

func TestDiffHeaderShowsMetaAndStats(t *testing.T) {
	m := Model{
		proposals: []*model.Proposal{{ID: "p_1", Agent: "builtin", Summary: "add endpoint"}},
		width:     60,
		height:    15,
		author:    "tester",
	}
	m.diff = "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-x\n+y\n+z\n"
	m.diffLines = wrapDiff(m.diff, m.width)
	m.diffAdds, m.diffDels, m.diffFiles = diffStats(m.diff)
	m.current = viewDiff
	view := ansi.Strip(m.viewDiff())
	for _, want := range []string{"p_1", "builtin", "add endpoint", "+2", "−1", "1 file"} {
		if !strings.Contains(view, want) {
			t.Fatalf("diff header missing %q:\n%s", want, view)
		}
	}
}
