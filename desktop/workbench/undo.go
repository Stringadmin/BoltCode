package workbench

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/idgen"
)

type roundCheckpoint struct {
	ID      string   `json:"id"`
	Before  string   `json:"before"`
	After   string   `json:"after,omitempty"`
	Ignored []string `json:"ignored,omitempty"`
	Undone  bool     `json:"undone,omitempty"`
}

type UndoFile struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Block  string `json:"block,omitempty"`
}

type UndoPreview struct {
	Files           []UndoFile `json:"files"`
	Allowed         bool       `json:"allowed"`
	AlreadyRestored bool       `json:"alreadyRestored"`
}

func checkpointRef(chatID, turnID string, before bool) string {
	suffix := "-after"
	if before {
		suffix = "-before"
	}
	return "refs/boltcode/desktop-checkpoints/" + chatID + "/" + turnID + suffix
}

func (s *Service) startCheckpoint(id, workdir string) (roundCheckpoint, error) {
	turnID := idgen.New("r")
	tree, err := s.worktreeTree(workdir, "HEAD")
	if err != nil {
		return roundCheckpoint{}, err
	}
	raw, err := runGit(workdir, "", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return roundCheckpoint{}, err
	}
	if _, err := gitText(workdir, "update-ref", checkpointRef(id, turnID, true), tree); err != nil {
		return roundCheckpoint{}, err
	}
	return roundCheckpoint{ID: turnID, Before: tree, Ignored: gitPaths(raw)}, nil
}

func (s *Service) finishCheckpoint(thread *storedChat, checkpoint *roundCheckpoint) error {
	workdir := s.worktree(thread.ID)
	if err := validateWorktree(thread, workdir); err != nil {
		return err
	}
	tree, err := s.worktreeTree(workdir, "HEAD")
	if err != nil {
		return err
	}
	if tree == checkpoint.Before {
		// No files changed during this turn.
		_, _ = gitText(workdir, "update-ref", "-d", checkpointRef(thread.ID, checkpoint.ID, true), checkpoint.Before)
		return nil
	}
	if _, err := gitText(workdir, "update-ref", checkpointRef(thread.ID, checkpoint.ID, false), tree); err != nil {
		return err
	}
	checkpoint.After = tree
	return nil
}

func (s *Service) undoCheckpoint(id, turnID string) (*storedChat, *roundCheckpoint, string, error) {
	if s.active != nil || s.checking != nil || s.closed {
		return nil, nil, "", errors.New("请在任务结束后撤回改动")
	}
	thread, err := s.chat(id)
	if err != nil {
		return nil, nil, "", err
	}
	if thread.Merged {
		return nil, nil, "", errors.New("已合并的对话不能撤回工作区改动")
	}
	if thread.Archived {
		return nil, nil, "", errors.New("请先移出归档，再撤回改动")
	}
	index := slices.IndexFunc(thread.Checkpoints, func(checkpoint roundCheckpoint) bool {
		return checkpoint.ID == turnID && checkpoint.After != "" && !checkpoint.Undone
	})
	if index < 0 {
		return nil, nil, "", errors.New("这一轮没有可撤回的文件改动")
	}
	workdir := s.worktree(thread.ID)
	if err := validateWorktree(thread, workdir); err != nil {
		return nil, nil, "", err
	}
	if err := checkGitOperations(workdir); err != nil {
		return nil, nil, "", err
	}
	return thread, &thread.Checkpoints[index], workdir, nil
}

func (s *Service) PreviewUndo(id, turnID string) (UndoPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, checkpoint, workdir, err := s.undoCheckpoint(id, turnID)
	if err != nil {
		return UndoPreview{}, err
	}
	return s.previewCheckpoint(workdir, checkpoint)
}

func (s *Service) previewCheckpoint(workdir string, checkpoint *roundCheckpoint) (UndoPreview, error) {
	raw, err := runGit(workdir, "", "diff", "--name-status", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", checkpoint.Before, checkpoint.After, "--")
	if err != nil {
		return UndoPreview{}, err
	}
	fields := gitPaths(raw)
	if len(fields)%2 != 0 {
		return UndoPreview{}, errors.New("Git 返回了无法解析的撤回文件列表")
	}
	current, err := s.worktreeTree(workdir, "HEAD")
	if err != nil {
		return UndoPreview{}, err
	}
	againstAfter, err := diffPathSet(workdir, checkpoint.After, current)
	if err != nil {
		return UndoPreview{}, err
	}
	againstBefore, err := diffPathSet(workdir, checkpoint.Before, current)
	if err != nil {
		return UndoPreview{}, err
	}
	staged, err := diffPathSet(workdir, "--cached", "HEAD")
	if err != nil {
		return UndoPreview{}, err
	}
	preview := UndoPreview{Files: []UndoFile{}, Allowed: len(fields) != 0, AlreadyRestored: len(fields) != 0}
	for i := 0; i < len(fields); i += 2 {
		name := fields[i+1]
		if againstBefore[name] {
			preview.AlreadyRestored = false
		}
		file := UndoFile{Path: name, Action: "恢复"}
		if fields[i] == "A" {
			file.Action = "移除"
		}
		preview.Files = append(preview.Files, file)
	}
	if preview.AlreadyRestored {
		return preview, nil
	}
	for i := range preview.Files {
		file := &preview.Files[i]
		switch {
		case staged[file.Path]:
			file.Block = "有暂存改动"
		case againstAfter[file.Path]:
			file.Block = "本轮结束后已改动"
		case file.Action == "移除" && slices.ContainsFunc(checkpoint.Ignored, func(ignored string) bool {
			return ignored == file.Path || (strings.HasSuffix(ignored, "/") && strings.HasPrefix(file.Path, ignored))
		}):
			file.Block = "本轮前已有被忽略的文件"
		}
		if file.Block != "" {
			preview.Allowed = false
		}
	}
	return preview, nil
}

func diffPathSet(workdir string, versions ...string) (map[string]bool, error) {
	args := append([]string{"diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv"}, versions...)
	raw, err := runGit(workdir, "", append(args, "--")...)
	names := make(map[string]bool)
	for _, name := range gitPaths(raw) {
		names[name] = true
	}
	return names, err
}

func (s *Service) UndoTurn(id, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, checkpoint, workdir, err := s.undoCheckpoint(id, turnID)
	if err != nil {
		return err
	}
	preview, err := s.previewCheckpoint(workdir, checkpoint)
	if err != nil {
		return err
	}
	if !preview.Allowed {
		return errors.New("文件已变化或有暂存改动，已停止撤回。请刷新预览")
	}
	if !preview.AlreadyRestored {
		patch, err := runGit(workdir, "", "diff", "--binary", "--full-index", "--no-color", "--no-renames", "--no-ext-diff", "--no-textconv",
			"--src-prefix=a/", "--dst-prefix=b/", checkpoint.Before, checkpoint.After, "--")
		if err != nil {
			return err
		}
		// Applying without --index preserves the real staging area. Git checks
		// the full inverse patch before touching the working files.
		if _, err := runGitInput(workdir, "", patch, "apply", "--reverse", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("无法安全撤回，请检查当前工作区：%w", err)
		}
	}

	checkpoint.Undone = true
	previousHistory := thread.History
	thread.History = append(thread.History, agent.Message{Role: "user",
		Content: "[BoltCode] 我撤回了较早一轮的工作区改动。继续编辑前，请重新读取要修改的文件，以当前工作区状态为准。"})
	messageIndex := slices.IndexFunc(thread.Messages, func(message Message) bool { return message.TurnID == turnID })
	if messageIndex >= 0 {
		thread.Messages[messageIndex].Undone = true
	}
	s.review = nil
	if err := s.save(); err != nil {
		checkpoint.Undone = false
		thread.History = previousHistory
		if messageIndex >= 0 {
			thread.Messages[messageIndex].Undone = false
		}
		return fmt.Errorf("文件已恢复，保存撤回记录失败。可以重新确认：%w", err)
	}
	return nil
}
