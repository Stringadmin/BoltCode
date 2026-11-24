package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// ToolLesson contains a verified correction with no command, log, or argument values.
type ToolLesson struct {
	Tool  string `json:"tool"`
	Kind  string `json:"kind"`
	Field string `json:"field,omitempty"`
	Type  string `json:"type,omitempty"`
}

const (
	jsonObject    = "json_object"
	argumentType  = "argument_type"
	requiredField = "required_field"
	windowsShell  = "windows_shell"
)

func toolSchema(name string) map[string]any {
	for _, tool := range Tools {
		if tool["name"] == name {
			schema, _ := tool["input_schema"].(map[string]any)
			return schema
		}
	}
	return nil
}

func (lesson ToolLesson) Guidance(mode Mode) string {
	schema := toolSchema(lesson.Tool)
	if schema == nil || !ModeAllowsTool(mode, lesson.Tool) {
		return ""
	}
	switch lesson.Kind {
	case jsonObject:
		return lesson.Tool + ": send arguments as one valid JSON object."
	case argumentType:
		if expected := toolArgumentType(schema, lesson.Field); expected != "" && expected == lesson.Type {
			return fmt.Sprintf("%s: pass %s as JSON type %s.", lesson.Tool, lesson.Field, lesson.Type)
		}
	case requiredField:
		required, _ := schema["required"].([]string)
		if slices.Contains(required, lesson.Field) {
			return fmt.Sprintf("%s: include the required %s argument.", lesson.Tool, lesson.Field)
		}
	case windowsShell:
		if runtime.GOOS == "windows" && lesson.Tool == "run_command" {
			return "run_command: the default shell on Windows is cmd.exe. Use cmd syntax or explicitly invoke PowerShell for PowerShell cmdlets."
		}
	}
	return ""
}

func toolArgumentType(schema map[string]any, field string) string {
	properties, _ := schema["properties"].(map[string]any)
	switch property := properties[field].(type) {
	case map[string]string:
		return property["type"]
	case map[string]any:
		value, _ := property["type"].(string)
		return value
	default:
		return ""
	}
}

func decodeToolUse(id, name, raw string, repair bool) (map[string]any, error) {
	var input any = map[string]any{}
	var err error
	if raw != "" {
		err = json.Unmarshal([]byte(raw), &input)
	}
	_, object := input.(map[string]any)
	malformed := repair && (err != nil || !object)
	if malformed {
		input, err = map[string]any{}, nil
	}
	block := map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
	if malformed {
		block["invalid_input"] = true
	}
	return block, err
}

func validateToolInput(call ToolCall) ToolLesson {
	schema := toolSchema(call.Name)
	if schema == nil {
		return ToolLesson{}
	}
	if call.invalidInput {
		return ToolLesson{Tool: call.Name, Kind: jsonObject}
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(call.Input, &input); err != nil || input == nil {
		return ToolLesson{Tool: call.Name, Kind: jsonObject}
	}
	required, _ := schema["required"].([]string)
	for _, field := range required {
		if _, exists := input[field]; !exists {
			return ToolLesson{Tool: call.Name, Kind: requiredField, Field: field}
		}
	}
	for _, field := range slices.Sorted(maps.Keys(input)) {
		expected := toolArgumentType(schema, field)
		if expected != "" && jsonArgumentType(input[field]) != expected {
			return ToolLesson{Tool: call.Name, Kind: argumentType, Field: field, Type: expected}
		}
	}
	if call.Name == "run_command" && strings.TrimSpace(toolInputValue(call, "command")) == "" {
		return ToolLesson{Tool: call.Name, Kind: requiredField, Field: "command"}
	}
	return ToolLesson{}
}

func jsonArgumentType(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		return "string"
	case '[':
		return "array"
	case '{':
		return "object"
	case 't', 'f':
		return "boolean"
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-':
		return "number"
	}
	return ""
}

var readPowerShellCmdlet = regexp.MustCompile(`(?i)^(Get-ChildItem|Get-Content|Select-String|Test-Path)([ \t]|$)`)
var explicitPowerShell = regexp.MustCompile(`(?i)^(?:pwsh|powershell)(?:\.exe)?(?:[ \t]+-(?:NoProfile|NonInteractive|NoLogo))*[ \t]+-(?:Command|c)[ \t]+(.+)$`)

func commandFailureLesson(command, output string) ToolLesson {
	if runtime.GOOS == "windows" && strings.Contains(output, "\nexit: exit status ") &&
		readPowerShellCmdlet.MatchString(strings.TrimSpace(command)) {
		return ToolLesson{Tool: "run_command", Kind: windowsShell}
	}
	return ToolLesson{}
}

type toolRepairAttempt struct {
	lesson  ToolLesson
	command string
	round   int
}

func (a *Agent) observeToolRepair(call ToolCall, result ToolResult) {
	if a.toolRepairAttempts == nil {
		return
	}
	if result.IsError {
		if result.lesson.Kind != "" {
			a.toolRepairAttempts[call.Name] = toolRepairAttempt{
				lesson: result.lesson, command: windowsFailedCommand(call, result.lesson), round: a.Stats.Rounds,
			}
		}
		return
	}
	attempt, pending := a.toolRepairAttempts[call.Name]
	if !pending || attempt.round == a.Stats.Rounds || !attempt.correctedBy(call) {
		return
	}
	delete(a.toolRepairAttempts, call.Name)
	if !slices.Contains(a.LearnedToolLessons, attempt.lesson) {
		a.LearnedToolLessons = append(a.LearnedToolLessons, attempt.lesson)
	}
}

func windowsFailedCommand(call ToolCall, lesson ToolLesson) string {
	if lesson.Kind == windowsShell {
		return strings.TrimSpace(toolInputValue(call, "command"))
	}
	return ""
}

func (attempt toolRepairAttempt) correctedBy(call ToolCall) bool {
	if attempt.lesson.Kind == windowsShell {
		match := explicitPowerShell.FindStringSubmatch(strings.TrimSpace(toolInputValue(call, "command")))
		return match != nil && strings.Trim(strings.TrimSpace(match[1]), "\"") == attempt.command
	}
	if attempt.lesson.Field == "" {
		return true
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(call.Input, &input) != nil {
		return false
	}
	_, present := input[attempt.lesson.Field]
	return present
}
