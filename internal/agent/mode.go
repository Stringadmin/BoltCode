package agent

import (
	"fmt"
	"strings"
)

// Mode defines the agent's operating boundary for a session. Build is the
// default and permits every tool; Plan and Ask expose progressively smaller,
// read-only tool sets. ExecTool enforces the same boundary at runtime so a
// provider cannot bypass it by returning a tool that was not advertised.
type Mode string

const (
	ModeBuild Mode = "build"
	ModePlan  Mode = "plan"
	ModeAsk   Mode = "ask"
)

func ParseMode(value string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case ModeBuild:
		return ModeBuild, nil
	case ModePlan:
		return ModePlan, nil
	case ModeAsk:
		return ModeAsk, nil
	default:
		return "", fmt.Errorf("unknown mode %q (use build, plan, or ask)", value)
	}
}

func (a *Agent) EffectiveMode() Mode {
	switch a.Mode {
	case ModePlan, ModeAsk:
		return a.Mode
	default:
		return ModeBuild
	}
}

func (a *Agent) SetMode(mode Mode) {
	switch mode {
	case ModePlan, ModeAsk:
		a.Mode = mode
	default:
		a.Mode = ModeBuild
	}
}

func NextMode(mode Mode) Mode {
	switch mode {
	case ModeBuild:
		return ModePlan
	case ModePlan:
		return ModeAsk
	default:
		return ModeBuild
	}
}

func ModeDescription(mode Mode) string {
	switch mode {
	case ModePlan:
		return "inspect and design only; no project changes"
	case ModeAsk:
		return "answer and inspect only; no plan or project changes"
	default:
		return "edit files, run commands, and implement changes"
	}
}

func modeSystemInstructions(mode Mode) string {
	switch mode {
	case ModePlan:
		return `## Active Mode: PLAN (overrides conflicting workflow instructions)
You may inspect the project and maintain a todo plan, but you MUST NOT modify project files, memory, git history, or run shell/diagnostic commands. Produce a concrete implementation plan and wait for the user to switch to Build mode before implementing.`
	case ModeAsk:
		return `## Active Mode: ASK (overrides conflicting workflow instructions)
Answer the user's question. You may inspect existing project information when useful, but you MUST NOT create a task plan, modify files or memory, run commands, or change git state. Do not begin implementation.`
	default:
		return `## Active Mode: BUILD
You may inspect, edit, run commands, and implement the requested work. Follow the normal planning, verification, and safety rules above.`
	}
}

var inspectToolNames = map[string]bool{
	"read_file":   true,
	"search_code": true,
	"list_files":  true,
	"web_fetch":   true,
	"git_status":  true,
	"git_diff":    true,
	"glob":        true,
}

func ModeAllowsTool(mode Mode, name string) bool {
	switch mode {
	case ModePlan:
		return inspectToolNames[name] || name == "todo_write"
	case ModeAsk:
		return inspectToolNames[name]
	default:
		return true
	}
}

func ToolsForMode(mode Mode) []map[string]interface{} {
	mode = normalizeMode(mode)
	out := make([]map[string]interface{}, 0, len(Tools))
	for _, tool := range Tools {
		name, _ := tool["name"].(string)
		if ModeAllowsTool(mode, name) {
			out = append(out, tool)
		}
	}
	return out
}

func normalizeMode(mode Mode) Mode {
	if mode == ModePlan || mode == ModeAsk {
		return mode
	}
	return ModeBuild
}
