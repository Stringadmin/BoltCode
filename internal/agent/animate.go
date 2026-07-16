package agent

import (
	"fmt"
	"time"
)

// AnimateSessionEnd shows a celebration when the agent finishes.
func AnimateSessionEnd() {
	fmt.Println()
	frames := []string{
		"  ✧",
		"  ✧ ･ﾟ",
		"  ✧ ･ﾟ: *",
		"  ✧ ･ﾟ: *✧",
		"  ✧ ･ﾟ: *✧ ･ﾟ:*",
		"  ✧ ･ﾟ: *✧ ･ﾟ:* done",
		"  ✧ ･ﾟ: *✧ ･ﾟ:* done ٩(◕‿◕｡)۶",
	}
	for _, f := range frames {
		fmt.Print("\r" + ansiYellow + f + ansiReset)
		time.Sleep(80 * time.Millisecond)
	}
	fmt.Println()
	fmt.Println("  " + ansiGreen + "✓ session complete — review your work, then submit" + ansiReset)
	fmt.Println()
}
