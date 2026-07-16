package shell

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/gitx"
)

func (s *Session) showHelp() {
	teamMode := s.App != nil && s.App.IsTeamMode()

	solo := `
  Commands (solo mode):
    run [instruction]    launch AI agent here (no task/worktree needed)
    config               manage AI provider profiles
    setup                connect a remote (GitHub / self-hosted)
    spec [init]          show / create project spec
    push / pull          push to / pull from remote
    team init            upgrade to team collaboration mode
    help / ?             this message
    exit / quit / q      leave
`
	team := `
  Commands (team mode):
    status / st          team dashboard
    task new <intent>    create a task + worktree
    task ls              list all tasks
    task claim <id>      claim a task
    run <task-id>        launch AI agent in task worktree
    submit <task> <msg>  submit work as a proposal
    review               list pending proposals
    diff <proposal>      show proposal diff
    approve <proposal>   approve a proposal
    merge <proposal>     approve + merge
    reject <proposal> <note>  send back with note
    sync                 sync metadata with team
    setup                configure remote
    spec [init]          show / create project spec
    push / pull          push to / pull from remote
    help / ?             this message
    exit / quit / q      leave
`
	if teamMode {
		fmt.Println(infoStyle.Render(team))
	} else {
		fmt.Println(infoStyle.Render(solo))
	}
}

func (s *Session) runInit() {
	wd, _ := os.Getwd()
	// Pre-fill the identity from git config so the user can just press Enter.
	var suggested string
	if repo, err := gitx.Open(wd); err == nil {
		suggested = repo.ConfigUser()
	}
	prompt := "  Your name (for team collaboration): "
	if suggested != "" {
		prompt = fmt.Sprintf("  Your name (for team collaboration) [%s]: ", suggested)
	}
	user := s.ask(prompt)
	if user == "" {
		user = suggested
	}
	if user == "" {
		user = "anonymous"
	}
	if err := command.Init(wd, user); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	app, err := command.Load(wd)
	if err == nil {
		s.App = app
	}
}

func (s *Session) ensureApp() bool {
	if s.App == nil {
		fmt.Println(errStyle.Render("  not in a boltcode project — run 'init' first"))
		return false
	}
	return true
}

func (s *Session) runTask(args []string) {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("task"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(args) == 0 {
		fmt.Println(errStyle.Render("  usage: task <new|ls|claim> ..."))
		return
	}
	switch args[0] {
	case "new":
		intent := strings.Join(args[1:], " ")
		t, err := s.App.TaskNew(intent, "builtin", "")
		if err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render(fmt.Sprintf("  created %s", t.ID)))
		fmt.Println(infoStyle.Render(fmt.Sprintf("  branch:   %s", t.Branch)))
		fmt.Println(infoStyle.Render(fmt.Sprintf("  worktree: %s", t.Worktree)))
		fmt.Println(infoStyle.Render(fmt.Sprintf("  launch the agent: run %s", t.ID)))
	case "ls":
		tasks, err := s.App.ListTasks()
		if err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		if len(tasks) == 0 {
			fmt.Println(infoStyle.Render("  no tasks yet"))
			return
		}
		for _, t := range tasks {
			who := ""
			if t.Assignee != "" {
				who = " @" + t.Assignee
			}
			fmt.Printf("  %-10s [%-9s] %s%s\n", t.ID, t.Status, t.Intent, who)
		}
	case "claim":
		if len(args) < 2 {
			fmt.Println(errStyle.Render("  usage: task claim <id>"))
			return
		}
		who := s.App.WhoAmI()
		t, err := s.App.TaskClaim(args[1], who)
		if err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render(fmt.Sprintf("  %s claimed by %s", t.ID, t.Assignee)))
	default:
		fmt.Println(errStyle.Render("  unknown: task " + args[0]))
	}
}

func (s *Session) runSubmit(args []string) {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("submit"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(args) < 2 {
		fmt.Println(errStyle.Render("  usage: submit <task> <summary>"))
		return
	}
	summary := strings.Join(args[1:], " ")
	p, err := s.App.Submit(args[0], summary, "")
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	fmt.Println(okStyle.Render(fmt.Sprintf("  submitted %s", p.ID)))
}

func (s *Session) runReview() {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("review"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	pending, err := s.App.ListPending()
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(pending) == 0 {
		fmt.Println(infoStyle.Render("  nothing to review — all caught up"))
		return
	}
	fmt.Printf("  %d proposal(s) pending:\n\n", len(pending))
	for _, p := range pending {
		stat, _ := s.App.Repo.DiffStat(p.BaseCommit, p.Commit)
		fmt.Printf("  %-10s [%-12s] %s\n", p.ID, p.Agent, p.Summary)
		for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
			if line != "" {
				fmt.Printf("    %s\n", line)
			}
		}
	}
}

func (s *Session) runDiff(args []string) {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("diff"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(args) < 1 {
		fmt.Println(errStyle.Render("  usage: diff <proposal>"))
		return
	}
	p, err := s.App.Store.GetProposal(args[0])
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	diff, err := s.App.ProposalDiff(p)
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if diff == "" {
		fmt.Println(infoStyle.Render("  (no changes)"))
		return
	}
	fmt.Println(diff)
}

func (s *Session) runApprove(args []string, merge bool) {
	if !s.ensureApp() {
		return
	}
	cmd := "approve"
	if merge {
		cmd = "merge"
	}
	if err := s.App.RequireTeam(cmd); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(args) < 1 {
		fmt.Println(errStyle.Render("  usage: approve/merge <proposal>"))
		return
	}
	reviewer := s.App.WhoAmI()
	if !merge {
		p, err := s.App.Approve(args[0], reviewer)
		if err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render(fmt.Sprintf("  approved %s", p.ID)))
	} else {
		t, err := s.App.ApproveAndMerge(args[0], reviewer)
		if err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render(fmt.Sprintf("  merged into %s (task %s done)", t.Base, t.ID)))
	}
}

func (s *Session) runReject(args []string) {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("reject"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if len(args) < 2 {
		fmt.Println(errStyle.Render("  usage: reject <proposal> <note>"))
		return
	}
	note := strings.Join(args[1:], " ")
	if err := s.App.Reject(args[0], s.App.WhoAmI(), note); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	fmt.Println(okStyle.Render(fmt.Sprintf("  rejected %s — task reopened", args[0])))
}

func (s *Session) runSpec(args []string) {
	if !s.ensureApp() {
		return
	}
	if len(args) > 0 && args[0] == "init" {
		if s.App.SpecExists() {
			fmt.Println(errStyle.Render("  spec already exists"))
			return
		}
		if err := s.App.SpecInit(); err != nil {
			fmt.Println(errStyle.Render("  " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render("  created .boltcode/spec.md"))
		return
	}
	content, err := s.App.SpecContent()
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	fmt.Println(content)
}

func (s *Session) runSync() {
	if !s.ensureApp() {
		return
	}
	if err := s.App.RequireTeam("sync"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	if err := s.App.Sync(""); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
}

// runTeam handles `team init` inside the session — upgrades to team mode.
func (s *Session) runTeam(args []string) {
	if !s.ensureApp() {
		return
	}
	if len(args) == 0 || args[0] != "init" {
		fmt.Println(errStyle.Render("  usage: team init [team-name]"))
		return
	}
	teamName := strings.Join(args[1:], " ")
	wd, _ := os.Getwd()
	if err := command.TeamInit(wd, teamName); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	fmt.Println(okStyle.Render("  ✓ team mode enabled"))
	fmt.Println(infoStyle.Render("  now available: task / submit / review / approve / sync"))
}

func (s *Session) runPush() {
	if !s.ensureApp() {
		return
	}
	if err := s.App.Push(false); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
	}
}

func (s *Session) runPull() {
	if !s.ensureApp() {
		return
	}
	if err := s.App.Pull(); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
	}
}

// runConfig handles `config` — manage named API provider profiles. The user can
// keep several (e.g. anthropic / 火山方舟 / local) and switch with /profile.
// This wizard lists existing profiles, lets you create a new one or edit an
// existing one via a navigable field form, and sets the saved one as current.
func (s *Session) runConfig(args []string) {
	defer s.rl.SetPrompt(mainPrompt)
	fmt.Println()
	fmt.Println(titleStyle.Render("  BoltCode / 服务商配置"))
	fmt.Println(infoStyle.Render("  API Key 仅保存在本机，并以加密形式写入配置文件"))
	fmt.Println()

	// Show existing profiles + current.
	profiles, current, err := command.ListProfiles()
	if err != nil {
		fmt.Println(errStyle.Render("  读取配置失败: " + err.Error()))
		return
	}
	if len(profiles) > 0 {
		fmt.Println(titleStyle.Render("  配置档案"))
		for i, p := range profiles {
			marker := "○"
			if p.Name == current {
				marker = "●"
			}
			fmt.Printf("  %s [%d] %s\n", marker, i+1, p.Name)
			window := p.ContextWindow
			if window <= 0 {
				window = agent.ModelContextWindow(p.Model)
			}
			fmt.Println(infoStyle.Render(fmt.Sprintf("        %s · %s · context %s", providerDisplay(p.Provider), p.Model, agent.FormatContextWindow(window))))
		}
		fmt.Println()
	} else {
		fmt.Println(infoStyle.Render("  还没有配置档案，创建第一个即可开始使用。"))
		fmt.Println()
	}

	name := ""
	if len(profiles) > 0 {
		for name == "" {
			fmt.Println(infoStyle.Render("  输入编号或名称编辑 · n 新建 · 回车返回"))
			in, _, cancel := s.askWithBack("  选择: ")
			if cancel || in == "" {
				fmt.Println(infoStyle.Render("  已返回"))
				return
			}
			if strings.EqualFold(in, "n") || strings.EqualFold(in, "new") {
				name = s.ask("  新档案名 (如 work / local): ")
				break
			}
			var exists bool
			name, exists = profileNameFromInput(in, profiles)
			if !exists {
				fmt.Println(errStyle.Render("  未找到该档案；请输入列表编号，或用 n 新建"))
				name = ""
			}
		}
	} else {
		name = s.ask("  档案名 (如 work / local): ")
	}
	if name == "" {
		fmt.Println(infoStyle.Render("  已取消"))
		return
	}
	if existingName, exists := existingProfileName(name, profiles); exists {
		name = existingName
	}

	// Pre-fill from that profile if it exists, else defaults.
	prefill := &agent.Config{Provider: "anthropic", Model: agent.DefaultModel}
	isEditing := false
	for _, p := range profiles {
		if p.Name == name {
			c := p.ToConfig()
			prefill = &c
			isEditing = true
			break
		}
	}
	if isEditing {
		fmt.Println(infoStyle.Render(fmt.Sprintf("\n  正在编辑 %q；留空即可保留原值", name)))
	} else {
		fmt.Println(infoStyle.Render(fmt.Sprintf("\n  正在创建 %q", name)))
	}

	cfg, ok := s.editProfileFields(name, prefill)
	if !ok {
		fmt.Println(infoStyle.Render("  未保存任何更改"))
		return
	}
	if err := command.SaveProfile(name, cfg); err != nil {
		fmt.Println(errStyle.Render("  保存失败: " + err.Error()))
		return
	}
	fmt.Println()
	fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ 档案 %q 已保存并设为当前", name)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  %s · %s · context %s", providerDisplay(cfg.Provider), cfg.Model, agent.FormatContextWindow(cfg.ContextWindow))))
	fmt.Println(infoStyle.Render("  接下来可运行 run，或在会话中用 /profile 切换档案"))
}

// editProfileFields runs the navigable 5-field form for one profile, pre-filled
// from prefill. Returns the completed config and ok=true on save, ok=false on
// cancel.
func (s *Session) editProfileFields(name string, prefill *agent.Config) (*agent.Config, bool) {
	fmt.Println(infoStyle.Render("  回车保留 · b 返回上一步 · q 取消"))

	provider := prefill.Provider
	if provider == "" {
		provider = "anthropic"
	}
	apiKey := prefill.APIKey
	model := prefill.Model
	if model == "" {
		model = agent.DefaultModel
	}
	baseURL := prefill.BaseURL
	contextWindow := prefill.ContextWindow

	const (
		stepProvider = iota
		stepAPIKey
		stepModel
		stepBaseURL
		stepContextWindow
		stepConfirm
	)
	step := stepProvider

	for {
		switch step {
		case stepProvider:
			renderConfigStep(1, 5, "选择服务商")
			for i, p := range agent.KnownProviders {
				marker := " "
				if strings.EqualFold(provider, p.ID) {
					marker = "●"
				}
				fmt.Printf("  %s %d. %s [%s]\n", marker, i+1, p.Name, p.ID)
			}
			if _, known := agent.ProviderByID(provider); !known {
				fmt.Println(infoStyle.Render("    当前自定义 provider: " + provider))
			}
			in, back, cancel := s.askWithBack(fmt.Sprintf("  选择编号或 provider ID [%s]: ", provider))
			if cancel {
				return nil, false
			}
			if back {
				return nil, false
			}
			if in != "" {
				picked, valid := providerFromInput(in)
				if !valid {
					fmt.Println(errStyle.Render("  请输入列表中的编号，或自定义 provider ID"))
					continue
				}
				if !strings.EqualFold(provider, picked.ID) {
					provider = picked.ID
					baseURL = picked.BaseURL
					contextWindow = 0
					options := agent.ModelsForProvider(provider)
					if len(options) > 0 {
						model = options[0].ID
					} else {
						model = ""
					}
				}
			}
			step = stepAPIKey

		case stepAPIKey:
			renderConfigStep(2, 5, "配置 API Key")
			shown := "未设置"
			if apiKey != "" {
				shown = "已设置 " + maskKey(apiKey)
			}
			fmt.Println(infoStyle.Render("  输入内容仅显示为 *，不会写入命令历史；留空保留已有 Key"))
			in, back, cancel := s.askSecretWithBack(fmt.Sprintf("  API Key [%s]: ", shown))
			if cancel {
				return nil, false
			}
			if back {
				step = stepProvider
				continue
			}
			if in != "" {
				apiKey = in
			}
			if apiKey == "" {
				fmt.Println(errStyle.Render("  API Key 不能为空（输入值，或 q 取消）"))
				continue
			}
			step = stepModel

		case stepModel:
			renderConfigStep(3, 5, "选择模型")
			models := agent.AvailableModels(agent.Config{Provider: provider, BaseURL: baseURL, Model: model})
			for i, option := range models {
				marker := " "
				if option.ID == model {
					marker = "●"
				}
				label := option.Label
				if label == "" {
					label = option.ID
				}
				fmt.Printf("  %s %d. %s [%s]\n", marker, i+1, label, option.ID)
			}
			if len(models) == 0 {
				fmt.Println(infoStyle.Render("  此服务商没有内置模型列表，请输入模型 ID"))
			} else {
				fmt.Println(infoStyle.Render("  也可以直接输入列表外的模型 ID"))
			}
			in, back, cancel := s.askWithBack(fmt.Sprintf("  选择编号或模型 ID [%s]: ", orDefault(model, "未设置")))
			if cancel {
				return nil, false
			}
			if back {
				step = stepAPIKey
				continue
			}
			if in != "" {
				picked, valid := modelFromInput(in, models)
				if !valid {
					fmt.Println(errStyle.Render("  模型编号超出范围，请重新输入"))
					continue
				}
				model = picked
			}
			if model == "" {
				fmt.Println(errStyle.Render("  模型不能为空"))
				continue
			}
			step = stepBaseURL

		case stepBaseURL:
			renderConfigStep(4, 5, "确认 API 地址")
			defaultURL := providerDefaultURL(provider)
			shown := orDefault(baseURL, "服务商默认")
			if providerRequiresBaseURL(provider) {
				shown = orDefault(baseURL, "未设置")
				fmt.Println(infoStyle.Render("  自定义服务商需要完整的 http:// 或 https:// 地址"))
			} else {
				fmt.Println(infoStyle.Render("  留空保留当前值 · 输入 - 恢复服务商默认地址"))
			}
			in, back, cancel := s.askWithBack(fmt.Sprintf("  Base URL [%s]: ", shown))
			if cancel {
				return nil, false
			}
			if back {
				step = stepModel
				continue
			}
			if in == "-" {
				baseURL = defaultURL
			} else if in != "" {
				baseURL = in
			}
			if providerRequiresBaseURL(provider) && baseURL == "" {
				fmt.Println(errStyle.Render("  自定义服务商需要填写 Base URL"))
				continue
			}
			if err := validateBaseURL(baseURL); err != nil {
				fmt.Println(errStyle.Render("  " + err.Error()))
				continue
			}
			step = stepContextWindow

		case stepContextWindow:
			renderConfigStep(5, 5, "配置上下文窗口")
			effective := contextWindow
			shown := agent.FormatContextWindow(contextWindow)
			if effective <= 0 {
				effective = agent.ModelContextWindow(model)
				shown = fmt.Sprintf("auto (%s)", agent.FormatContextWindow(effective))
			}
			fmt.Println(infoStyle.Render("  用于状态栏和自动压缩判断；达到 80% 时自动总结旧对话"))
			fmt.Println(infoStyle.Render("  支持 128k、200000、1m；输入 - 恢复自动推断"))
			in, back, cancel := s.askWithBack(fmt.Sprintf("  Context Window [%s]: ", shown))
			if cancel {
				return nil, false
			}
			if back {
				step = stepBaseURL
				continue
			}
			if in != "" {
				parsed, err := agent.ParseContextWindow(in)
				if err != nil {
					fmt.Println(errStyle.Render("  " + err.Error()))
					continue
				}
				contextWindow = parsed
			}
			step = stepConfirm

		case stepConfirm:
			fmt.Println()
			fmt.Println(titleStyle.Render("  保存前确认"))
			fmt.Printf("  1  服务商   %s\n", providerDisplay(provider))
			fmt.Printf("  2  API Key  %s\n", maskKey(apiKey))
			fmt.Printf("  3  模型      %s\n", model)
			fmt.Printf("  4  地址      %s\n", orDefault(baseURL, "服务商默认"))
			contextLabel := agent.FormatContextWindow(contextWindow)
			if contextWindow <= 0 {
				contextLabel = fmt.Sprintf("auto (%s)", agent.FormatContextWindow(agent.ModelContextWindow(model)))
			}
			fmt.Printf("  5  上下文    %s tokens\n", contextLabel)
			fmt.Println(infoStyle.Render("     档案名    " + name))
			fmt.Println()
			in, back, cancel := s.askWithBack("  回车/Y 保存 · 1-5 修改对应项 · q 取消: ")
			if cancel {
				return nil, false
			}
			if back {
				step = stepContextWindow
				continue
			}
			switch strings.ToLower(in) {
			case "q", "quit", "cancel":
				return nil, false
			case "b", "back":
				step = stepBaseURL
				continue
			case "1":
				step = stepProvider
				continue
			case "2":
				step = stepAPIKey
				continue
			case "3":
				step = stepModel
				continue
			case "4":
				step = stepBaseURL
				continue
			case "5":
				step = stepContextWindow
				continue
			case "", "y", "yes":
				return &agent.Config{
					Provider:      provider,
					APIKey:        apiKey,
					Model:         model,
					BaseURL:       baseURL,
					ContextWindow: contextWindow,
				}, true
			default:
				fmt.Println(errStyle.Render("  请输入 Y、1-5 或 q"))
			}
		}
	}
}

func renderConfigStep(step, total int, title string) {
	progress := strings.TrimSpace(strings.Repeat("● ", step) + strings.Repeat("○ ", total-step))
	fmt.Println()
	fmt.Println(titleStyle.Render(fmt.Sprintf("  %d/%d  %s", step, total, title)))
	fmt.Println(infoStyle.Render("  " + progress))
}

func profileNameFromInput(input string, profiles []agent.Profile) (string, bool) {
	input = strings.TrimSpace(input)
	if n, err := strconv.Atoi(input); err == nil {
		if n < 1 || n > len(profiles) {
			return input, false
		}
		return profiles[n-1].Name, true
	}
	if name, ok := existingProfileName(input, profiles); ok {
		return name, true
	}
	return input, false
}

func existingProfileName(input string, profiles []agent.Profile) (string, bool) {
	for _, p := range profiles {
		if strings.EqualFold(p.Name, strings.TrimSpace(input)) {
			return p.Name, true
		}
	}
	return "", false
}

func providerFromInput(input string) (agent.ProviderInfo, bool) {
	input = strings.TrimSpace(input)
	if n, err := strconv.Atoi(input); err == nil {
		if n < 1 || n > len(agent.KnownProviders) {
			return agent.ProviderInfo{}, false
		}
		return agent.KnownProviders[n-1], true
	}
	if p, ok := agent.ProviderByID(input); ok {
		return p, true
	}
	if input == "" {
		return agent.ProviderInfo{}, false
	}
	return agent.ProviderInfo{ID: input, Name: input, Custom: true}, true
}

func modelFromInput(input string, models []agent.ModelOption) (string, bool) {
	input = strings.TrimSpace(input)
	if n, err := strconv.Atoi(input); err == nil && len(models) > 0 {
		if n < 1 || n > len(models) {
			return "", false
		}
		return models[n-1].ID, true
	}
	return input, input != ""
}

func providerDisplay(provider string) string {
	if strings.TrimSpace(provider) == "" {
		provider = "anthropic"
	}
	if p, ok := agent.ProviderByID(provider); ok {
		return fmt.Sprintf("%s [%s]", p.Name, p.ID)
	}
	return provider + " [自定义]"
}

func providerDefaultURL(provider string) string {
	if strings.TrimSpace(provider) == "" {
		provider = "anthropic"
	}
	if p, ok := agent.ProviderByID(provider); ok {
		return p.BaseURL
	}
	return ""
}

func providerRequiresBaseURL(provider string) bool {
	if strings.TrimSpace(provider) == "" {
		return false
	}
	p, ok := agent.ProviderByID(provider)
	return !ok || p.Custom
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("Base URL 需要是完整的 http:// 或 https:// 地址")
	}
	return nil
}

// maskKey renders an API key as "xxxx...yyyy" so it can be shown in prompts and
// the confirmation page without leaking the full secret. Short keys are fully
// masked.
func maskKey(k string) string {
	if k == "" {
		return "(空)"
	}
	r := []rune(k)
	if len(r) <= 8 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "..." + string(r[len(r)-4:])
}

// orDefault returns def when v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
