package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// sleepFn pauses for backoff between retries. It's a package variable so tests
// can swap in a no-op to avoid real waits; production code uses time.Sleep.
var sleepFn = time.Sleep

// callAPI sends the current conversation to the API and streams the response.
// Text content is printed to stdout in real-time as it arrives; tool_use blocks
// are accumulated and returned as JSON so the agent loop can execute them.
//
// Transient failures (network errors, 429 rate limits, 5xx server errors) are
// retried with exponential backoff so a flaky connection or a momentary rate
// limit doesn't kill the whole session. Non-retryable failures (4xx auth /
// request errors) surface immediately.
func (a *Agent) callAPI(system string) ([]byte, Usage, error) {
	if err := validateToolMessageSequence(a.Messages, isAnthropicProvider(a.Config)); err != nil {
		return nil, Usage{}, fmt.Errorf("invalid tool message sequence: %w", err)
	}
	url := providerURL(a.Config)
	mode := a.EffectiveMode()
	system = system + "\n\n" + modeSystemInstructions(mode)
	payload := buildPayloadForMode(a.Config, system, a.Messages, mode)

	// Cancelable context so Ctrl+C can interrupt a long API call without
	// killing the process. The cancel func is stored on the Agent so the
	// signal handler in launchAgent can call it.
	ctx, cancel := context.WithCancel(a.context())
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()

	const maxAttempts = 4
	var backoff time.Duration = 1 * time.Second
	client := &http.Client{Timeout: 5 * time.Minute}

	var lastErr error
	repairedMissingToolOutput := false
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Rebuild the request each attempt: the request body reader is
		// consumed by client.Do, so it can't be reused across calls.
		content, usage, err, retryable, wait := a.doOnce(ctx, client, url, payload)

		if err == nil {
			return content, usage, nil
		}
		// User interrupt — don't retry, surface immediately.
		if ctx.Err() != nil {
			return nil, Usage{}, ctx.Err()
		}
		if !retryable {
			if !repairedMissingToolOutput && !isAnthropicProvider(a.Config) {
				callID := missingToolOutputCallID(err)
				if rebindLatestSingleToolCall(a.Messages, callID) {
					repairedMissingToolOutput = true
					payload = buildPayloadForMode(a.Config, system, a.Messages, mode)
					a.reportProtocolRepair(callID)
					continue
				}
			}
			// Non-retryable failure (4xx auth/bad request) — surface now.
			return nil, Usage{}, err
		}

		lastErr = err
		if attempt == maxAttempts-1 {
			break
		}
		if wait <= 0 {
			wait = backoff
			backoff *= 2
			if backoff > 8*time.Second {
				backoff = 8 * time.Second
			}
		}
		a.reportRetry(lastErr, wait)
		sleepFn(wait)
	}

	return nil, Usage{}, fmt.Errorf("API error after %d attempts: %w", maxAttempts, lastErr)
}

// doOnce performs a single API request. It returns (content, usage, err,
// retryable, wait):
//   - on success: (content, usage, nil, false, 0)
//   - on a transient failure: (nil, usage, err, true, wait) where wait is the
//     server-suggested delay (e.g. from Retry-After) or 0 to let the caller
//     apply its own backoff
//   - on a non-retryable failure: (nil, usage, err, false, 0)
func (a *Agent) doOnce(ctx context.Context, client *http.Client, url string, payload []byte) ([]byte, Usage, error, bool, time.Duration) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		// Malformed request is deterministic — no point retrying.
		return nil, Usage{}, err, false, 0
	}
	req.Header.Set("Content-Type", "application/json")
	if isAnthropicProvider(a.Config) {
		req.Header.Set("x-api-key", a.Config.APIKey)
		req.Header.Set("anthropic-version", "2025-01-25")
	} else {
		req.Header.Set("Authorization", "Bearer "+a.Config.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Connection failure / timeout — transient.
		return nil, Usage{}, fmt.Errorf("connection failed: %w", err), true, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var content []byte
		var usage Usage
		var perr error
		if isAnthropicProvider(a.Config) {
			content, usage, perr = a.parseStream(resp.Body)
		} else if isResponsesProvider(a.Config) {
			content, usage, perr = a.parseResponsesStream(resp.Body)
		} else {
			content, usage, perr = a.parseOpenAIStream(resp.Body)
		}
		if perr != nil {
			// A mid-stream failure (dropped connection, oversized line) is
			// worth retrying — the next attempt may complete cleanly.
			return nil, usage, perr, true, 0
		}
		return content, usage, nil, false, 0
	}

	// Non-200: read the body for a diagnostic message.
	var errBody bytes.Buffer
	errBody.ReadFrom(resp.Body)
	msg := fmt.Sprintf("API %d: %s", resp.StatusCode, truncateStr(string(errBody.Bytes()), 200, "..."))

	switch {
	case resp.StatusCode == 429:
		// Rate limited — honor the server's Retry-After if present.
		return nil, Usage{}, fmt.Errorf("%s (rate limited)", msg), true, parseRetryAfter(resp.Header.Get("Retry-After"))
	case resp.StatusCode >= 500:
		return nil, Usage{}, fmt.Errorf("%s (server error)", msg), true, 0
	default:
		// 4xx (auth, bad request, etc.) is deterministic — don't retry.
		return nil, Usage{}, fmt.Errorf("%s", msg), false, 0
	}
}

// parseRetryAfter interprets an HTTP Retry-After header as a delay. Accepts
// either an integer number of seconds or an HTTP-date; returns 0 if it can't
// be parsed (letting the caller fall back to its own backoff).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	// Integer seconds.
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form.
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// stderrWriter is the sink for retry-progress notices. It's a package variable
// so tests can redirect it to io.Discard; production code writes to os.Stderr.
var stderrWriter io.Writer = os.Stderr

// stderr returns the current progress sink.
func stderr() io.Writer {
	if stderrWriter == nil {
		return io.Discard
	}
	return stderrWriter
}

// reportRetry keeps full-screen frontends synchronized with the runtime. A
// direct stderr write moves the terminal cursor behind Bubble Tea's back and
// leaves duplicated status/input rows on the next repaint. Structured events
// let the TUI render the notice inside its own frame; the classic shell keeps
// the existing stderr progress line.
func (a *Agent) reportRetry(err error, wait time.Duration) {
	if a.UIMode && a.EventFn != nil {
		detail := fmt.Sprintf("in %s · %s", wait, truncate(err.Error(), 96))
		a.emit(Event{Kind: EventPhase, Activity: "Retrying API request", Detail: detail})
		return
	}
	fmt.Fprintf(stderr(), "  api: %v — retrying in %s\n", err, wait)
}

func (a *Agent) reportProtocolRepair(callID string) {
	if a.UIMode && a.EventFn != nil {
		a.emit(Event{Kind: EventPhase, Activity: "Repairing tool call sequence", Detail: callID})
		return
	}
	fmt.Fprintf(stderr(), "  api: repairing tool output id %s and retrying\n", callID)
}

func missingToolOutputCallID(err error) string {
	if err == nil {
		return ""
	}
	const marker = "No tool output found for function call"
	message := err.Error()
	start := strings.Index(message, marker)
	if start < 0 {
		return ""
	}
	rest := strings.TrimLeft(message[start+len(marker):], " :\t\r\n\"'")
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			end++
			continue
		}
		break
	}
	return rest[:end]
}

// streamBlock tracks a content block being assembled from SSE deltas.
type streamBlock struct {
	Type  string // "text", "tool_use", "thinking", or "redacted_thinking"
	ID    string // tool_use id
	Name  string // tool_use name
	Text  string // accumulated text
	Input string // accumulated JSON input for tool_use
	// Signature is required when replaying Anthropic extended-thinking blocks
	// after a tool call. Data carries encrypted redacted_thinking content.
	Signature string
	Data      string
}

// parseStream reads SSE events from the Anthropic streaming API, prints text
// in real-time, and returns assembled content blocks + usage when done.
func (a *Agent) parseStream(body interface{ Read([]byte) (int, error) }) ([]byte, Usage, error) {
	scanner := bufio.NewScanner(body)
	// A single SSE data line can carry a large text delta or a big tool input
	// JSON blob. The default 64KB token limit would silently truncate such
	// lines (and bufio.Scanner drops the rest without erroring here), corrupting
	// the response. Raise it to 4MB.
	scanner.Buffer(make([]byte, 0, 1<<20), 4<<20)
	var blocks []streamBlock
	var usage Usage

	for scanner.Scan() {
		line := scanner.Text()

		data, ok := parseSSEDataLine(line)
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Thinking  string `json:"thinking"`
				Signature string `json:"signature"`
				JSON      string `json:"partial_json"`
			} `json:"delta"`
			ContentBlock struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Thinking  string `json:"thinking"`
				Signature string `json:"signature"`
				Data      string `json:"data"`
			} `json:"content_block"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, usage, fmt.Errorf("invalid Anthropic stream event: %w", err)
		}

		switch event.Type {
		case "content_block_start":
			if event.Index < 0 {
				return nil, usage, fmt.Errorf("Anthropic stream used negative content block index %d", event.Index)
			}
			block := streamBlock{
				Type:      event.ContentBlock.Type,
				Text:      event.ContentBlock.Thinking,
				Signature: event.ContentBlock.Signature,
				Data:      event.ContentBlock.Data,
			}
			if event.ContentBlock.Type == "tool_use" {
				block.ID = event.ContentBlock.ID
				block.Name = event.ContentBlock.Name
			}
			for len(blocks) <= event.Index {
				blocks = append(blocks, streamBlock{})
			}
			if blocks[event.Index].Type != "" {
				return nil, usage, fmt.Errorf("Anthropic stream repeated content block index %d", event.Index)
			}
			blocks[event.Index] = block

		case "content_block_delta":
			if event.Index < 0 || event.Index >= len(blocks) || blocks[event.Index].Type == "" {
				return nil, usage, fmt.Errorf("Anthropic stream delta referenced unknown content block %d", event.Index)
			}
			switch event.Delta.Type {
			case "text_delta":
				blocks[event.Index].Text += event.Delta.Text
				// Stream text immediately for real-time feedback (typewriter effect).
				fmt.Print(event.Delta.Text)
			case "thinking_delta":
				blocks[event.Index].Text += event.Delta.Thinking
			case "signature_delta":
				blocks[event.Index].Signature += event.Delta.Signature
			case "input_json_delta":
				blocks[event.Index].Input += event.Delta.JSON
			}

		case "content_block_stop":
			if event.Index < len(blocks) && blocks[event.Index].Type == "text" && strings.TrimSpace(blocks[event.Index].Text) != "" {
				// Text was already streamed token-by-token above; just end the paragraph.
				fmt.Println()
			}

		case "message_delta":
			if event.Usage != nil {
				if event.Usage.InputTokens > 0 {
					usage.InputTokens = event.Usage.InputTokens
				}
				if event.Usage.OutputTokens > 0 {
					usage.OutputTokens = event.Usage.OutputTokens
				}
			}
			// Also check for usage in delta
			var deltaMsg struct {
				Usage Usage `json:"usage"`
			}
			json.Unmarshal([]byte(data), &deltaMsg)
			if deltaMsg.Usage.OutputTokens > 0 {
				usage.OutputTokens = deltaMsg.Usage.OutputTokens
			}

		case "message_start":
			var startMsg struct {
				Message struct {
					Usage Usage `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &startMsg); err == nil {
				usage.InputTokens = startMsg.Message.Usage.InputTokens
			}

		case "error":
			var errEvt struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &errEvt) == nil {
				return nil, Usage{}, fmt.Errorf("stream error: %s", errEvt.Error.Message)
			}
		}
	}

	// Scanner exits the loop on read error too (e.g. a line exceeding the
	// buffer cap, or a dropped connection). Surface it instead of treating a
	// truncated stream as complete.
	if err := scanner.Err(); err != nil {
		return nil, usage, fmt.Errorf("stream read error: %w", err)
	}

	// Assemble content blocks in the same format as the Messages API. Anthropic
	// requires thinking/signature blocks to be replayed unchanged when the
	// assistant also used tools; dropping them causes the next tool round to be
	// rejected even though thinking text is not rendered to the user.
	var content []map[string]interface{}
	hasActionableContent := false
	hasToolUse := false
	for _, b := range blocks {
		if b.Type == "tool_use" {
			hasToolUse = true
			break
		}
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": b.Text,
			})
			hasActionableContent = true
		case "thinking":
			if hasToolUse && b.Signature == "" {
				return nil, usage, fmt.Errorf("Anthropic thinking block is missing the signature required for tool continuation")
			}
			content = append(content, map[string]interface{}{
				"type":      "thinking",
				"thinking":  b.Text,
				"signature": b.Signature,
			})
		case "redacted_thinking":
			if b.Data == "" {
				return nil, usage, fmt.Errorf("Anthropic redacted_thinking block is missing encrypted data")
			}
			content = append(content, map[string]interface{}{
				"type": "redacted_thinking",
				"data": b.Data,
			})
		case "tool_use":
			if b.ID == "" || b.Name == "" {
				return nil, usage, fmt.Errorf("Anthropic tool call is missing id or name")
			}
			var input interface{} = map[string]interface{}{}
			if b.Input != "" {
				if err := json.Unmarshal([]byte(b.Input), &input); err != nil {
					return nil, usage, fmt.Errorf("Anthropic tool call %s has invalid input: %w", b.Name, err)
				}
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    b.ID,
				"name":  b.Name,
				"input": input,
			})
			hasActionableContent = true
		}
	}
	if !hasActionableContent {
		return nil, usage, fmt.Errorf("Anthropic stream ended without text or tool calls")
	}

	result, _ := json.Marshal(content)
	return result, usage, nil
}

func parseSSEDataLine(line string) (string, bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}

// buildURL constructs the full API endpoint from a base URL.
func buildURL(baseURL string) string {
	if baseURL == "" {
		return defaultAnthropicURL
	}
	if strings.HasSuffix(baseURL, "/messages") {
		return baseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/messages"
	}
	return baseURL + "/v1/messages"
}

// callAPINonStreaming makes a single non-streaming API call (no SSE, no tools)
// and returns the raw content-blocks JSON. Used by context compaction, where we
// want a quiet one-shot summary without streaming to the console.
func (a *Agent) callAPINonStreaming() ([]byte, error) {
	if !isAnthropicProvider(a.Config) {
		return a.callOpenAINonStreaming()
	}
	url := buildURL(a.Config.BaseURL)
	model := a.Config.Model
	if model == "" {
		model = DefaultModel
	}
	body := map[string]interface{}{
		"model":      model,
		"max_tokens": 1024,
		"system":     "You are a helpful assistant that summarizes conversation history concisely.",
		"messages":   a.Messages,
		"stream":     false,
	}
	payload, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(a.context(), 60*time.Second)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.Config.APIKey)
	req.Header.Set("anthropic-version", "2025-01-25")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, truncateStr(string(errBody.Bytes()), 200, "..."))
	}

	var result struct {
		Content []map[string]interface{} `json:"content"`
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse non-stream response: %w", err)
	}
	return json.Marshal(result.Content)
}

// callOpenAINonStreaming makes a non-streaming OpenAI API call for compaction.
func (a *Agent) callOpenAINonStreaming() ([]byte, error) {
	if isResponsesProvider(a.Config) {
		return a.callResponsesNonStreaming()
	}
	url := buildOpenAIURL(a.Config.BaseURL)
	model := a.Config.Model
	if model == "" {
		model = DefaultModel
	}
	body := map[string]interface{}{
		"model":    model,
		"messages": convertMessagesToOpenAI("You are a helpful assistant that summarizes conversation history concisely.", a.Messages),
		"stream":   false,
	}
	payload, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(a.context(), 60*time.Second)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Config.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, truncateStr(string(errBody.Bytes()), 200, "..."))
	}

	return parseOpenAINonStreaming(resp.Body)
}

// callResponsesNonStreaming is the quiet compaction path for Responses API
// providers. Codex gateways may return SSE even for summary requests, so use
// the same parser without rendering the assistant text into the terminal.
func (a *Agent) callResponsesNonStreaming() ([]byte, error) {
	url := buildResponsesURL(a.Config.BaseURL)
	payload := buildResponsesPayloadWithTools(
		a.Config,
		"You are a helpful assistant that summarizes conversation history concisely.",
		a.Messages,
		nil,
	)

	ctx, cancel := context.WithTimeout(a.context(), 60*time.Second)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Config.APIKey)

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, truncateStr(string(errBody.Bytes()), 200, "..."))
	}
	content, _, err := parseResponsesStream(resp.Body, false)
	return content, err
}

// supportsThinking reports whether a model name supports extended thinking.
// Only Claude 3.5+ and Claude 4 models (anthropic) support it; other providers
// and older model families are excluded.
func supportsThinking(model string) bool {
	m := strings.ToLower(model)
	// Only enable for known Anthropic model families that support thinking.
	switch {
	case strings.Contains(m, "claude-sonnet-4"),
		strings.Contains(m, "claude-3-5-sonnet"),
		strings.Contains(m, "claude-opus-4"),
		strings.Contains(m, "claude-3-opus"),
		strings.Contains(m, "claude-3.5"):
		return true
	}
	return false
}
