package shell

import (
	"fmt"
	"os"
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
    config               set API key + model for the agent
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
	fmt.Println(titleStyle.Render("  ╔══════════════════════════════════════╗"))
	fmt.Println(titleStyle.Render("  ║   BoltCode — AI Agent 配置            ║"))
	fmt.Println(titleStyle.Render("  ╚══════════════════════════════════════╝"))
	fmt.Println()
	fmt.Println(infoStyle.Render("  管理服务商配置档案（API Key 存本地，不上传）"))
	fmt.Println()

	// Show existing profiles + current.
	profiles, current, _ := command.ListProfiles()
	if len(profiles) > 0 {
		fmt.Println(infoStyle.Render("  已有配置档案:"))
		for _, p := range profiles {
			marker := "  "
			if p.Name == current {
				marker = "▶ "
			}
			fmt.Printf("  %s%-14s %s / %s\n", marker, p.Name, p.Provider, p.Model)
		}
		fmt.Println()
	} else {
		fmt.Println(infoStyle.Render("  还没有配置档案，先创建一个。"))
		fmt.Println()
	}

	// Choose: new profile, or edit an existing one by name.
	fmt.Println(infoStyle.Render("  输入新档案名创建 · 输入已有档案名编辑 · 回车退出"))
	in, _, cancel := s.askWithBack("  档案名 (如 anthropic / 火山方舟): ")
	if cancel || in == "" {
		fmt.Println(errStyle.Render("  退出"))
		return
	}
	name := in

	// Pre-fill from that profile if it exists, else defaults.
	prefill := &agent.Config{Provider: "anthropic", Model: agent.DefaultModel}
	for _, p := range profiles {
		if p.Name == name {
			c := p.ToConfig()
			prefill = &c
			break
		}
	}

	cfg, ok := s.editProfileFields(name, prefill)
	if !ok {
		fmt.Println(errStyle.Render("  取消"))
		return
	}
	if err := command.SaveProfile(name, cfg); err != nil {
		fmt.Println(errStyle.Render("  保存失败: " + err.Error()))
		return
	}
	fmt.Println()
	fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ 档案 %q 已保存并设为当前", name)))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  provider: %s, model: %s", cfg.Provider, cfg.Model)))
	fmt.Println(infoStyle.Render("  切换档案用: /profile <名称>   ·   现在可以用 'run' 启动 AI 编程"))
}

// editProfileFields runs the navigable 4-field form for one profile, pre-filled
// from prefill. Returns the completed config and ok=true on save, ok=false on
// cancel.
func (s *Session) editProfileFields(name string, prefill *agent.Config) (*agent.Config, bool) {
	fmt.Println(infoStyle.Render("  提示：回车=保留当前值 · b=返回上一步 · q=取消"))
	fmt.Println()

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

	const (
		stepProvider = iota
		stepAPIKey
		stepModel
		stepBaseURL
		stepConfirm
	)
	step := stepProvider

	for {
		switch step {
		case stepProvider:
			in, _, cancel := s.askWithBack(fmt.Sprintf("  [1/4] API 提供商 [%s]: ", provider))
			if cancel {
				return nil, false
			}
			if in != "" {
				provider = in
			}
			step = stepAPIKey

		case stepAPIKey:
			shown := "未设置"
			if apiKey != "" {
				shown = maskKey(apiKey) + "（回车保留，输入新值覆盖）"
			}
			in, back, cancel := s.askWithBack(fmt.Sprintf("  [2/4] API Key [%s]: ", shown))
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
			in, back, cancel := s.askWithBack(fmt.Sprintf("  [3/4] 模型 [%s]: ", model))
			if cancel {
				return nil, false
			}
			if back {
				step = stepAPIKey
				continue
			}
			if in != "" {
				model = in
			}
			step = stepBaseURL

		case stepBaseURL:
			shown := baseURL
			if shown == "" {
				shown = "默认"
			}
			in, back, cancel := s.askWithBack(fmt.Sprintf("  [4/4] API 地址 [%s]: ", shown))
			if cancel {
				return nil, false
			}
			if back {
				step = stepModel
				continue
			}
			if in != "" {
				baseURL = in
			}
			step = stepConfirm

		case stepConfirm:
			fmt.Println()
			fmt.Println(infoStyle.Render("  ── 确认档案 " + name + " ──"))
			fmt.Printf("  提供商:  %s\n", provider)
			fmt.Printf("  API Key: %s\n", maskKey(apiKey))
			fmt.Printf("  模型:    %s\n", model)
			fmt.Printf("  地址:    %s\n", orDefault(baseURL, "默认"))
			fmt.Println()
			in, _, cancel := s.askWithBack("  保存？[Y=保存 / b=返回编辑 / q=取消]: ")
			if cancel {
				return nil, false
			}
			switch strings.ToLower(in) {
			case "q", "quit", "cancel":
				return nil, false
			case "b", "back":
				step = stepBaseURL
				continue
			default:
				return &agent.Config{
					Provider: provider,
					APIKey:   apiKey,
					Model:    model,
					BaseURL:  baseURL,
				}, true
			}
		}
	}
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
