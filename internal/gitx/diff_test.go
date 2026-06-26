package gitx

import "testing"

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"single line", "hello", 1},
		{"two lines", "a\nb", 2},
		{"trailing newline", "a\nb\n", 2},
		{"empty string", "", 0},
		{"only newlines", "\n\n", 0},
		{"mixed", "src/main.go\nsrc/util.go\nREADME.md", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != tt.want {
				t.Errorf("splitLines(%q) = %v (len %d), want len %d",
					tt.input, got, len(got), tt.want)
			}
		})
	}
}

func TestSplitLinesStripsCRLF(t *testing.T) {
	// Windows git may emit CRLF; the trailing \r must not survive into paths.
	got := splitLines("src/a.go\r\nsrc/b.go\r\n")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	for _, line := range got {
		for _, ch := range line {
			if ch == '\r' {
				t.Errorf("line %q contains stray CR", line)
			}
		}
	}
	if got[0] != "src/a.go" || got[1] != "src/b.go" {
		t.Errorf("got %v, want [src/a.go src/b.go]", got)
	}
}
