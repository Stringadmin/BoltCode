package agent

import "testing"

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"empty defaults to anthropic", "", "https://api.anthropic.com/v1/messages"},
		{"already ends with /messages", "https://proxy.example.com/v1/messages", "https://proxy.example.com/v1/messages"},
		{"ends with /v1", "https://proxy.example.com/v1", "https://proxy.example.com/v1/messages"},
		{"bare host", "https://proxy.example.com", "https://proxy.example.com/v1/messages"},
		{"trailing slash stripped", "https://proxy.example.com/", "https://proxy.example.com/v1/messages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildURL(tt.baseURL)
			if got != tt.want {
				t.Errorf("buildURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
