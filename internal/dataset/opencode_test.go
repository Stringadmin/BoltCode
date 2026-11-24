package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeOpenCodeExportFiltersReasoningAndRedactsSecrets(t *testing.T) {
	payload := `{
  "info": {
    "id": "ses_test",
    "projectID": "global",
    "directory": "F:/dianqi",
    "title": "test",
    "version": "1.18.1",
    "agent": "build",
    "model": {"id":"teacher-code","providerID":"teacher","variant":"high"}
  },
  "messages": [
    {
      "info": {"id":"m1","role":"user"},
      "parts": [
        {"type":"text","text":"fix this; OPENAI_API_KEY=sk-abcdefghijklmnop"}
      ]
    },
    {
      "info": {"id":"m2","role":"assistant","agent":"build","model":{"modelID":"teacher-code","providerID":"teacher"}},
      "parts": [
        {"type":"step-start"},
        {"type":"reasoning","text":"private chain of thought"},
        {"type":"tool","callID":"call_1","tool":"bash","state":{"input":{"command":"go test ./...","password":"do-not-keep"},"output":"Authorization=Bearer abcdefghijklmnop","status":"error","title":"run tests","time":{"start":10,"end":20}}},
        {"type":"text","text":"Tests failed; token sk-qrstuvwxyz123456 was removed."},
        {"type":"step-finish"}
      ]
    }
  ]
}`

	trace, err := NormalizeOpenCodeExport([]byte(payload), openCodeSession{
		ID:        "ses_test",
		Directory: "F:/dianqi",
		Created:   1,
		Updated:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if trace.SchemaVersion != SchemaVersion || !trace.Source.Sanitized {
		t.Fatalf("unexpected trace metadata: %+v", trace)
	}
	if len(trace.Messages) != 2 {
		t.Fatalf("kept messages = %d, want 2", len(trace.Messages))
	}
	if trace.Stats.DroppedReasoningParts != 1 || trace.Stats.DroppedControlParts != 2 {
		t.Fatalf("unexpected dropped stats: %+v", trace.Stats)
	}
	if trace.Stats.ToolExecutions != 1 || trace.Stats.ToolErrors != 1 {
		t.Fatalf("unexpected tool stats: %+v", trace.Stats)
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"private chain of thought", "do-not-keep", "abcdefghijklmnop", "qrstuvwxyz123456"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("normalized trace leaked %q: %s", forbidden, text)
		}
	}
	if trace.Stats.Redactions < 4 {
		t.Fatalf("redactions = %d, want at least 4", trace.Stats.Redactions)
	}
}

func TestImportOpenCodeFiltersProjectAndUsesSanitizedExport(t *testing.T) {
	list := `[
  {"id":"ses_other","projectId":"global","directory":"F:/other","title":"other","created":1,"updated":2},
  {"id":"ses_keep","projectId":"global","directory":"f:/DIANQI","title":"keep","created":3,"updated":4}
]`
	export := `{
  "info":{"id":"ses_keep","projectID":"global","directory":"F:/dianqi","title":"keep","version":"1.18.1","model":{}},
  "messages":[{"info":{"id":"m1","role":"user"},"parts":[{"type":"text","text":"hello"}]}]
}`
	var calls [][]string
	runner := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		call := append([]string{dir, name}, args...)
		calls = append(calls, call)
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "session list"):
			return []byte(list), nil
		case strings.HasPrefix(joined, "export ses_keep"):
			if !strings.Contains(joined, "--sanitize") || !strings.Contains(joined, "--pure") {
				return nil, fmt.Errorf("unsafe export args: %s", joined)
			}
			return []byte(export), nil
		default:
			return nil, fmt.Errorf("unexpected command: %s", joined)
		}
	}

	traces, summary, err := ImportOpenCodeWithRunner(context.Background(), OpenCodeOptions{
		Executable: "opencode-test",
		ProjectDir: `F:\dianqi`,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 1 || traces[0].Source.SessionID != "ses_keep" {
		t.Fatalf("unexpected traces: %+v", traces)
	}
	if summary.Sessions != 1 || summary.Messages != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want list + one export", len(calls))
	}
}

func TestImportOpenCodeRawSourceStillUsesPureAndSkipsSanitize(t *testing.T) {
	runner := func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "session list") {
			return []byte(`[{"id":"ses_raw","directory":"F:/dianqi"}]`), nil
		}
		if strings.HasPrefix(joined, "export ses_raw") {
			if strings.Contains(joined, "--sanitize") || !strings.Contains(joined, "--pure") {
				return nil, fmt.Errorf("unexpected raw export args: %s", joined)
			}
			return []byte(`{"info":{"id":"ses_raw","directory":"F:/dianqi"},"messages":[]}`), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", joined)
	}
	_, _, err := ImportOpenCodeWithRunner(context.Background(), OpenCodeOptions{
		Executable:     "opencode-test",
		ProjectDir:     "F:/dianqi",
		AllowRawSource: true,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportOpenCodeCanExportExplicitChildSessionMissingFromList(t *testing.T) {
	runner := func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "session list") {
			return []byte(`[]`), nil
		}
		if strings.HasPrefix(joined, "export ses_child") {
			return []byte(`{"info":{"id":"ses_child","directory":"F:/dianqi"},"messages":[]}`), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", joined)
	}
	traces, _, err := ImportOpenCodeWithRunner(context.Background(), OpenCodeOptions{
		Executable: "opencode-test",
		ProjectDir: "F:/dianqi",
		SessionIDs: []string{"ses_child"},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 1 || traces[0].Source.SessionID != "ses_child" {
		t.Fatalf("unexpected child traces: %+v", traces)
	}
}

func TestImportOpenCodeRejectsInvalidRequestedSessionID(t *testing.T) {
	runner := func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte(`[]`), nil
	}
	_, _, err := ImportOpenCodeWithRunner(context.Background(), OpenCodeOptions{
		Executable: "opencode-test",
		ProjectDir: "F:/dianqi",
		SessionIDs: []string{"ses_ok;rm"},
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "invalid OpenCode session id") {
		t.Fatalf("error = %v, want invalid session id", err)
	}
}

func TestMarshalJSONLWritesOneTracePerLine(t *testing.T) {
	data, err := MarshalJSONL([]Trace{
		{SchemaVersion: SchemaVersion, Source: TraceSource{SessionID: "one"}},
		{SchemaVersion: SchemaVersion, Source: TraceSource{SessionID: "two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
}
