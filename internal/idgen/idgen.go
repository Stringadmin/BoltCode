// Package idgen produces short, stable, collision-resistant ids for tasks and
// proposals, e.g. "t_a1b2c3d4". Short enough to type in the CLI, random enough
// to avoid clashes across parallel agents.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

// idRe matches the canonical idgen format: lowercase prefix, underscore, 8 hex
// chars (e.g. "t_a1b2c3d4", "p_9f8e7d6c", "r_00112233"). Used by Validate to
// reject path-traversal attempts like "../../etc/passwd" before they reach the
// filesystem.
var idRe = regexp.MustCompile(`^[a-z]+_[0-9a-f]{8}$`)

// New returns prefix + "_" + 8 random hex chars, e.g. New("t") -> "t_a1b2c3d4".
func New(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// rand.Read only fails if the OS RNG is unavailable; nothing sane to
		// recover to, so a panic surfaces the broken environment immediately.
		panic("idgen: system RNG unavailable: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b)
}

// Validate reports whether id is a well-formed idgen id (prefix + "_" + 8 hex).
// Callers that build filesystem paths from user-supplied ids should reject
// anything Validate rejects, so a crafted id like "../../etc/passwd" can never
// escape the intended directory.
func Validate(id string) bool {
	return idRe.MatchString(id)
}
