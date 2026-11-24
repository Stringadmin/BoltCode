package dataset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// SchemaVersion identifies the normalized trace contract emitted by BoltCode.
const SchemaVersion = "boltcode.coding_trace.v1"

// OpenCodeOptions controls an OpenCode session import.
type OpenCodeOptions struct {
	Executable string
	ProjectDir string
	SessionIDs []string
	MaxCount   int
	// AllowRawSource requests the unsanitized OpenCode export. This is intended
	// only for local, authorized projects; BoltCode still drops private
	// reasoning and runs its own recursive redactor before writing JSONL.
	AllowRawSource bool
}

// CommandRunner executes one OpenCode CLI command and returns its stdout.
// It is injectable so the importer can be tested without a local OpenCode
// installation or access to a real transcript database.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// Trace is one normalized, sanitized coding-agent session. It is deliberately
// provider-neutral so later exporters can target SFT, preference, or eval
// formats without binding the dataset to OpenCode's internal database schema.
type Trace struct {
	SchemaVersion string         `json:"schema_version"`
	Source        TraceSource    `json:"source"`
	Messages      []TraceMessage `json:"messages"`
	Stats         TraceStats     `json:"stats"`
}

// TraceSource describes where a normalized trace came from.
type TraceSource struct {
	Kind            string `json:"kind"`
	SessionID       string `json:"session_id"`
	ProjectID       string `json:"project_id,omitempty"`
	Directory       string `json:"directory"`
	Title           string `json:"title,omitempty"`
	OpenCodeVersion string `json:"opencode_version,omitempty"`
	Agent           string `json:"agent,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	Variant         string `json:"variant,omitempty"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
	Sanitized       bool   `json:"sanitized"`
}

// TraceMessage is one visible user or assistant message after private
// reasoning and OpenCode lifecycle-control parts have been removed.
type TraceMessage struct {
	ID       string      `json:"id,omitempty"`
	Role     string      `json:"role"`
	Agent    string      `json:"agent,omitempty"`
	Provider string      `json:"provider,omitempty"`
	Model    string      `json:"model,omitempty"`
	Parts    []TracePart `json:"parts"`
}

// TracePart is either visible text or one complete tool execution.
type TracePart struct {
	Type string         `json:"type"`
	Text string         `json:"text,omitempty"`
	Tool *ToolExecution `json:"tool,omitempty"`
}

// ToolExecution combines an observable tool call with its result. Later model
// exporters may split it into assistant tool-call and tool-result messages.
type ToolExecution struct {
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status,omitempty"`
	Input     any    `json:"input,omitempty"`
	Output    any    `json:"output,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	StartedAt int64  `json:"started_at,omitempty"`
	EndedAt   int64  `json:"ended_at,omitempty"`
}

// TraceStats reports filtering and redaction decisions for auditability.
type TraceStats struct {
	SourceMessages        int `json:"source_messages"`
	KeptMessages          int `json:"kept_messages"`
	TextParts             int `json:"text_parts"`
	ToolExecutions        int `json:"tool_executions"`
	ToolErrors            int `json:"tool_errors"`
	DroppedReasoningParts int `json:"dropped_reasoning_parts"`
	DroppedControlParts   int `json:"dropped_control_parts"`
	DroppedUnknownParts   int `json:"dropped_unknown_parts"`
	Redactions            int `json:"redactions"`
}

// ImportSummary is the aggregate result printed after an import.
type ImportSummary struct {
	Sessions         int
	Messages         int
	TextParts        int
	ToolExecutions   int
	ToolErrors       int
	DroppedReasoning int
	Redactions       int
}

type openCodeSession struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated"`
}

type openCodeModel struct {
	ID         string `json:"id"`
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}

type openCodeExport struct {
	Info struct {
		ID        string        `json:"id"`
		ProjectID string        `json:"projectID"`
		Directory string        `json:"directory"`
		Title     string        `json:"title"`
		Version   string        `json:"version"`
		Agent     string        `json:"agent"`
		Model     openCodeModel `json:"model"`
	} `json:"info"`
	Messages []struct {
		Info struct {
			ID    string        `json:"id"`
			Role  string        `json:"role"`
			Agent string        `json:"agent"`
			Model openCodeModel `json:"model"`
		} `json:"info"`
		Parts []json.RawMessage `json:"parts"`
	} `json:"messages"`
}

// ImportOpenCode lists and exports matching sessions through OpenCode's public
// CLI. Export always uses --sanitize and --pure: callers cannot accidentally
// disable transcript redaction or load third-party plugins during collection.
func ImportOpenCode(ctx context.Context, opts OpenCodeOptions) ([]Trace, ImportSummary, error) {
	return ImportOpenCodeWithRunner(ctx, opts, runCommand)
}

// ImportOpenCodeWithRunner is ImportOpenCode with an injectable command
// runner. Production callers normally use ImportOpenCode.
func ImportOpenCodeWithRunner(ctx context.Context, opts OpenCodeOptions, runner CommandRunner) ([]Trace, ImportSummary, error) {
	if runner == nil {
		return nil, ImportSummary{}, fmt.Errorf("OpenCode command runner is required")
	}
	projectDir := strings.TrimSpace(opts.ProjectDir)
	if projectDir == "" {
		return nil, ImportSummary{}, fmt.Errorf("OpenCode project directory is required")
	}
	executable := strings.TrimSpace(opts.Executable)
	if executable == "" {
		var err error
		executable, err = exec.LookPath("opencode")
		if err != nil {
			return nil, ImportSummary{}, fmt.Errorf("finding opencode executable: %w", err)
		}
	}
	wanted, err := requestedSessions(opts.SessionIDs)
	if err != nil {
		return nil, ImportSummary{}, err
	}

	listArgs := []string{"session", "list", "--format", "json", "--pure"}
	if opts.MaxCount > 0 {
		listArgs = append(listArgs, "--max-count", strconv.Itoa(opts.MaxCount))
	}
	listData, err := runner(ctx, projectDir, executable, listArgs...)
	if err != nil {
		return nil, ImportSummary{}, fmt.Errorf("listing OpenCode sessions: %w", err)
	}
	var sessions []openCodeSession
	if err := json.Unmarshal(listData, &sessions); err != nil {
		return nil, ImportSummary{}, fmt.Errorf("decoding OpenCode session list: %w", err)
	}

	var selected []openCodeSession
	found := make(map[string]bool)
	for _, session := range sessions {
		if !sameDirectory(session.Directory, projectDir) {
			continue
		}
		if len(wanted) > 0 && !wanted[session.ID] {
			continue
		}
		selected = append(selected, session)
		found[session.ID] = true
	}
	// OpenCode's public session list omits child/subagent sessions. Explicitly
	// requested ids are still exportable, so add any missing ids and verify the
	// exported directory before accepting their data.
	for id := range wanted {
		if !found[id] {
			selected = append(selected, openCodeSession{ID: id, Directory: projectDir})
		}
	}
	if len(selected) == 0 {
		return nil, ImportSummary{}, fmt.Errorf("no OpenCode sessions found for %s", projectDir)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Created == selected[j].Created {
			return selected[i].ID < selected[j].ID
		}
		return selected[i].Created < selected[j].Created
	})

	traces := make([]Trace, 0, len(selected))
	var summary ImportSummary
	for _, session := range selected {
		exportArgs := []string{"export", session.ID}
		if !opts.AllowRawSource {
			exportArgs = append(exportArgs, "--sanitize")
		}
		exportArgs = append(exportArgs, "--pure")
		data, runErr := runner(ctx, projectDir, executable, exportArgs...)
		if runErr != nil {
			return nil, ImportSummary{}, fmt.Errorf("exporting OpenCode session %s: %w", session.ID, runErr)
		}
		trace, normalizeErr := NormalizeOpenCodeExport(data, session)
		if normalizeErr != nil {
			return nil, ImportSummary{}, fmt.Errorf("normalizing OpenCode session %s: %w", session.ID, normalizeErr)
		}
		if !sameDirectory(trace.Source.Directory, projectDir) {
			return nil, ImportSummary{}, fmt.Errorf("OpenCode session %s belongs to %s, not %s", session.ID, trace.Source.Directory, projectDir)
		}
		traces = append(traces, trace)
		summary.Sessions++
		summary.Messages += trace.Stats.KeptMessages
		summary.TextParts += trace.Stats.TextParts
		summary.ToolExecutions += trace.Stats.ToolExecutions
		summary.ToolErrors += trace.Stats.ToolErrors
		summary.DroppedReasoning += trace.Stats.DroppedReasoningParts
		summary.Redactions += trace.Stats.Redactions
	}
	return traces, summary, nil
}

// NormalizeOpenCodeExport converts one sanitized OpenCode export into the
// provider-neutral trace contract. Reasoning and lifecycle-control parts are
// intentionally excluded from the result.
func NormalizeOpenCodeExport(data []byte, session openCodeSession) (Trace, error) {
	var exported openCodeExport
	if err := json.Unmarshal(data, &exported); err != nil {
		return Trace{}, err
	}
	if exported.Info.ID == "" {
		return Trace{}, fmt.Errorf("export is missing session id")
	}
	if session.ID != "" && exported.Info.ID != session.ID {
		return Trace{}, fmt.Errorf("exported session id %q does not match requested id %q", exported.Info.ID, session.ID)
	}

	redactor := newRedactor()
	provider := exported.Info.Model.ProviderID
	model := exported.Info.Model.ID
	trace := Trace{
		SchemaVersion: SchemaVersion,
		Source: TraceSource{
			Kind:            "opencode",
			SessionID:       exported.Info.ID,
			ProjectID:       firstNonEmpty(exported.Info.ProjectID, session.ProjectID),
			Directory:       firstNonEmpty(exported.Info.Directory, session.Directory),
			Title:           redactor.text(firstNonEmpty(exported.Info.Title, session.Title)),
			OpenCodeVersion: exported.Info.Version,
			Agent:           redactor.text(exported.Info.Agent),
			Provider:        provider,
			Model:           model,
			Variant:         exported.Info.Model.Variant,
			CreatedAt:       session.Created,
			UpdatedAt:       session.Updated,
			Sanitized:       true,
		},
	}
	trace.Stats.SourceMessages = len(exported.Messages)
	for _, sourceMessage := range exported.Messages {
		role := strings.ToLower(strings.TrimSpace(sourceMessage.Info.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		message := TraceMessage{
			ID:       sourceMessage.Info.ID,
			Role:     role,
			Agent:    redactor.text(sourceMessage.Info.Agent),
			Provider: sourceMessage.Info.Model.ProviderID,
			Model:    sourceMessage.Info.Model.ModelID,
		}
		for _, rawPart := range sourceMessage.Parts {
			var header struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(rawPart, &header); err != nil {
				trace.Stats.DroppedUnknownParts++
				continue
			}
			switch strings.ToLower(header.Type) {
			case "text":
				var textPart struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(rawPart, &textPart); err != nil {
					trace.Stats.DroppedUnknownParts++
					continue
				}
				text := redactor.text(textPart.Text)
				if strings.TrimSpace(text) == "" {
					continue
				}
				message.Parts = append(message.Parts, TracePart{Type: "text", Text: text})
				trace.Stats.TextParts++
			case "tool":
				var toolPart struct {
					CallID string `json:"callID"`
					Tool   string `json:"tool"`
					State  struct {
						Input  any    `json:"input"`
						Output any    `json:"output"`
						Status string `json:"status"`
						Title  string `json:"title"`
						Time   struct {
							Start int64 `json:"start"`
							End   int64 `json:"end"`
						} `json:"time"`
					} `json:"state"`
				}
				if err := json.Unmarshal(rawPart, &toolPart); err != nil || strings.TrimSpace(toolPart.Tool) == "" {
					trace.Stats.DroppedUnknownParts++
					continue
				}
				status := strings.ToLower(toolPart.State.Status)
				tool := &ToolExecution{
					CallID:    toolPart.CallID,
					Name:      toolPart.Tool,
					Title:     redactor.text(toolPart.State.Title),
					Status:    status,
					Input:     redactor.value(toolPart.State.Input),
					Output:    redactor.value(toolPart.State.Output),
					IsError:   status == "error" || status == "failed",
					StartedAt: toolPart.State.Time.Start,
					EndedAt:   toolPart.State.Time.End,
				}
				message.Parts = append(message.Parts, TracePart{Type: "tool_execution", Tool: tool})
				trace.Stats.ToolExecutions++
				if tool.IsError {
					trace.Stats.ToolErrors++
				}
			case "reasoning", "thinking", "redacted_thinking":
				trace.Stats.DroppedReasoningParts++
			case "step-start", "step-finish":
				trace.Stats.DroppedControlParts++
			default:
				trace.Stats.DroppedUnknownParts++
			}
		}
		if len(message.Parts) > 0 {
			trace.Messages = append(trace.Messages, message)
		}
	}
	trace.Stats.KeptMessages = len(trace.Messages)
	trace.Stats.Redactions = redactor.count
	return trace, nil
}

// MarshalJSONL serializes one trace per line for streaming dataset tools.
func MarshalJSONL(traces []Trace) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, trace := range traces {
		if err := encoder.Encode(trace); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	commandName := name
	commandArgs := args
	// npm installs command shims as .cmd files on Windows. CreateProcess cannot
	// launch those scripts directly, so preserve the shim's environment setup by
	// running it through COMSPEC instead of bypassing it for the underlying exe.
	ext := strings.ToLower(filepath.Ext(name))
	if runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat") {
		commandName = os.Getenv("COMSPEC")
		if commandName == "" {
			commandName = "cmd.exe"
		}
		commandArgs = append([]string{"/d", "/s", "/c", name}, args...)
	}
	cmd := exec.CommandContext(ctx, commandName, commandArgs...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[:500] + "..."
		}
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func requestedSessions(ids []string) (map[string]bool, error) {
	wanted := make(map[string]bool)
	for _, raw := range ids {
		for _, id := range strings.Split(raw, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !sessionIDPattern.MatchString(id) {
				return nil, fmt.Errorf("invalid OpenCode session id %q", id)
			}
			wanted[id] = true
		}
	}
	return wanted, nil
}

func sameDirectory(left, right string) bool {
	return canonicalDirectory(left) == canonicalDirectory(right)
}

func canonicalDirectory(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	value = path.Clean(value)
	if len(value) >= 3 && value[1] == ':' && value[2] == '/' {
		value = strings.ToLower(value)
	}
	return strings.TrimSuffix(value, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type redactor struct {
	count int
}

var (
	secretKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|passwd|secret|private[_-]?key|cookie|credential)`)
	bearerPattern    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}`)
	apiTokenPattern  = regexp.MustCompile(`\b(?:sk|rk|pk)-[A-Za-z0-9_-]{12,}\b`)
	jwtPattern       = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	privateKeyBlock  = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	secretLine       = regexp.MustCompile(`(?im)(^|\n)([ \t]*(?:[A-Z][A-Z0-9_]*(?:API_KEY|TOKEN|PASSWORD|SECRET|PRIVATE_KEY)[A-Z0-9_]*|AUTHORIZATION)[ \t]*[:=][ \t]*)([^\r\n]+)`)
	secretAssignment = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|passwd|secret|private[_-]?key|cookie|credential)["']?[ \t]*[:=][ \t]*["']?)([^"'\\s,}]+)`)
)

func newRedactor() *redactor {
	return &redactor{}
}

func (r *redactor) value(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			if secretKeyPattern.MatchString(key) {
				clean[key] = "[REDACTED]"
				r.count++
				continue
			}
			clean[key] = r.value(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = r.value(item)
		}
		return clean
	case string:
		return r.text(typed)
	default:
		return value
	}
}

func (r *redactor) text(value string) string {
	patterns := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{privateKeyBlock, "[REDACTED PRIVATE KEY]"},
		{bearerPattern, "Bearer [REDACTED]"},
		{apiTokenPattern, "[REDACTED TOKEN]"},
		{jwtPattern, "[REDACTED JWT]"},
		{secretLine, "${1}${2}[REDACTED]"},
		{secretAssignment, "${1}[REDACTED]"},
	}
	for _, item := range patterns {
		matches := item.pattern.FindAllStringIndex(value, -1)
		if len(matches) == 0 {
			continue
		}
		r.count += len(matches)
		value = item.pattern.ReplaceAllString(value, item.replacement)
	}
	return value
}
