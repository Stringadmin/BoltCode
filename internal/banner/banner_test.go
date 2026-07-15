package banner

import (
	"os"
	"testing"
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
	}
	for _, c := range cases {
		got := visibleWidth(c.s)
		if got != c.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}
