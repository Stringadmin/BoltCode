package banner

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPrintNoCrash(t *testing.T) {
	// Redirect stderr to avoid polluting test output
	tmp := os.Stderr
	os.Stderr = nil
	defer func() {
		os.Stderr = tmp
	}()
	// Should not panic with any input
	Print("")
	Print("0.1.0")
	Print("test-version")
}

func TestVisibleWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"hello", 5},
		{"", 0},
		{"\033[31mhello\033[0m", 5},
		{"\033[1m\033[38;5;220mBOLD\033[0m", 4},
		{"⚡", 2},
	}
	for _, c := range cases {
		got := visibleWidth(c.s)
		if got != c.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestColorBannerOuterBorderStaysInOneColumn(t *testing.T) {
	lines := colorBannerLines("0.1.0")
	wantWidth := w + 2 // left border + inner width + right border
	for index, line := range lines {
		if got := visibleWidth(line); got != wantWidth {
			t.Errorf("banner line %d width = %d, want %d: %q", index, got, wantWidth, ansi.Strip(line))
		}
		plain := ansi.Strip(line)
		if index == 0 {
			if !strings.HasSuffix(plain, "╮") {
				t.Errorf("top border missing right corner: %q", plain)
			}
		} else if index == len(lines)-1 {
			if !strings.HasSuffix(plain, "╯") {
				t.Errorf("bottom border missing right corner: %q", plain)
			}
		} else if !strings.HasSuffix(plain, "│") {
			t.Errorf("banner line %d missing aligned right border: %q", index, plain)
		}
	}
}
