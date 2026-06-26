package agent

import (
	"fmt"
)

// runTriage scans the codebase (worktree or main repo) and asks the AI to
// review it and return prioritized action items. It appends a triage prompt to
// the conversation and returns true so the agent loop calls the API next — the
// AI's triage report then streams back through the normal rendering path.
//
// Scope:
//   - "worktree": the agent's working directory (default). The AI has full tool
//     access here and can dig deeper interactively.
//   - "repo": the main repository root (a.RepoRoot). This is a one-shot analysis
//     based on the scan only — the AI's file tools are confined to the worktree
//     by safeJoin, so it cannot read arbitrary files under the repo root.
func (a *Agent) runTriage(scope string) bool {
	target := a.Workdir
	if scope == "repo" {
		if a.RepoRoot == "" {
			fmt.Println("\n  " + ansiGray + "repo root unknown — using worktree scope" + ansiReset)
			scope = "worktree"
		} else {
			target = a.RepoRoot
		}
	}

	fmt.Printf("\n  "+ansiCyan+"🔍 triage"+ansiReset+" "+ansiDarkGray+"(scope: %s)"+ansiReset+" — scanning codebase...\n", scope)

	ctx := a.scanProjectAt(target)

	// Warn about the repo-scope limitation up front so expectations are set.
	limitNote := ""
	if scope == "repo" {
		limitNote = "\n\nNote: this is a repo-scope review based on the scan above. Your file tools are " +
			"confined to the current worktree, so for deeper interactive investigation recommend " +
			"specific files and let the user run /triage worktree or point you at them."
	}

	prompt := fmt.Sprintf(`You are now in triage mode. Review the codebase below and produce a prioritized
list of action items.

For each item give:
1. A one-line title (category prefix: [bug] / [security] / [architecture] / [tests] / [tech-debt] / [perf])
2. What the problem is (be specific — name files/symbols)
3. Why it matters (impact / risk)
4. A concrete fix suggestion

Order by impact and urgency (most important first). Use a numbered list.
Be honest about uncertainty — mark items you couldn't fully verify from the scan.
Keep it actionable; this will guide the next work session.

Triage scope: %s%s

--- CODEBASE ---
%s
--- END CODEBASE ---`, scope, limitNote, ctx)

	a.Messages = append(a.Messages, Message{Role: "user", Content: prompt})
	return true
}
