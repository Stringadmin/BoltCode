package workbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/subprocess"
)

type Review struct {
	Token        string       `json:"token"`
	Files        []ReviewFile `json:"files"`
	Base         string       `json:"base"`
	Verification Verification `json:"verification"`
}

type ReviewOpening struct {
	Review
	Selected   string `json:"selected"`
	Patch      string `json:"patch"`
	PatchError string `json:"patchError,omitempty"`
}

type ReviewFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"`
}

type snapshot struct {
	Review
	chatID  string
	tree    string
	head    string
	target  string
	patches map[string]string
}

func (s *Service) worktree(id string) string {
	return filepath.Join(s.root, "worktrees", id)
}

func (s *Service) ensureWorktree(thread *storedChat) (string, error) {
	repo := &gitx.Repo{Root: thread.Repository, Quiet: true}
	if !idgen.Validate(thread.ID) {
		return "", errors.New("对话编号无效")
	}
	if thread.Start == "" {
		base, err := gitText(repo.Root, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return "", errors.New("请先切换到要开发的 Git 分支")
		}
		start, err := repo.Head("refs/heads/" + base)
		if err != nil {
			return "", err
		}
		thread.Base, thread.Start, thread.Branch = base, start, "boltcode/desktop/"+thread.ID
		if err := s.save(); err != nil {
			thread.Base, thread.Start, thread.Branch = "", "", ""
			return "", err
		}
	}
	path := s.worktree(thread.ID)
	if _, err := os.Stat(path); err == nil {
		if err := validateWorktree(thread, path); err != nil {
			return "", err
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if _, err := gitText(repo.Root, "worktree", "add", "-b", thread.Branch, path, thread.Start); err != nil {
		return "", err
	}
	return path, nil
}

func validateWorktree(thread *storedChat, path string) error {
	_, err := inspectWorktree(thread, path, false)
	return err
}

type worktreeRefs struct {
	head   string
	target string
}

func inspectWorktree(thread *storedChat, path string, resolveRefs bool) (worktreeRefs, error) {
	args := []string{"rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir"}
	if resolveRefs {
		args = append(args, "--end-of-options", "HEAD", "refs/heads/"+thread.Base)
	}
	var metadata, commonDir, branch string
	var worktreeErr, commonErr, branchErr error
	var queries sync.WaitGroup
	queries.Go(func() { metadata, worktreeErr = gitText(path, args...) })
	queries.Go(func() {
		commonDir, commonErr = gitText(thread.Repository, "rev-parse", "--path-format=absolute", "--git-common-dir")
	})
	queries.Go(func() { branch, branchErr = gitText(path, "symbolic-ref", "HEAD") })
	queries.Wait()

	lines := strings.Split(strings.ReplaceAll(metadata, "\r\n", "\n"), "\n")
	if len(lines) < 2 || !sameDirectory(lines[0], path) {
		return worktreeRefs{}, errors.New("对话的 Git worktree 已移动或丢失")
	}
	if commonErr != nil || !sameDirectory(commonDir, lines[1]) {
		return worktreeRefs{}, errors.New("对话的 worktree 已指向其他 Git 仓库")
	}
	if branchErr != nil || branch != "refs/heads/"+thread.Branch {
		return worktreeRefs{}, fmt.Errorf("请在任务 worktree 恢复分支 %s", thread.Branch)
	}
	if worktreeErr != nil {
		return worktreeRefs{}, worktreeErr
	}
	if !resolveRefs {
		return worktreeRefs{}, nil
	}
	// rev-parse can echo the end-of-options marker in addition to the refs.
	refs := lines[2:]
	if len(refs) > 0 && refs[0] == "--end-of-options" {
		refs = refs[1:]
	}
	if len(refs) != 2 {
		return worktreeRefs{}, errors.New("无法读取工作区或目标分支的提交")
	}
	return worktreeRefs{head: refs[0], target: refs[1]}, nil
}

func sameDirectory(left, right string) bool {
	a, err := os.Stat(left)
	if err != nil {
		return false
	}
	b, err := os.Stat(right)
	return err == nil && a.IsDir() && b.IsDir() && os.SameFile(a, b)
}

func (s *Service) Review(id string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return Review{}, errors.New("Agent 结束后可以审查改动")
	}
	thread, err := s.chat(id)
	if err != nil {
		return Review{}, err
	}
	if s.checking != nil {
		if s.checking.chatID != id || s.review == nil {
			return Review{}, errors.New("正在验证另一个对话")
		}
		open := s.review.Review
		open.Verification = s.verification(thread, s.review, s.review)
		return open, nil
	}
	if thread.Start == "" {
		return Review{Files: []ReviewFile{}, Verification: s.verification(thread, nil, nil)}, nil
	}
	next, err := s.capture(thread)
	if err != nil {
		return Review{}, err
	}
	if previous := s.review; previous != nil && previous.chatID == id && previous.checkSnapshot() == next.checkSnapshot() {
		view := previous.Review
		view.Verification = s.verification(thread, previous, next)
		return view, nil
	}
	raw, err := runGit(thread.Repository, "", "diff", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", thread.Start, next.tree, "--")
	if err != nil {
		return Review{}, err
	}
	next.Files, err = reviewFiles(raw)
	if err != nil {
		return Review{}, err
	}
	next.Token = idgen.New("r")
	next.Verification = s.verification(thread, next, next)
	s.review = next
	return next.Review, nil
}

// OpenReview returns the file list and the initial patch from one snapshot.
func (s *Service) OpenReview(id, selected string) (ReviewOpening, error) {
	view, err := s.Review(id)
	if err != nil {
		return ReviewOpening{}, err
	}
	initial := ReviewOpening{Review: view}
	if len(view.Files) == 0 {
		return initial, nil
	}
	if !slices.ContainsFunc(view.Files, func(file ReviewFile) bool { return file.Path == selected }) {
		selected = view.Files[0].Path
	}
	initial.Selected = selected
	initial.Patch, err = s.Diff(id, view.Token, selected)
	if err != nil {
		initial.PatchError = err.Error()
	}
	return initial, nil
}

func reviewFiles(raw []byte) ([]ReviewFile, error) {
	files := []ReviewFile{}
	for _, row := range strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00") {
		if row == "" {
			continue
		}
		fields := strings.SplitN(row, "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			return nil, errors.New("Git 返回了无法解析的文件变更统计")
		}
		file := ReviewFile{Path: fields[2], Binary: fields[0] == "-" && fields[1] == "-"}
		if !file.Binary {
			added, addErr := strconv.Atoi(fields[0])
			deleted, deleteErr := strconv.Atoi(fields[1])
			if addErr != nil || deleteErr != nil || added < 0 || deleted < 0 {
				return nil, errors.New("Git 返回了无效的增删行数")
			}
			file.Added, file.Deleted = added, deleted
		}
		files = append(files, file)
	}
	return files, nil
}

func (s *Service) Diff(id, token, file string) (string, error) {
	s.mu.Lock()
	thread, err := s.chat(id)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	review, err := s.reviewFor(id, token)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if !slices.ContainsFunc(review.Files, func(entry ReviewFile) bool { return entry.Path == file }) {
		s.mu.Unlock()
		return "", errors.New("此文件不在审查范围内")
	}
	cached, ok := review.patches[file]
	repo, start, tree := thread.Repository, thread.Start, review.tree
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	raw, err := runGit(repo, "", "--literal-pathspecs", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", start, tree, "--", file)
	if err != nil {
		return "", err
	}
	const limit = 256 << 10
	var patch string
	if len(raw) > limit {
		end := limit
		for end > 0 && !utf8.RuneStart(raw[end]) {
			end--
		}
		patch = string(raw[:end]) + "\n… 差异显示已截断"
	} else {
		patch = string(raw)
	}
	s.mu.Lock()
	// Bound the cache to the active snapshot and a small set of files.
	if s.review == review && len(review.patches) < 12 {
		if review.patches == nil {
			review.patches = make(map[string]string)
		}
		review.patches[file] = patch
	}
	s.mu.Unlock()
	return patch, nil
}

func (s *Service) Merge(id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.checking != nil {
		return errors.New("任务与验证结束后可以合并改动")
	}
	thread, err := s.chat(id)
	if err != nil {
		return err
	}
	if thread.Merged {
		return nil
	}
	if thread.Archived {
		return errors.New("请先移出归档，再合并改动")
	}
	review, err := s.reviewFor(id, token)
	if err != nil {
		return err
	}
	if len(review.Files) == 0 {
		return errors.New("没有可合并的改动")
	}
	if err := cleanTarget(thread, review.target); err != nil {
		return err
	}
	current, err := s.capture(thread)
	if err != nil {
		return err
	}
	if current.tree != review.tree || current.head != review.head || current.target != review.target {
		s.review = nil
		return errors.New("审查后改动或目标分支发生变化，请重新打开 Diff")
	}

	path := s.worktree(id)
	if _, err := gitText(path, "add", "-A", "--", "."); err != nil {
		return err
	}
	staged, err := gitText(path, "write-tree")
	if err != nil {
		return err
	}
	if staged != review.tree {
		s.review = nil
		return errors.New("任务文件刚刚发生变化，请重新审查")
	}
	commit := current.head
	committedTree, err := gitText(path, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return err
	}
	if staged != committedTree {
		if _, err := gitText(path, "commit", "-m", "BoltCode: "+thread.Title); err != nil {
			return err
		}
		commit, err = gitText(path, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
	}
	finalTree, err := gitText(path, "rev-parse", commit+"^{tree}")
	if err != nil {
		return err
	}
	if finalTree != review.tree {
		s.review = nil
		return errors.New("实际提交和审查内容不一致，请重新打开 Diff")
	}
	if err := cleanTarget(thread, review.target); err != nil {
		return err
	}
	repo := &gitx.Repo{Root: thread.Repository, Quiet: true}
	if err := repo.Merge(commit, "BoltCode: 合并 "+thread.Title); err != nil {
		return err
	}
	thread.Merged = true
	if err := s.save(); err != nil {
		return fmt.Errorf("Git 已合并，保存对话状态失败: %w", err)
	}
	return nil
}

func (s *Service) reviewFor(id, token string) (*snapshot, error) {
	if snap := s.review; snap != nil && snap.chatID == id && snap.Token == token {
		return snap, nil
	}
	return nil, errors.New("审查已过期，请重新打开 Diff")
}

func (s *Service) capture(thread *storedChat) (*snapshot, error) {
	path := s.worktree(thread.ID)
	refs, err := inspectWorktree(thread, path, true)
	if err != nil {
		return nil, err
	}
	if thread.Start != refs.target {
		if _, err := gitText(thread.Repository, "merge-base", "--is-ancestor", thread.Start, refs.target); err != nil {
			return nil, errors.New("目标分支已改写历史，请在 Git 中检查后再合并")
		}
	}
	tree, err := s.worktreeTree(path, refs.head)
	if err != nil {
		return nil, err
	}
	return &snapshot{Review: Review{Base: thread.Base}, chatID: thread.ID, tree: tree, head: refs.head, target: refs.target}, nil
}

func (s *Service) worktreeTree(path, head string) (string, error) {
	index, err := os.CreateTemp(s.root, "review-index-*")
	if err != nil {
		return "", err
	}
	indexPath := index.Name()
	defer os.Remove(indexPath)
	defer os.Remove(indexPath + ".lock")
	if err := index.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(indexPath); err != nil {
		return "", err
	}
	if _, err := runGit(path, indexPath, "read-tree", head); err != nil {
		return "", err
	}
	if _, err := runGit(path, indexPath, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	tree, err := runGit(path, indexPath, "write-tree")
	return strings.TrimSpace(string(tree)), err
}

func cleanTarget(thread *storedChat, expected string) error {
	branch, err := gitText(thread.Repository, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != thread.Base {
		return fmt.Errorf("请先在原仓库切换到目标分支 %s", thread.Base)
	}
	head, err := gitText(thread.Repository, "rev-parse", "HEAD")
	if err != nil || head != expected {
		return errors.New("目标分支已变更，请重新审查")
	}
	status, err := gitText(thread.Repository, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("原仓库有未提交的改动，请先提交或暂存")
	}
	return checkGitOperations(thread.Repository)
}

func checkGitOperations(workdir string) error {
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		path, err := gitText(workdir, "rev-parse", "--path-format=absolute", "--git-path", marker)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			return errors.New("工作区有进行中的 Git 操作")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func gitText(dir string, args ...string) (string, error) {
	out, err := runGit(dir, "", args...)
	return strings.TrimSpace(string(out)), err
}

func runGit(dir, index string, args ...string) ([]byte, error) {
	return runGitInput(dir, index, nil, args...)
}

func runGitInput(dir, index string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args = append([]string{"--no-pager", "-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	subprocess.HideConsole(cmd)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(name) {
		case "GIT_INDEX_FILE", "GIT_TERMINAL_PROMPT", "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) != 0 {
			return nil, fmt.Errorf("Git %s: %s", args[3], strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("Git %s: %w", args[3], err)
	}
	return out, nil
}
