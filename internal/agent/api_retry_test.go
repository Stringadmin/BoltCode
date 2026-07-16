package agent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withStubs replaces the package-level sleep and stderr hooks with inert
// versions so retry tests run instantly and quietly. It returns a restore func.
func withStubs(t *testing.T) func() {
	t.Helper()
	oldSleep := sleepFn
	oldErr := stderrWriter
	sleepFn = func(time.Duration) {}
	stderrWriter = io.Discard
	return func() {
		sleepFn = oldSleep
		stderrWriter = oldErr
	}
}

// TestRetryOnServerError verifies a 500 response is retried up to maxAttempts
// and the final error mentions the retry count.
func TestRetryOnServerError(t *testing.T) {
	restore := withStubs(t)
	defer restore()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &Agent{Config: Config{BaseURL: srv.URL, APIKey: "k"}}
	_, _, err := a.callAPI("sys")
	if err == nil {
		t.Fatal("expected error from repeated 500s, got nil")
	}
	if got, want := atomic.LoadInt32(&hits), int32(4); got != want {
		t.Errorf("request count = %d, want %d (one per attempt)", got, want)
	}
	if !strings.Contains(err.Error(), "after 4 attempts") {
		t.Errorf("error = %q, want it to mention attempt count", err.Error())
	}
}

// TestRetryOn429HonorsRetryAfter verifies a 429 is retried and the attempt
// count reflects maxAttempts when it keeps failing.
func TestRetryOn429HonorsRetryAfter(t *testing.T) {
	restore := withStubs(t)
	defer restore()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "0") // no real wait in tests
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	a := &Agent{Config: Config{BaseURL: srv.URL, APIKey: "k"}}
	_, _, err := a.callAPI("sys")
	if err == nil {
		t.Fatal("expected error from repeated 429s, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 4 {
		t.Errorf("request count = %d, want 4", got)
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, want mention of rate limited", err.Error())
	}
}

// TestNoRetryOn4xx verifies a 400 (non-retryable) surfaces immediately with a
// single request — no retry loop.
func TestNoRetryOn4xx(t *testing.T) {
	restore := withStubs(t)
	defer restore()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{Config: Config{BaseURL: srv.URL, APIKey: "k"}}
	_, _, err := a.callAPI("sys")
	if err == nil {
		t.Fatal("expected error from 400, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("request count = %d, want 1 (4xx must not retry)", got)
	}
	if !strings.Contains(err.Error(), "API 400") {
		t.Errorf("error = %q, want status code in message", err.Error())
	}
	if strings.Contains(err.Error(), "after 4 attempts") {
		t.Errorf("4xx must not report as retried: %q", err.Error())
	}
}

// TestRetrySucceedsEventually verifies that a server that fails a few times then
// succeeds returns the content without an error.
func TestRetrySucceedsEventually(t *testing.T) {
	restore := withStubs(t)
	defer restore()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		// Emit a minimal valid Anthropic SSE stream that ends cleanly.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	a := &Agent{Config: Config{BaseURL: srv.URL, APIKey: "k"}}
	content, _, err := a.callAPI("sys")
	if err != nil {
		t.Fatalf("expected eventual success, got: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("request count = %d, want 3 (2 failures then success)", got)
	}
	if len(content) == 0 {
		t.Error("expected non-empty content on success")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"5", 5 * time.Second},
		{"  10  ", 10 * time.Second},
		{"not-a-number", 0},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.in)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
