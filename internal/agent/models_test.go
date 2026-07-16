package agent

import "testing"

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
