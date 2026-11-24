// Package workbench connects the desktop UI to the BoltCode agent and Git.
package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/charmbracelet/x/ansi"
)

type Message struct {
	Role            string           `json:"role"`
	Text            string           `json:"text"`
	Detail          string           `json:"detail,omitempty"`
	Failed          bool             `json:"failed,omitempty"`
	Output          string           `json:"output,omitempty"`
	Progress        bool             `json:"progress,omitempty"`
	DurationMS      *int64           `json:"durationMs,omitempty"`
	References      []string         `json:"references,omitempty"`
	ProjectRules    []string         `json:"projectRules,omitempty"`
	WorkspaceMemory *WorkspaceMemory `json:"workspaceMemory,omitempty"`
	TurnID          string           `json:"turnId,omitempty"`
	Undoable        bool             `json:"undoable,omitempty"`
	Undone          bool             `json:"undone,omitempty"`
}

type Chat struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Base     string    `json:"base"`
	Branch   string    `json:"branch"`
	Merged   bool      `json:"merged"`
	Archived bool      `json:"archived,omitempty"`
	RunState string    `json:"runState,omitempty"`
	RunMode  string    `json:"runMode,omitempty"`
	Messages []Message `json:"messages"`
}

type storedChat struct {
	Chat
	Repository     string            `json:"repository"`
	Start          string            `json:"start"`
	History        []agent.Message   `json:"history"`
	Checkpoints    []roundCheckpoint `json:"checkpoints,omitempty"`
	CheckResults   []savedCheck      `json:"checkResults,omitempty"`
	MemoryVersions []WorkspaceFact   `json:"memoryVersions,omitempty"`
}

type storage struct {
	Repository       string               `json:"repository"`
	Chats            []storedChat         `json:"chats"`
	Recent           []string             `json:"recent,omitempty"`
	ArchivedProjects []string             `json:"archivedProjects,omitempty"`
	Projects         []projectPreferences `json:"projects,omitempty"`
}

type Repository struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type State struct {
	Repository       string         `json:"repository"`
	Name             string         `json:"name"`
	Chats            []Chat         `json:"chats"`
	Running          string         `json:"running"`
	Checking         string         `json:"checking"`
	ProjectChecks    ProjectChecks  `json:"projectChecks"`
	Approval         *Confirmation  `json:"approval,omitempty"`
	Model            Model          `json:"model"`
	Models           []Model        `json:"models"`
	ModelChannels    []ModelChannel `json:"modelChannels"`
	FullAccess       bool           `json:"fullAccess"`
	Recent           []Repository   `json:"recent"`
	ArchivedProjects []Repository   `json:"archivedProjects"`
	Providers        []Provider     `json:"providers"`
}

type Confirmation struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

type Event struct {
	ChatID          string           `json:"chatId"`
	Kind            string           `json:"kind"`
	Text            string           `json:"text,omitempty"`
	Detail          string           `json:"detail,omitempty"`
	Failed          bool             `json:"failed,omitempty"`
	Output          string           `json:"output,omitempty"`
	Progress        bool             `json:"progress,omitempty"`
	DurationMS      *int64           `json:"durationMs,omitempty"`
	Prompt          *Confirmation    `json:"prompt,omitempty"`
	ProjectRules    []string         `json:"projectRules,omitempty"`
	WorkspaceMemory *WorkspaceMemory `json:"workspaceMemory,omitempty"`
}

type execution struct {
	id       string
	cancel   context.CancelFunc
	done     chan struct{}
	prompt   *Confirmation
	decision chan bool
}

type Service struct {
	mu           sync.Mutex
	root         string
	data         storage
	active       *execution
	checking     *verificationRun
	closed       bool
	chooseFolder func() (string, error)
	emit         func(Event)
	review       *snapshot
}

func New(root string, chooseFolder func() (string, error), emit func(Event)) (*Service, error) {
	service := &Service{root: root, chooseFolder: chooseFolder, emit: emit}
	raw, err := os.ReadFile(filepath.Join(root, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return service, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &service.data); err != nil {
		return nil, fmt.Errorf("读取桌面对话历史: %w", err)
	}
	if service.data.Repository != "" && len(service.data.Recent) == 0 {
		service.data.Recent = []string{service.data.Repository}
		for _, entry := range service.data.Chats {
			if entry.Repository != "" && !slices.ContainsFunc(service.data.ArchivedProjects, func(path string) bool {
				return sameProject(path, entry.Repository)
			}) && !slices.ContainsFunc(service.data.Recent, func(path string) bool {
				return samePath(path, entry.Repository)
			}) {
				service.data.Recent = append(service.data.Recent, entry.Repository)
			}
		}
	}
	for i := range service.data.Chats {
		chat := &service.data.Chats[i]
		if chat.RunState == "running" {
			chat.RunState = "interrupted"
		}
	}
	return service, nil
}

func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()

	model, profiles, channels := s.modelState()
	settings := s.projectSettings()
	state := State{
		Repository: s.data.Repository, Chats: []Chat{}, Model: model, Models: profiles, ModelChannels: channels, FullAccess: settings.FullAccess,
		Recent: []Repository{}, ArchivedProjects: []Repository{}, Providers: modelProviders(),
		ProjectChecks: settings.Checks,
	}
	for _, path := range s.data.Recent {
		state.Recent = append(state.Recent, Repository{Path: path, Name: filepath.Base(path)})
	}
	for _, path := range s.data.ArchivedProjects {
		state.ArchivedProjects = append(state.ArchivedProjects, Repository{Path: path, Name: filepath.Base(path)})
	}
	if state.Repository != "" {
		state.Name = filepath.Base(state.Repository)
	}
	for _, entry := range s.data.Chats {
		if sameProject(entry.Repository, state.Repository) {
			chat := entry.Chat
			chat.Messages = slices.Clone(chat.Messages)
			state.Chats = append(state.Chats, chat)
		}
	}
	if s.active != nil {
		state.Running = s.active.id
		state.Approval = s.active.prompt
	}
	if s.checking != nil {
		state.Checking = s.checking.chatID
	}
	return state
}

func (s *Service) ChooseRepository() (bool, error) {
	path, err := s.chooseFolder()
	if err != nil || path == "" {
		return false, err
	}
	if err := s.openRepository(path, false); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) OpenRecent(path string) error {
	return s.openRepository(path, true)
}

func (s *Service) openRepository(path string, recentOnly bool) error {
	repo, err := gitx.OpenQuiet(path)
	if err != nil {
		return fmt.Errorf("请选择 Git 仓库目录: %w", err)
	}
	if !repo.HasCommits() {
		return errors.New("仓库至少需要一个 Git 提交")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if recentOnly && !slices.ContainsFunc(s.data.Recent, func(entry string) bool {
		return sameProject(entry, repo.Root)
	}) {
		return errors.New("此仓库不在最近打开记录中")
	}
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	previous := s.data
	s.data.Repository = s.knownProjectPath(repo.Root)
	s.data.Recent = recentWithProject(previous.Recent, s.data.Repository)
	s.data.ArchivedProjects = slices.DeleteFunc(slices.Clone(previous.ArchivedProjects), func(entry string) bool {
		return sameProject(entry, repo.Root)
	})
	if err := s.save(); err != nil {
		s.data = previous
		return err
	}
	s.review = nil
	return nil
}

func (s *Service) RenameChat(id, title string) error {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || utf8.RuneCountInString(title) > 100 {
		return errors.New("对话名称应为 1 至 100 个字符")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, err := s.chat(id)
	if err != nil {
		return err
	}
	previous := thread.Title
	thread.Title = title
	if err := s.save(); err != nil {
		thread.Title = previous
		return err
	}
	return nil
}

func (s *Service) WorktreePath(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, err := s.chat(id)
	if err != nil {
		return "", err
	}
	if thread.Start == "" {
		return "", errors.New("此对话尚未创建工作区")
	}
	path := s.worktree(id)
	if err := validateWorktree(thread, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) NewChat() (Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Repository == "" {
		return Chat{}, errors.New("请先打开 Git 仓库")
	}

	chat := Chat{ID: idgen.New("c"), Title: "新对话", Messages: []Message{}}
	previous := s.data.Chats
	s.data.Chats = append([]storedChat{{Chat: chat, Repository: s.data.Repository}}, previous...)
	if err := s.save(); err != nil {
		s.data.Chats = previous
		return Chat{}, err
	}
	return chat, nil
}

func (s *Service) Send(id, prompt, modeName string) error {
	return s.SendWithReferences(id, prompt, modeName, nil)
}

func (s *Service) SendWithReferences(id, prompt, modeName string, references []string) error {
	started := time.Now()
	mode, err := agent.ParseMode(modeName)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return errors.New("请输入开发需求")
	}

	s.mu.Lock()
	if s.closed || s.active != nil || s.checking != nil {
		s.mu.Unlock()
		return errors.New("已有任务或验证在运行")
	}
	thread, err := s.chat(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if thread.Merged {
		s.mu.Unlock()
		return errors.New("此对话的改动已合并，请开启新对话")
	}
	if thread.Archived {
		s.mu.Unlock()
		return errors.New("请先移出归档，再继续此对话")
	}
	config, err := s.agentConfig()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	workdir, err := s.ensureWorktree(thread)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	references = slices.Clone(references)
	modelPrompt, err := referencePrompt(workdir, prompt, references)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	rulePath, guidance, err := readProjectGuidance(workdir)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	var ruleSources []string
	if rulePath != "" {
		ruleSources = []string{rulePath}
	}
	var checkpoint roundCheckpoint
	if mode == agent.ModeBuild {
		checkpoint, err = s.startCheckpoint(id, workdir)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("无法为本轮建立工作区检查点：%w", err)
		}
		thread.Checkpoints = append(thread.Checkpoints, checkpoint)
	}
	previous := thread.Chat
	previousVersions := thread.MemoryVersions
	var memory *WorkspaceMemory
	memoryContext := ""
	resolved := s.workspaceMemory(thread, workdir, true, mode)
	if len(thread.MemoryVersions) > 0 {
		memoryContext = resolved.prompt()
	}
	if len(resolved.Facts)+len(resolved.Checks)+len(resolved.ToolLessons) > 0 {
		memory = &resolved
	}
	if len(thread.Messages) == 0 && thread.Title == "新对话" {
		title := []rune(strings.Join(strings.Fields(prompt), " "))
		if len(title) > 30 {
			title = append(title[:30], '…')
		}
		thread.Title = string(title)
	}
	thread.Messages = append(thread.Messages, Message{Role: "user", Text: prompt, References: references, TurnID: checkpoint.ID, ProjectRules: ruleSources, WorkspaceMemory: memory})
	messageIndex := len(thread.Messages) - 1
	thread.RunState, thread.RunMode = "running", string(mode)
	if err := s.save(); err != nil {
		thread.Chat = previous
		thread.MemoryVersions = previousVersions
		if checkpoint.ID != "" {
			thread.Checkpoints = thread.Checkpoints[:len(thread.Checkpoints)-1]
			_, _ = gitText(workdir, "update-ref", "-d", checkpointRef(id, checkpoint.ID, true), checkpoint.Before)
		}
		s.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &execution{id: id, cancel: cancel, done: make(chan struct{})}
	s.active = run
	s.review = nil
	engine := &agent.Agent{
		Mode: mode, Config: *config, RepoRoot: workdir, Intent: thread.Title,
		MemoryFile:      filepath.Join(s.root, "memory", id+".json"),
		ExecEnv:         agent.ExecEnv{Workdir: workdir, AutoAllow: s.projectSettings().FullAccess, HideConsole: true},
		Messages:        slices.Clone(thread.History),
		ProjectGuidance: guidance,
		WorkspaceMemory: memoryContext,
		ToolLessons:     resolved.ToolLessons,
	}
	engine.ConfirmFn = func(command string) bool { return s.confirm(ctx, run, command) }
	engine.EventFn = func(event agent.Event) { s.onAgentEvent(run, event, config.APIKey) }
	s.mu.Unlock()
	defer cancel()
	defer close(run.done)

	s.publish(Event{ChatID: id, Kind: "started", ProjectRules: ruleSources, WorkspaceMemory: memory})
	runErr := engine.RunTurn(ctx, modelPrompt)
	s.mu.Lock()
	// New conversations may have reallocated the backing slice during streaming.
	thread, _ = s.chat(id)
	thread.History = engine.Messages
	var checkpointErr error
	if checkpoint.ID != "" {
		pending := &thread.Checkpoints[len(thread.Checkpoints)-1]
		checkpointErr = s.finishCheckpoint(thread, pending)
		if checkpointErr != nil {
			checkpointErr = fmt.Errorf("本轮文件检查点无法完成：%w", checkpointErr)
		} else if pending.After != "" {
			thread.Messages[messageIndex].Undoable = true
			thread.workspaceFacts(workdir, true)
		} else {
			thread.Checkpoints = thread.Checkpoints[:len(thread.Checkpoints)-1]
		}
	}
	switch {
	case errors.Is(runErr, context.Canceled):
		thread.RunState = "interrupted"
	case runErr != nil:
		thread.RunState = "failed"
		runErr = errors.New(modelError(runErr, config.APIKey))
		thread.Messages = append(thread.Messages, Message{Role: "error", Text: runErr.Error()})
	default:
		thread.RunState = "done"
	}
	duration := time.Since(started).Milliseconds()
	thread.Messages[messageIndex].DurationMS = &duration
	s.active = nil
	previousProjects := s.data.Projects
	s.rememberToolLessons(engine.LearnedToolLessons)
	saveErr := s.save()
	if saveErr != nil {
		s.data.Projects = previousProjects
	}
	s.mu.Unlock()
	s.publish(Event{ChatID: id, Kind: "finished", DurationMS: &duration})
	if errors.Is(runErr, context.Canceled) {
		return errors.Join(checkpointErr, saveErr)
	}
	return errors.Join(runErr, checkpointErr, saveErr)
}

func (s *Service) Stop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.id == id {
		s.active.cancel()
	}
}

// Close stops the current turn and any verification before the runtime exits.
func Close(s *Service) {
	s.mu.Lock()
	s.closed = true
	run := s.active
	checking := s.checking
	if run != nil {
		run.cancel()
	}
	if checking != nil {
		checking.cancel()
	}
	s.mu.Unlock()
	if run != nil {
		<-run.done
	}
	if checking != nil {
		<-checking.done
	}
}

func (s *Service) AnswerApproval(id string, approved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.active; run != nil && run.prompt != nil && run.prompt.ID == id {
		run.decision <- approved
		run.prompt = nil
	}
}

func (s *Service) confirm(ctx context.Context, run *execution, command string) bool {
	if ctx.Err() != nil {
		return false
	}
	prompt := &Confirmation{ID: idgen.New("a"), Command: command}
	decision := make(chan bool, 1)
	s.mu.Lock()
	run.prompt, run.decision = prompt, decision
	s.mu.Unlock()
	s.publish(Event{ChatID: run.id, Kind: "confirm", Prompt: prompt})
	defer func() {
		s.mu.Lock()
		run.prompt, run.decision = nil, nil
		s.mu.Unlock()
		s.publish(Event{ChatID: run.id, Kind: "confirmed", Prompt: prompt})
	}()
	select {
	case approved := <-decision:
		return approved && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func (s *Service) onAgentEvent(run *execution, event agent.Event, key string) {
	update := Event{ChatID: run.id, Kind: string(event.Kind)}
	switch event.Kind {
	case agent.EventText, agent.EventTextDelta:
		update.Text = redact(event.Result, key)
		update.Progress = event.Progress
	case agent.EventTextReset:
	case agent.EventPhase:
		update.Text = event.Activity
	case agent.EventToolStarted, agent.EventToolFinished:
		update.Text = event.ToolName
		update.Detail = redact(event.Detail, key)
		update.Failed = event.IsError
		if event.Kind == agent.EventToolFinished && (event.IsError || event.ToolName == "run_command" || event.ToolName == "get_diagnostics") {
			update.Output = toolOutput(redact(event.Result, key))
		}
	default:
		return
	}
	if event.Kind == agent.EventText || event.Kind == agent.EventToolFinished {
		role := "assistant"
		if event.Kind == agent.EventToolFinished {
			role = "tool"
		}
		s.mu.Lock()
		thread, _ := s.chat(run.id)
		thread.Messages = append(thread.Messages, Message{
			Role: role, Text: update.Text, Detail: update.Detail, Failed: update.Failed, Output: update.Output, Progress: update.Progress,
		})
		s.mu.Unlock()
	}
	s.publish(update)
}

func toolOutput(raw string) string {
	output := strings.TrimSpace(ansi.Strip(raw))
	const limit = 12 << 10
	if len(output) <= limit {
		return output
	}
	head, tail := limit/2, len(output)-limit/2
	for head > 0 && !utf8.RuneStart(output[head]) {
		head--
	}
	for tail < len(output) && !utf8.RuneStart(output[tail]) {
		tail++
	}
	return output[:head] + "\n… 中间输出已省略 …\n" + output[tail:]
}

func (s *Service) publish(event Event) {
	if s.emit != nil {
		s.emit(event)
	}
}

func (s *Service) chat(id string) (*storedChat, error) {
	for i := range s.data.Chats {
		entry := &s.data.Chats[i]
		if entry.ID == id && sameProject(entry.Repository, s.data.Repository) {
			return entry, nil
		}
	}
	return nil, errors.New("找不到此对话")
}

func (s *Service) save() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	return fio.WriteFileAtomic(filepath.Join(s.root, "state.json"), raw, 0o600)
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func redact(text, key string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "[已隐藏]")
}
