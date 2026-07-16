package gitx

import (
	"fmt"
	"testing"
)

type fakeExitError struct {
	code int
}

func (e fakeExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e fakeExitError) ExitCode() int { return e.code }

func TestIsGitInterrupt(t *testing.T) {
	if !isGitInterrupt(fakeExitError{code: 130}) {
		t.Fatal("exit status 130 should be treated as an interrupted git command")
	}
	if isGitInterrupt(fakeExitError{code: 1}) {
		t.Fatal("ordinary git failure should not be treated as an interruption")
	}
}
