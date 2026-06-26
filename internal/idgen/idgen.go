// Package idgen produces short, stable, collision-resistant ids for tasks and
// proposals, e.g. "t_a1b2c3d4". Short enough to type in the CLI, random enough
// to avoid clashes across parallel agents.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

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
