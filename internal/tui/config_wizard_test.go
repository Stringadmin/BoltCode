package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// withWizardTempHome redirects ~/.boltcode (where profiles live) to a temp dir.
func withWizardTempHome(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	return func() {}
}

func TestWizardStartsInCreateFormWhenNoProfiles(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	m := newConfigWizard()
	if m.mode != wizForm || !m.creating {
		t.Fatalf("expected create form, got mode=%v creating=%v", m.mode, m.creating)
	}
}

func TestWizardListNavigationAndDeleteFlow(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	seed := &agent.ProfileFile{Profiles: []agent.Profile{
		{Name: "a", Provider: "anthropic", APIKey: "k1", Model: "claude-x"},
		{Name: "b", Provider: "deepseek", APIKey: "k2", Model: "deepseek-chat"},
	}, Current: "a"}
	if err := agent.SaveProfiles(seed); err != nil {
		t.Fatal(err)
	}

	m := newConfigWizard()
	if m.mode != wizList || len(m.profiles) != 2 {
		t.Fatalf("mode=%v profiles=%d", m.mode, len(m.profiles))
	}

	m.Update(key(tea.KeyDown))
	if m.selected != 1 {
		t.Fatalf("selected=%d, want 1", m.selected)
	}

	m.Update(runes("d"))
	if m.mode != wizDeleteConfirm {
		t.Fatalf("mode=%v, want delete confirm", m.mode)
	}
	m.Update(runes("y"))
	if len(m.profiles) != 1 || m.profiles[0].Name != "a" {
		t.Fatalf("after delete: %+v", m.profiles)
	}
	if m.result.Deleted != "b" {
		t.Fatalf("result.Deleted=%q", m.result.Deleted)
	}
	// Deleting the non-current profile keeps the current one active.
	if m.current != "a" {
		t.Fatalf("current=%q, want a", m.current)
	}
}

func TestWizardSaveValidatesAndPersists(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	m := newConfigWizard()

	// Empty name → validation error, no save.
	m.saveForm()
	if m.formErr == "" {
		t.Fatal("expected validation error for empty name")
	}

	m.nameIn.SetValue("work")
	// DeepSeek (index 3) has known models; provider switch resets the model.
	m.setProvider(3)
	if m.providerID() != "deepseek" || m.model != "deepseek-chat" {
		t.Fatalf("provider=%q model=%q", m.providerID(), m.model)
	}
	m.apiKeyIn.SetValue("sk-test-key")
	m.baseIn.SetValue("https://api.deepseek.com/v1")
	m.ctxIn.SetValue("128k")
	m.effort = 3

	m.saveForm()
	if m.formErr != "" {
		t.Fatalf("saveForm error: %s", m.formErr)
	}
	if m.mode != wizList || m.result.Saved != "work" {
		t.Fatalf("mode=%v saved=%q", m.mode, m.result.Saved)
	}

	pf, err := agent.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if pf.Current != "work" || len(pf.Profiles) != 1 {
		t.Fatalf("saved profiles=%+v current=%q", pf.Profiles, pf.Current)
	}
	p := pf.Profiles[0]
	if p.Provider != "deepseek" || p.ContextWindow != 128_000 || p.ReasoningEffort != agent.ReasoningHigh {
		t.Fatalf("saved profile=%+v", p)
	}
}

func TestWizardCustomProviderRequiresBaseURL(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	m := newConfigWizard()
	m.nameIn.SetValue("local")
	customIdx := len(agent.KnownProviders) - 1
	m.setProvider(customIdx)
	m.customID.SetValue("ollama")
	m.model = "llama3"
	m.apiKeyIn.SetValue("sk-x")

	m.saveForm()
	if m.formErr == "" {
		t.Fatal("custom provider without Base URL should fail validation")
	}

	m.baseIn.SetValue("http://localhost:11434/v1")
	m.saveForm()
	if m.formErr != "" {
		t.Fatalf("saveForm error: %s", m.formErr)
	}
	pf, _ := agent.LoadProfiles()
	if pf.Profiles[0].Provider != "ollama" {
		t.Fatalf("provider=%q, want ollama", pf.Profiles[0].Provider)
	}
}

func TestWizardPickerFilterAndCustomModel(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	m := newConfigWizard()
	m.setProvider(3) // deepseek
	m.openPicker(pickModel)
	if m.mode != wizPicker || len(m.pickItems) != 2 {
		t.Fatalf("picker mode=%v items=%d", m.mode, len(m.pickItems))
	}

	// Filter down to reasoner.
	m.Update(runes("reason"))
	visible := m.filteredPickItems()
	if len(visible) != 1 || m.pickItems[visible[0]].id != "deepseek-reasoner" {
		t.Fatalf("filtered=%v", visible)
	}
	m.Update(key(tea.KeyEnter))
	if m.model != "deepseek-reasoner" || m.mode != wizForm {
		t.Fatalf("model=%q mode=%v", m.model, m.mode)
	}

	// No match → Enter adopts the typed text as a custom model id.
	m.openPicker(pickModel)
	m.Update(runes("my-private-model"))
	if len(m.filteredPickItems()) != 0 {
		t.Fatal("expected no matches")
	}
	m.Update(key(tea.KeyEnter))
	if m.model != "my-private-model" {
		t.Fatalf("custom model=%q", m.model)
	}
}

func TestWizardEditPrefillKeepsValues(t *testing.T) {
	restore := withWizardTempHome(t)
	defer restore()
	seed := &agent.ProfileFile{Profiles: []agent.Profile{
		{Name: "ark", Provider: "volc", APIKey: "sk-ark", Model: "glm-5.2",
			BaseURL: "https://ark.cn-beijing.volces.com/api/v3", ContextWindow: 192_000,
			ReasoningEffort: agent.ReasoningMedium},
	}, Current: "ark"}
	if err := agent.SaveProfiles(seed); err != nil {
		t.Fatal(err)
	}

	m := newConfigWizard()
	m.startEdit(m.profiles[0])
	if m.creating || m.original != "ark" {
		t.Fatalf("creating=%v original=%q", m.creating, m.original)
	}
	if m.providerID() != "volc" || m.model != "glm-5.2" || m.effort != 2 {
		t.Fatalf("provider=%q model=%q effort=%d", m.providerID(), m.model, m.effort)
	}
	if m.ctxIn.Value() != "192k" {
		t.Fatalf("ctx=%q", m.ctxIn.Value())
	}

	// Saving without touching anything must not wipe fields.
	m.saveForm()
	if m.formErr != "" {
		t.Fatalf("saveForm error: %s", m.formErr)
	}
	pf, _ := agent.LoadProfiles()
	p := pf.Profiles[0]
	if p.APIKey != "sk-ark" || p.ContextWindow != 192_000 || p.ReasoningEffort != agent.ReasoningMedium {
		t.Fatalf("round-trip profile=%+v", p)
	}
}
