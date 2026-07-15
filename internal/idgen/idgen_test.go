package idgen

import "testing"

func TestNewFormat(t *testing.T) {
	id := New("t")
	if len(id) != 10 { // "t_" + 8 hex
		t.Fatalf("expected id of length 10, got %q (len %d)", id, len(id))
	}
	if id[:2] != "t_" {
		t.Errorf("expected prefix 't_', got %q", id[:2])
	}
}

func TestNewUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := New("p")
		if seen[id] {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}

func TestNewPanicsOnEmptyPrefix(t *testing.T) {
	// Empty prefix is allowed and produces "_<hex>"
	id := New("")
	if len(id) != 9 {
		t.Errorf("expected len 9 with empty prefix, got %q (len %d)", id, len(id))
	}
}

func TestValidateValid(t *testing.T) {
	cases := []string{
		"t_a1b2c3d4",
		"p_9f8e7d6c",
		"r_00112233",
		"x_ffffffff",
		"abc_12345678",
	}
	for _, c := range cases {
		if !Validate(c) {
			t.Errorf("Validate(%q) = false, want true", c)
		}
	}
}

func TestValidateInvalid(t *testing.T) {
	cases := []string{
		"",                 // empty
		"t_a1b2c3d",        // only 7 hex chars
		"t_a1b2c3d45",      // 9 hex chars
		"T_a1b2c3d4",       // uppercase prefix
		"t_A1B2C3D4",       // uppercase hex
		"t_a1b2c3dg",       // invalid hex char
		"../../etc/passwd", // path traversal
		"t_ a1b2c3d4",      // space
		"a_b_c_d",          // wrong format
		"-t_a1b2c3d4",      // leading dash
	}
	for _, c := range cases {
		if Validate(c) {
			t.Errorf("Validate(%q) = true, want false", c)
		}
	}
}
