package main

import "testing"

func TestPopFlagSpaceForm(t *testing.T) {
	val, rest := popFlag([]string{"--base", "main", "task", "new"}, "--base", "")
	if val != "main" {
		t.Errorf("val = %q, want %q", val, "main")
	}
	if len(rest) != 2 || rest[0] != "task" || rest[1] != "new" {
		t.Errorf("rest = %v, want [task new]", rest)
	}
}

func TestPopFlagEqualsForm(t *testing.T) {
	val, rest := popFlag([]string{"--base=develop", "task", "new"}, "--base", "")
	if val != "develop" {
		t.Errorf("val = %q, want %q", val, "develop")
	}
	if len(rest) != 2 || rest[0] != "task" || rest[1] != "new" {
		t.Errorf("rest = %v, want [task new]", rest)
	}
}

func TestPopFlagDefault(t *testing.T) {
	val, rest := popFlag([]string{"task", "new"}, "--base", "main")
	if val != "main" {
		t.Errorf("default val = %q, want %q", val, "main")
	}
	if len(rest) != 2 {
		t.Errorf("rest = %v", rest)
	}
}

func TestPopFlagNotConsumedAsPositional(t *testing.T) {
	// --base with no following value (last arg) must NOT consume the next
	// positional; it should fall through to rest with the default.
	val, rest := popFlag([]string{"task", "--base"}, "--base", "main")
	if val != "main" {
		t.Errorf("val = %q, want default %q", val, "main")
	}
	if len(rest) != 2 || rest[1] != "--base" {
		t.Errorf("rest = %v, want [task --base]", rest)
	}
}
