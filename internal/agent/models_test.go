package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestModelsForProviderNormalizesProviderName(t *testing.T) {
	models := ModelsForProvider("  DeEpSeEk  ")
	if len(models) != 2 || models[0].ID != "deepseek-chat" || models[1].ID != "deepseek-reasoner" {
		t.Fatalf("DeepSeek models = %+v", models)
	}
	if models := ModelsForProvider("unknown-provider"); models != nil {
		t.Fatalf("unknown provider models = %+v, want nil", models)
	}
	if models := ModelsForProvider(""); len(models) == 0 || models[0].ID != "claude-sonnet-4-20250514" {
		t.Fatalf("default provider models = %+v", models)
	}
}

func TestAvailableModelsIncludesCompatibleCustomProfilesOnly(t *testing.T) {
	restore := withTempHome(t)
	defer restore()
	profiles := ProfileFile{Profiles: []Profile{
		{Name: "same-endpoint", Provider: "custom", Model: "custom-b", BaseURL: "https://local.example/v1"},
		{Name: "duplicate-current", Provider: "CUSTOM", Model: "CUSTOM-A", BaseURL: "https://local.example/v1/"},
		{Name: "other-endpoint", Provider: "custom", Model: "custom-c", BaseURL: "https://other.example/v1"},
		{Name: "other-provider", Provider: "openai", Model: "gpt-4o", BaseURL: "https://local.example/v1"},
	}}
	data, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profilePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	models := AvailableModels(Config{Provider: "custom", Model: "custom-a", BaseURL: "https://local.example/v1/"})
	if len(models) != 2 {
		t.Fatalf("available custom models = %+v, want current plus one compatible profile", models)
	}
	if models[0].ID != "custom-a" || models[1].ID != "custom-b" {
		t.Fatalf("available custom model order = %+v", models)
	}
}

func TestProviderByIDIsCaseInsensitive(t *testing.T) {
	provider, ok := ProviderByID("VoLc")
	if !ok || provider.ID != "volc" || provider.BaseURL == "" || provider.Custom {
		t.Fatalf("ProviderByID(VoLc) = %+v, %v", provider, ok)
	}
	if _, ok := ProviderByID("missing"); ok {
		t.Fatal("unknown provider unexpectedly resolved")
	}
}

func TestAvailableModelsIncludesCurrentAndConfiguredModels(t *testing.T) {
	restore := withTempHome(t)
	defer restore()
	if err := SaveProfiles(&ProfileFile{
		Profiles: []Profile{
			{Name: "v4", Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-pro"},
			{Name: "coder", Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1/", Model: "deepseek-coder"},
			{Name: "other-endpoint", Provider: "deepseek", BaseURL: "https://other.example/v1", Model: "private-model"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	models := AvailableModels(Config{Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-pro"})
	if len(models) < 4 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].ID != "deepseek-v4-pro" || models[0].Label != "Configured model · current" {
		t.Fatalf("current model not first: %+v", models[0])
	}
	want := map[string]bool{"deepseek-v4-pro": false, "deepseek-coder": false, "deepseek-chat": false, "deepseek-reasoner": false}
	for _, model := range models {
		if _, ok := want[model.ID]; ok {
			want[model.ID] = true
		}
		if model.ID == "private-model" {
			t.Fatal("model from another endpoint leaked into list")
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing model %q: %+v", id, models)
		}
	}
}

func TestAvailableModelsDeduplicatesBuiltInCurrentModel(t *testing.T) {
	restore := withTempHome(t)
	defer restore()
	models := AvailableModels(Config{Provider: "deepseek", Model: "deepseek-chat"})
	count := 0
	for _, model := range models {
		if model.ID == "deepseek-chat" {
			count++
		}
	}
	if count != 1 || models[0].ID != "deepseek-chat" {
		t.Fatalf("built-in current model duplicated: %+v", models)
	}
}
