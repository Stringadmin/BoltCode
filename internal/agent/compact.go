package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// compactMessages summarizes the older portion of the conversation into a single
// message, keeping the most recent `keepRecent` messages verbatim. It calls the
// LLM with a compact summarization prompt. On any error it falls back to a
// hard truncation (keep first context msg + recent) so the session never breaks.
//
// This is the core of context management: when usage nears the window, we trade
// detailed history for a condensed summary so the conversation can continue
// without hitting the token limit.
func (a *Agent) compactMessages(keepRecent int) error {
	if len(a.Messages) < 2 {
		return nil // nothing to compact
	}
	if keepRecent >= len(a.Messages) {
		keepRecent = len(a.Messages) - 1
	}
	if keepRecent < 1 {
		keepRecent = 1
	}
	split := len(a.Messages) - keepRecent
	// Never cut between an assistant tool_use message and its required user
	// tool_result message; doing so creates an invalid history on the next API
	// request even though both messages existed before compaction.
	if split > 0 && split < len(a.Messages) && messageHasToolResults(a.Messages[split]) {
		split--
		if split == 0 {
			split = len(a.Messages)
		}
	}
	older := a.Messages[:split]
	recent := a.Messages[split:]

	summary, err := a.summarizeHistory(older)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		// Fallback: hard truncation. Keep the first message (project context)
		// + recent. Better than a broken session.
		summary = fallbackSummary(older)
	}

	// Rebuild around the summary plus recent turns. The original first message
	// can contain a very large project scan; preserving it verbatim defeated
	// compaction and caused the next API call to overflow again.
	rebuilt := []Message{{
		Role: "user",
		Content: "[Context summary — earlier conversation was compacted to save space]\n" + summary +
			"\n[End summary. The recent messages below continue from here.]",
	}}
	rebuilt = append(rebuilt, recent...)
	a.Messages = rebuilt
	return nil
}

func messageHasToolResults(message Message) bool {
	if message.Role != "user" {
		return false
	}
	for _, block := range messageContentBlocks(message.Content) {
		if kind, _ := block["type"].(string); kind == "tool_result" {
			return true
		}
	}
	return false
}

// summarizeHistory asks the LLM to produce a concise summary of the given
// messages: decisions made, files touched, current task state, open questions.
func (a *Agent) summarizeHistory(msgs []Message) (string, error) {
	// Build a compact transcript of the older messages.
	var transcript strings.Builder
	for _, m := range msgs {
		role := m.Role
		text := contentToText(m.Content)
		if len([]rune(text)) > 600 {
			text = string([]rune(text)[:600]) + "…"
		}
		transcript.WriteString(role + ": " + text + "\n\n")
	}

	prompt := "Summarize the following conversation history concisely for context retention. " +
		"Capture: key decisions, files created/edited and why, the current task and its progress, " +
		"any open questions or blockers. Be factual and compact (a few short paragraphs max).\n\n" +
		"--- HISTORY ---\n" + transcript.String() + "\n--- END ---"

	// One-shot non-streaming call for the summary. We reuse callAPI machinery
	// by temporarily swapping messages, then restore.
	saved := a.Messages
	a.Messages = []Message{{Role: "user", Content: prompt}}
	// Use a non-streaming summary fetch to avoid console noise.
	summary, err := a.fetchSummary()
	a.Messages = saved
	if err != nil {
		return "", err
	}
	return summary, nil
}

// fetchSummary does a single non-streaming API call and returns the assistant
// text. Used by compaction (output shouldn't stream to the console).
func (a *Agent) fetchSummary() (string, error) {
	body, err := a.callAPINonStreaming()
	if err != nil {
		return "", err
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(body, &blocks); err != nil {
		return "", err
	}
	for _, b := range blocks {
		if t, _ := b["type"].(string); t == "text" {
			if s, ok := b["text"].(string); ok {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("no text in summary response")
}

// fallbackSummary builds a crude summary by listing roles + truncated snippets,
// used when the LLM summary call fails.
func fallbackSummary(msgs []Message) string {
	var b strings.Builder
	b.WriteString("(LLM summary unavailable; crude truncation below)\n")
	for _, m := range msgs {
		t := contentToText(m.Content)
		if len([]rune(t)) > 120 {
			t = string([]rune(t)[:120]) + "…"
		}
		b.WriteString(m.Role + ": " + t + "\n")
	}
	return b.String()
}

// contentToText extracts a plain-text view of a message content (string or
// content-block slice).
func contentToText(c interface{}) string {
	switch v := c.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if mm, ok := item.(map[string]interface{}); ok {
				if s, ok := mm["text"].(string); ok {
					parts = append(parts, s)
				}
				if s, ok := mm["content"].(string); ok {
					parts = append(parts, "[tool result] "+s)
				}
				if n, ok := mm["name"].(string); ok {
					parts = append(parts, "[tool: "+n+"]")
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprintf("%v", c)
}
