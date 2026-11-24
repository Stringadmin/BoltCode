package workbench

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/keyring"
)

func TestModelChannelsMigrateAndShareCredentials(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://example.invalid/v1"
	record := func(id, key string, effort agent.ReasoningEffort) savedModel {
		t.Helper()
		wrapped, err := keyring.Wrap(key)
		if err != nil {
			t.Fatal(err)
		}
		return savedModel{Model: Model{ID: id, Label: id, Name: id, Provider: "openai", BaseURL: endpoint, ReasoningEffort: effort}, Key: wrapped}
	}
	legacy := modelStore{Active: "second", Profiles: []savedModel{
		record("first", "old-channel-key", agent.ReasoningHigh),
		record("second", "old-channel-key", agent.ReasoningXHigh),
		record("separate", "different-key", agent.ReasoningLow),
	}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.modelPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	state := service.State()
	if len(state.ModelChannels) != 2 || len(state.Models) != 3 || state.Models[0].ChannelID != state.Models[1].ChannelID ||
		state.Models[2].ChannelID == state.Models[1].ChannelID || state.Model.ID != "second" || state.Model.ReasoningEffort != agent.ReasoningXHigh {
		t.Fatalf("incorrect migration: channels=%+v, models=%+v, active=%+v", state.ModelChannels, state.Models, state.Model)
	}
	sharedID := state.Model.ChannelID
	added, err := service.SaveChannelModel(sharedID, "", "Added model", "third-model", "medium")
	if err != nil || added.ChannelID != sharedID || added.ReasoningEffort != agent.ReasoningMedium {
		t.Fatalf("add model to shared channel: %+v, %v", added, err)
	}
	if _, err := service.SaveModelChannel(sharedID, "RightCode", "openai", endpoint, "rotated-channel-key"); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct{ id, key string }{
		{"first", "rotated-channel-key"}, {"second", "rotated-channel-key"},
		{added.ID, "rotated-channel-key"}, {"separate", "different-key"},
	} {
		if err := reopened.SelectModel(expected.id); err != nil {
			t.Fatal(err)
		}
		config, err := reopened.agentConfig()
		if err != nil || config.APIKey != expected.key || config.BaseURL != endpoint {
			t.Fatalf("model %s did not use its channel credentials: %v", expected.id, err)
		}
	}
	if _, err := reopened.SaveModelChannel(sharedID, "Wrong address", "openai", "https://another.invalid/v1", ""); err == nil {
		t.Fatal("stored channel credential was reused with a different endpoint")
	}
	raw, err = os.ReadFile(service.modelPath())
	if err != nil || strings.Contains(string(raw), "rotated-channel-key") || strings.Count(string(raw), "\"key\":") != 2 {
		t.Fatal("profile storage did not keep one wrapped key per channel")
	}
}
