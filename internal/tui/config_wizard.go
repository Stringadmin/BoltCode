package tui

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

var (
	cfgBorderStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection)).Padding(1, 2)
	cfgActiveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorText)).Background(lipgloss.Color(theme.ColorPanel)).Bold(true)
	cfgNormalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSoftText))
	cfgFocusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorBrightText)).Background(lipgloss.Color(theme.ColorPanel))
)

// ConfigWizardResult summarizes what the wizard changed, so the caller (shell)
// can print a one-line follow-up after the TUI exits.
type ConfigWizardResult struct {
	Saved   string // name of the last saved profile (now active)
	Deleted string // name of the last deleted profile
}

type wizardMode int

const (
	wizList wizardMode = iota
	wizDeleteConfirm
	wizForm
	wizPicker
	wizTextEdit
)

type formField int

const (
	fieldName formField = iota
	fieldProvider
	fieldAPIKey
	fieldModel
	fieldBaseURL
	fieldContext
	fieldEffort
)

type pickerKind int

const (
	pickProvider pickerKind = iota
	pickModel
	pickEffort
)

type pickerItem struct {
	id    string
	label string
	desc  string
}

var effortOptions = []struct {
	value agent.ReasoningEffort
	desc  string
}{
	{agent.ReasoningAuto, "保持服务商默认 · 兼容性最好"},
	{agent.ReasoningLow, "更快、消耗更少，适合简单修改"},
	{agent.ReasoningMedium, "速度与推理深度平衡"},
	{agent.ReasoningHigh, "更深入，适合复杂架构和调试"},
	{agent.ReasoningXHigh, "适合需要更长推理时间的任务"},
}

type configWizardModel struct {
	mode     wizardMode
	profiles []agent.Profile
	current  string
	selected int
	notice   string

	// form state
	creating bool
	nameIn   textinput.Model
	provider int // index into agent.KnownProviders
	customID textinput.Model
	apiKeyIn textinput.Model
	model    string
	baseIn   textinput.Model
	ctxIn    textinput.Model
	effort   int
	focus    int // index into visibleFields()
	formErr  string
	original string // non-empty when editing an existing profile

	// picker state
	pickKind   pickerKind
	pickItems  []pickerItem
	pickFilter string
	pickSel    int

	// text edit state
	editing formField

	width, height int
	result        ConfigWizardResult
	quitting      bool
}

// RunConfigWizard opens the full-screen provider configuration UI. It loads
// and saves profiles itself; the caller only prints the returned summary.
func RunConfigWizard() (*ConfigWizardResult, error) {
	m := newConfigWizard()
	width, height := 90, 28
	if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	m.width, m.height = width, height
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	fm, ok := final.(*configWizardModel)
	if !ok {
		return &ConfigWizardResult{}, nil
	}
	return &fm.result, nil
}

func newConfigWizard() *configWizardModel {
	m := &configWizardModel{mode: wizList, provider: 0, model: agent.DefaultModel}
	m.nameIn = newInput("如 work / local / 火山方舟", false)
	m.customID = newInput("provider ID（如 ollama）", false)
	m.apiKeyIn = newInput("粘贴 API Key", true)
	m.baseIn = newInput("留空使用服务商默认地址", false)
	m.ctxIn = newInput("auto（如 128k / 200000 / 1m）", false)
	m.reload()
	if len(m.profiles) == 0 {
		m.startCreate()
	}
	return m
}

func newInput(placeholder string, password bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Width = 46
	if password {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

func (m *configWizardModel) reload() {
	pf, err := agent.LoadProfiles()
	if err != nil || pf == nil {
		pf = &agent.ProfileFile{}
	}
	m.profiles = pf.Profiles
	m.current = pf.Current
	if m.selected >= len(m.profiles) {
		m.selected = max(0, len(m.profiles)-1)
	}
}

func (m *configWizardModel) visibleFields() []formField {
	if m.creating {
		return []formField{fieldName, fieldProvider, fieldAPIKey, fieldModel, fieldBaseURL, fieldContext, fieldEffort}
	}
	return []formField{fieldProvider, fieldAPIKey, fieldModel, fieldBaseURL, fieldContext, fieldEffort}
}

func (m *configWizardModel) focusedField() formField {
	fields := m.visibleFields()
	if m.focus < 0 || m.focus >= len(fields) {
		return fieldProvider
	}
	return fields[m.focus]
}

func (m *configWizardModel) providerInfo() agent.ProviderInfo {
	if m.provider >= 0 && m.provider < len(agent.KnownProviders) {
		return agent.KnownProviders[m.provider]
	}
	return agent.KnownProviders[0]
}

func (m *configWizardModel) providerID() string {
	p := m.providerInfo()
	if p.Custom {
		if id := strings.TrimSpace(m.customID.Value()); id != "" {
			return id
		}
		return "custom"
	}
	return p.ID
}

func (m *configWizardModel) startCreate() {
	m.creating = true
	m.original = ""
	m.mode = wizForm
	m.focus = 0
	m.formErr = ""
	m.nameIn.SetValue("")
	m.provider = 0
	m.customID.SetValue("")
	m.apiKeyIn.SetValue("")
	m.apiKeyIn.Placeholder = "粘贴 API Key"
	m.model = firstModelFor("anthropic")
	m.baseIn.SetValue("")
	m.ctxIn.SetValue("")
	m.effort = 0
}

func (m *configWizardModel) startEdit(p agent.Profile) {
	m.creating = false
	m.original = p.Name
	m.mode = wizForm
	m.focus = 0
	m.formErr = ""
	m.nameIn.SetValue(p.Name)
	providerIdx := -1
	for i, kp := range agent.KnownProviders {
		if strings.EqualFold(kp.ID, p.Provider) && !kp.Custom {
			providerIdx = i
			break
		}
	}
	if providerIdx < 0 {
		for i, kp := range agent.KnownProviders {
			if kp.Custom {
				providerIdx = i
				break
			}
		}
		m.customID.SetValue(p.Provider)
	} else {
		m.customID.SetValue("")
	}
	m.provider = max(providerIdx, 0)
	m.apiKeyIn.SetValue(p.APIKey)
	m.apiKeyIn.Placeholder = "留空则清除 Key"
	m.model = p.Model
	if m.model == "" {
		m.model = firstModelFor(m.providerID())
	}
	m.baseIn.SetValue(p.BaseURL)
	if p.ContextWindow > 0 {
		m.ctxIn.SetValue(agent.FormatContextWindow(p.ContextWindow))
	} else {
		m.ctxIn.SetValue("")
	}
	m.effort = effortIndex(p.ReasoningEffort)
}

func firstModelFor(providerID string) string {
	if models := agent.ModelsForProvider(providerID); len(models) > 0 {
		return models[0].ID
	}
	return agent.DefaultModel
}

func effortIndex(e agent.ReasoningEffort) int {
	switch agent.NormalizeReasoningEffort(e) {
	case agent.ReasoningLow:
		return 1
	case agent.ReasoningMedium:
		return 2
	case agent.ReasoningHigh:
		return 3
	case agent.ReasoningXHigh:
		return 4
	default:
		return 0
	}
}

func (m *configWizardModel) Init() tea.Cmd { return nil }

func (m *configWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case wizList:
			return m.updateList(msg)
		case wizDeleteConfirm:
			return m.updateDeleteConfirm(msg)
		case wizForm:
			return m.updateForm(msg)
		case wizPicker:
			return m.updatePicker(msg)
		case wizTextEdit:
			return m.updateTextEdit(msg)
		}
	}
	return m, nil
}

// ── List ───────────────────────────────────────────────────────────────────

func (m *configWizardModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.selected > 0 {
			m.selected--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.selected < len(m.profiles)-1 {
			m.selected++
		}
	case tea.KeyEnter:
		if len(m.profiles) > 0 {
			m.startEdit(m.profiles[m.selected])
		} else {
			m.startCreate()
		}
	case tea.KeyEsc, tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyRunes:
		switch strings.ToLower(msg.String()) {
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "n":
			m.startCreate()
		case "d":
			if len(m.profiles) > 0 {
				m.mode = wizDeleteConfirm
			}
		case "a":
			if len(m.profiles) > 0 {
				name := m.profiles[m.selected].Name
				if _, err := agent.SwitchProfile(name); err != nil {
					m.notice = "切换失败: " + err.Error()
				} else {
					m.reload()
					m.notice = "已切换当前档案 → " + name
				}
			}
		}
	}
	return m, nil
}

func (m *configWizardModel) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.profiles) == 0 {
		m.mode = wizList
		return m, nil
	}
	name := m.profiles[m.selected].Name
	switch strings.ToLower(msg.String()) {
	case "y":
		if err := agent.DeleteProfile(name); err != nil {
			m.notice = "删除失败: " + err.Error()
		} else {
			m.result.Deleted = name
			m.notice = "已删除 " + name
			m.reload()
		}
		m.mode = wizList
	case "n", "":
		m.mode = wizList
	default:
		if msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter {
			m.mode = wizList
		}
	}
	return m, nil
}

// ── Form ───────────────────────────────────────────────────────────────────

func (m *configWizardModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		if m.focus > 0 {
			m.focus--
		}
	case tea.KeyDown, tea.KeyTab:
		if m.focus < len(m.visibleFields())-1 {
			m.focus++
		}
	case tea.KeyCtrlS:
		m.saveForm()
		return m, nil
	case tea.KeyEsc:
		m.mode = wizList
		m.formErr = ""
		return m, nil
	case tea.KeyEnter:
		switch m.focusedField() {
		case fieldName:
			m.beginTextEdit(fieldName)
		case fieldProvider:
			m.openPicker(pickProvider)
		case fieldAPIKey:
			m.beginTextEdit(fieldAPIKey)
		case fieldModel:
			m.openPicker(pickModel)
		case fieldBaseURL:
			m.beginTextEdit(fieldBaseURL)
		case fieldContext:
			m.beginTextEdit(fieldContext)
		case fieldEffort:
			m.openPicker(pickEffort)
		}
	}
	return m, nil
}

func (m *configWizardModel) beginTextEdit(f formField) {
	m.editing = f
	m.mode = wizTextEdit
	ti := m.inputFor(f)
	ti.CursorEnd()
	ti.Focus()
}

func (m *configWizardModel) inputFor(f formField) *textinput.Model {
	switch f {
	case fieldName:
		return &m.nameIn
	case fieldAPIKey:
		return &m.apiKeyIn
	case fieldBaseURL:
		return &m.baseIn
	case fieldContext:
		return &m.ctxIn
	case fieldProvider:
		return &m.customID
	}
	return &m.nameIn
}

func (m *configWizardModel) updateTextEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ti := m.inputFor(m.editing)
	switch msg.Type {
	case tea.KeyEnter:
		ti.Blur()
		m.mode = wizForm
		if m.focus < len(m.visibleFields())-1 {
			m.focus++
		}
		return m, nil
	case tea.KeyEsc:
		ti.Blur()
		m.mode = wizForm
		return m, nil
	case tea.KeyCtrlS:
		ti.Blur()
		m.mode = wizForm
		m.saveForm()
		return m, nil
	}
	var cmd tea.Cmd
	*ti, cmd = ti.Update(msg)
	if m.editing == fieldProvider {
		m.applyProviderCustomID()
	}
	return m, cmd
}

// applyProviderCustomID keeps the effective model sensible while the user
// types a custom provider id: switching to a known provider's id adopts its
// first model.
func (m *configWizardModel) applyProviderCustomID() {
	id := strings.TrimSpace(m.customID.Value())
	if id == "" {
		return
	}
	if _, known := agent.ProviderByID(id); known && m.model == "" {
		m.model = firstModelFor(id)
	}
}

func (m *configWizardModel) setProvider(idx int) {
	if idx == m.provider {
		return
	}
	m.provider = idx
	p := m.providerInfo()
	if p.Custom {
		m.model = ""
	} else {
		m.model = firstModelFor(p.ID)
	}
}

func (m *configWizardModel) saveForm() {
	name := strings.TrimSpace(m.nameIn.Value())
	if m.creating {
		if name == "" {
			m.formErr = "档案名不能为空"
			return
		}
		for _, p := range m.profiles {
			if strings.EqualFold(p.Name, name) {
				m.formErr = "档案名已存在: " + p.Name
				return
			}
		}
	} else {
		name = m.original
	}

	apiKey := strings.TrimSpace(m.apiKeyIn.Value())
	if apiKey == "" {
		m.formErr = "API Key 不能为空"
		return
	}
	model := strings.TrimSpace(m.model)
	if model == "" {
		m.formErr = "模型不能为空"
		return
	}
	baseURL := strings.TrimSpace(m.baseIn.Value())
	if err := validateWizardURL(baseURL); err != nil {
		m.formErr = err.Error()
		return
	}
	if m.providerInfo().Custom && baseURL == "" {
		m.formErr = "自定义服务商需要填写 Base URL"
		return
	}
	ctxWindow, err := agent.ParseContextWindow(m.ctxIn.Value())
	if err != nil {
		m.formErr = err.Error()
		return
	}

	cfg := &agent.Config{
		Provider:        m.providerID(),
		APIKey:          apiKey,
		Model:           model,
		BaseURL:         baseURL,
		ContextWindow:   ctxWindow,
		ReasoningEffort: effortOptions[m.effort].value,
	}
	if err := agent.SaveProfile(name, cfg); err != nil {
		m.formErr = "保存失败: " + err.Error()
		return
	}
	m.result.Saved = name
	m.notice = "✓ 已保存并设为当前: " + name
	m.reload()
	m.mode = wizList
	m.formErr = ""
}

func validateWizardURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("Base URL 需要是完整的 http:// 或 https:// 地址")
	}
	return nil
}

// ── Picker ─────────────────────────────────────────────────────────────────

func (m *configWizardModel) openPicker(kind pickerKind) {
	m.pickKind = kind
	m.pickFilter = ""
	m.pickSel = 0
	m.pickItems = nil
	switch kind {
	case pickProvider:
		for _, p := range agent.KnownProviders {
			desc := p.BaseURL
			if desc == "" {
				desc = "官方默认地址"
			}
			if p.Custom {
				desc = "任意 OpenAI 兼容端点"
			}
			m.pickItems = append(m.pickItems, pickerItem{id: p.ID, label: p.Name, desc: desc})
		}
		m.pickSel = m.provider
	case pickModel:
		providerID := m.providerID()
		for _, option := range agent.ModelsForProvider(providerID) {
			label := option.Label
			if label == "" {
				label = option.ID
			}
			m.pickItems = append(m.pickItems, pickerItem{id: option.ID, label: label, desc: option.ID})
		}
		for i, item := range m.pickItems {
			if item.id == m.model {
				m.pickSel = i
				break
			}
		}
	case pickEffort:
		for _, option := range effortOptions {
			m.pickItems = append(m.pickItems, pickerItem{id: string(option.value), label: string(option.value), desc: option.desc})
		}
		m.pickSel = m.effort
	}
	m.mode = wizPicker
}

func (m *configWizardModel) filteredPickItems() []int {
	if m.pickFilter == "" {
		indices := make([]int, len(m.pickItems))
		for i := range m.pickItems {
			indices[i] = i
		}
		return indices
	}
	query := strings.ToLower(m.pickFilter)
	var indices []int
	for i, item := range m.pickItems {
		if fuzzyMatch(strings.ToLower(item.label), query) || fuzzyMatch(strings.ToLower(item.id), query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *configWizardModel) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.filteredPickItems()
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.pickSel > 0 {
			m.pickSel--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.pickSel < len(visible)-1 {
			m.pickSel++
		}
	case tea.KeyEnter:
		if len(visible) > 0 {
			m.applyPick(m.pickItems[visible[m.pickSel]].id)
		} else if m.pickKind == pickModel && m.pickFilter != "" {
			m.applyPick(m.pickFilter)
		}
		m.mode = wizForm
		return m, nil
	case tea.KeyBackspace:
		if m.pickFilter != "" {
			runes := []rune(m.pickFilter)
			m.pickFilter = string(runes[:len(runes)-1])
			m.pickSel = 0
		}
	case tea.KeyEsc:
		if m.pickFilter != "" {
			m.pickFilter = ""
			m.pickSel = 0
		} else {
			m.mode = wizForm
		}
		return m, nil
	case tea.KeyRunes:
		m.pickFilter += msg.String()
		m.pickSel = 0
	}
	return m, nil
}

func (m *configWizardModel) applyPick(id string) {
	switch m.pickKind {
	case pickProvider:
		for i, p := range agent.KnownProviders {
			if p.ID == id {
				m.setProvider(i)
				break
			}
		}
		if m.providerInfo().Custom {
			m.beginTextEdit(fieldProvider)
		}
	case pickModel:
		m.model = id
	case pickEffort:
		for i, option := range effortOptions {
			if string(option.value) == id {
				m.effort = i
				break
			}
		}
	}
}

// ── View ───────────────────────────────────────────────────────────────────

func (m *configWizardModel) View() string {
	width, height := m.width, m.height
	if width < 60 {
		width = 60
	}
	if height < 14 {
		height = 14
	}
	boxWidth := min(width-8, 92)

	var lines []string
	switch m.mode {
	case wizList, wizDeleteConfirm:
		lines = m.viewList(boxWidth)
	case wizForm, wizTextEdit, wizPicker:
		lines = m.viewForm(boxWidth)
	}

	box := cfgBorderStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m *configWizardModel) viewList(boxWidth int) []string {
	lines := []string{
		chatTitleStyle.Render("⚡ 服务商配置"),
		chatDimStyle.Render("API Key 仅保存在本机，加密写入 ~/.boltcode/agent.json"),
		"",
	}
	if len(m.profiles) == 0 {
		lines = append(lines, chatDimStyle.Render("  还没有配置档案 — 按 n 新建"))
	}
	for i, p := range m.profiles {
		marker := "  "
		if p.Name == m.current {
			marker = chatGreenStyle.Render("● ")
		}
		window := p.ContextWindow
		if window <= 0 {
			window = agent.ModelContextWindow(p.Model)
		}
		summary := fmt.Sprintf("%s · %s · %s · thinking %s",
			providerLabel(p.Provider), p.Model, agent.FormatContextWindow(window), agent.FormatReasoningEffort(p.ReasoningEffort))
		row := fmt.Sprintf("%s%-18s %s", marker, p.Name, chatDimStyle.Render(summary))
		if i == m.selected {
			lines = append(lines, cfgActiveStyle.Width(boxWidth-8).Render("▶ "+row))
		} else {
			lines = append(lines, cfgNormalStyle.Render("  "+row))
		}
	}
	lines = append(lines, "")
	if m.mode == wizDeleteConfirm && len(m.profiles) > 0 {
		lines = append(lines, chatErrorStyle.Render(fmt.Sprintf("  删除档案 %q ?  y 确认 / n 取消", m.profiles[m.selected].Name)))
	} else if m.notice != "" {
		lines = append(lines, chatGreenStyle.Render("  "+m.notice))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, chatDimStyle.Render("  ↑↓ 选择 · Enter 编辑 · a 设为当前 · n 新建 · d 删除 · q 退出"))
	return lines
}

func (m *configWizardModel) viewForm(boxWidth int) []string {
	title := "新建配置档案"
	if !m.creating {
		title = fmt.Sprintf("编辑档案 · %s", m.original)
	}
	lines := []string{
		chatTitleStyle.Render(title),
		chatDimStyle.Render("↑↓ 移动 · Enter 修改 · Ctrl+S 保存 · Esc 返回"),
		"",
	}
	fields := m.visibleFields()
	for i, f := range fields {
		lines = append(lines, m.renderField(f, i == m.focus, boxWidth)...)
	}
	lines = append(lines, "")
	if m.formErr != "" {
		lines = append(lines, chatErrorStyle.Render("  ✗ "+m.formErr))
	} else {
		lines = append(lines, "")
	}
	if m.mode == wizPicker {
		lines = append(lines, m.viewPicker(boxWidth)...)
	}
	return lines
}

func (m *configWizardModel) renderField(f formField, focused bool, boxWidth int) []string {
	label := map[formField]string{
		fieldName:     "档案名",
		fieldProvider: "服务商",
		fieldAPIKey:   "API Key",
		fieldModel:    "模型",
		fieldBaseURL:  "Base URL",
		fieldContext:  "上下文窗口",
		fieldEffort:   "思考强度",
	}[f]

	var value string
	switch f {
	case fieldName:
		value = m.renderTextValue(fieldName, focused)
	case fieldProvider:
		p := m.providerInfo()
		value = fmt.Sprintf("%s [%s]", p.Name, m.providerID())
		if p.Custom && m.mode == wizTextEdit && m.editing == fieldProvider {
			value = m.customID.View()
		}
	case fieldAPIKey:
		value = m.renderTextValue(fieldAPIKey, focused)
	case fieldModel:
		value = m.model
		if value == "" {
			value = chatDimStyle.Render("(未设置 — Enter 选择)")
		} else if label := modelLabel(m.providerID(), m.model); label != "" {
			value = fmt.Sprintf("%s  %s", m.model, chatDimStyle.Render(label))
		}
	case fieldBaseURL:
		if m.baseIn.Value() == "" && !(m.mode == wizTextEdit && m.editing == fieldBaseURL) {
			def := m.providerInfo().BaseURL
			if def == "" {
				def = "官方默认"
			}
			value = chatDimStyle.Render("默认 · " + def)
		} else {
			value = m.renderTextValue(fieldBaseURL, focused)
		}
	case fieldContext:
		if m.ctxIn.Value() == "" && !(m.mode == wizTextEdit && m.editing == fieldContext) {
			value = chatDimStyle.Render(fmt.Sprintf("auto (%s)", agent.FormatContextWindow(agent.ModelContextWindow(m.model))))
		} else {
			value = m.renderTextValue(fieldContext, focused)
		}
	case fieldEffort:
		option := effortOptions[m.effort]
		value = fmt.Sprintf("%-7s %s", option.value, chatDimStyle.Render(option.desc))
	}

	row := fmt.Sprintf("  %-9s %s", label, value)
	if focused {
		return []string{cfgFocusedStyle.Width(boxWidth - 8).Render("▶" + row[2:])}
	}
	return []string{cfgNormalStyle.Render(row)}
}

func (m *configWizardModel) renderTextValue(f formField, focused bool) string {
	ti := m.inputFor(f)
	if m.mode == wizTextEdit && m.editing == f {
		return ti.View()
	}
	if f == fieldAPIKey {
		if ti.Value() == "" {
			return chatDimStyle.Render("(未设置)")
		}
		return maskWizardKey(ti.Value())
	}
	if ti.Value() == "" {
		return chatDimStyle.Render(ti.Placeholder)
	}
	return ti.Value()
}

func (m *configWizardModel) viewPicker(boxWidth int) []string {
	titles := map[pickerKind]string{
		pickProvider: "选择服务商",
		pickModel:    "选择模型（输入可筛选，Enter 使用自定义 ID）",
		pickEffort:   "选择思考强度",
	}
	lines := []string{
		"",
		chatTitleStyle.Render(titles[m.pickKind]),
		chatPromptStyle.Render("› ") + ansi.Strip(m.pickFilter) + chatDimStyle.Render("▏"),
	}
	visible := m.filteredPickItems()
	maxRows := 8
	start := m.pickSel - maxRows/2
	if start < 0 {
		start = 0
	}
	end := min(start+maxRows, len(visible))
	start = max(0, end-maxRows)
	if len(visible) == 0 {
		if m.pickKind == pickModel && m.pickFilter != "" {
			lines = append(lines, chatDimStyle.Render("  Enter 使用自定义模型: "+m.pickFilter))
		} else {
			lines = append(lines, chatDimStyle.Render("  (无匹配)"))
		}
	}
	for row := start; row < end; row++ {
		item := m.pickItems[visible[row]]
		text := fmt.Sprintf("%-28s %s", item.label, chatDimStyle.Render(item.desc))
		if row == m.pickSel {
			lines = append(lines, cfgActiveStyle.Width(boxWidth-8).Render("▶ "+text))
		} else {
			lines = append(lines, cfgNormalStyle.Render("  "+text))
		}
	}
	return lines
}

func providerLabel(id string) string {
	if p, ok := agent.ProviderByID(id); ok {
		return p.Name
	}
	return id
}

func modelLabel(providerID, modelID string) string {
	for _, option := range agent.ModelsForProvider(providerID) {
		if option.ID == modelID {
			return option.Label
		}
	}
	return ""
}

func maskWizardKey(k string) string {
	r := []rune(k)
	if len(r) <= 8 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "..." + string(r[len(r)-4:])
}
