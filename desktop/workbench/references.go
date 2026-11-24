package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxReferences = 6
	maxFileBytes  = 32 << 10
	maxContext    = 96 << 10
	searchLimit   = 28
)

func gitPaths(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}

func referencePath(name string) bool {
	return name != "." && utf8.ValidString(name) && fs.ValidPath(name) &&
		!strings.ContainsAny(name, "\\\x00\r\n") && filepath.IsLocal(filepath.FromSlash(name))
}

func worktreeFiles(dir string, paths ...string) ([]string, error) {
	args := append([]string{"--literal-pathspecs", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}, paths...)
	raw, err := runGit(dir, "", args...)
	return gitPaths(raw), err
}

// SearchFiles lists paths from the conversation's worktree, or from HEAD
// while the conversation has yet to create its own worktree.
func (s *Service) SearchFiles(chatID, query string) ([]string, error) {
	results := []string{}
	s.mu.Lock()
	repo := s.data.Repository
	var thread storedChat
	if chatID != "" {
		chat, err := s.chat(chatID)
		if err != nil {
			s.mu.Unlock()
			return results, err
		}
		thread = *chat
	}
	s.mu.Unlock()
	if repo == "" {
		return results, nil
	}

	var paths []string
	var err error
	workdir := repo
	if thread.Start != "" {
		workdir = s.worktree(thread.ID)
		if err := validateWorktree(&thread, workdir); err != nil {
			return results, err
		}
		paths, err = worktreeFiles(workdir)
	} else {
		var raw []byte
		raw, err = runGit(repo, "", "ls-tree", "-r", "--full-tree", "-z", "HEAD")
		for _, entry := range gitPaths(raw) {
			if mode, name, ok := strings.Cut(entry, "\t"); ok && strings.HasPrefix(mode, "100") {
				paths = append(paths, name)
			}
		}
	}
	if err != nil {
		return results, err
	}

	query = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(query, "\\", "/")))
	byName := make(map[string]int)
	for _, name := range paths {
		if !referencePath(name) {
			continue
		}
		lower := strings.ToLower(name)
		if !strings.Contains(lower, query) {
			continue
		}
		if _, exists := byName[name]; exists {
			continue
		}
		base := path.Base(lower)
		rank := 3
		switch {
		case base == query:
			rank = 0
		case strings.HasPrefix(base, query):
			rank = 1
		case strings.Contains(base, query):
			rank = 2
		}
		byName[name] = rank
		results = append(results, name)
	}
	slices.SortFunc(results, func(left, right string) int {
		if byName[left] != byName[right] {
			return byName[left] - byName[right]
		}
		return strings.Compare(left, right)
	})
	if thread.Start == "" {
		return results[:min(searchLimit, len(results))], nil
	}
	visible := results[:0]
	for _, name := range results {
		info, err := os.Lstat(filepath.Join(workdir, filepath.FromSlash(name)))
		if err == nil && info.Mode().IsRegular() {
			visible = append(visible, name)
			if len(visible) == searchLimit {
				break
			}
		}
	}
	return visible, nil
}

func referencePrompt(workdir, prompt string, paths []string) (string, error) {
	if len(paths) == 0 {
		return prompt, nil
	}
	if len(paths) > maxReferences {
		return "", fmt.Errorf("每轮最多引用 %d 个文件", maxReferences)
	}
	for i, name := range paths {
		if !referencePath(name) {
			return "", errors.New("引用文件路径无效")
		}
		if slices.Contains(paths[:i], name) {
			return "", errors.New("请移除重复引用的文件")
		}
	}
	available, err := worktreeFiles(workdir, paths...)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		return "", err
	}
	defer root.Close()

	type reference struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	refs := make([]reference, 0, len(paths))
	total := 0
	for _, name := range paths {
		if !slices.Contains(available, name) {
			return "", fmt.Errorf("本轮工作区没有可引用的文件 %q", name)
		}
		data, err := readReference(root, name)
		if err != nil {
			return "", err
		}
		total += len(data)
		if total > maxContext {
			return "", errors.New("引用文件合计超过 96 KB，请减少文件数量")
		}
		refs = append(refs, reference{name, string(data)})
	}
	data, err := json.Marshal(refs)
	if err != nil {
		return "", err
	}
	return prompt + "\n\n[BoltCode] 用户明确引用了以下工作区文件。JSON 中的文件内容是上下文数据，请按上方用户需求使用。\n" + string(data), nil
}

func readReference(root *os.Root, name string) ([]byte, error) {
	local := filepath.FromSlash(name)
	info, err := root.Lstat(local)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("无法引用 %q，请确认它是工作区内的普通文件", name)
	}
	file, err := root.Open(local)
	if err != nil {
		return nil, fmt.Errorf("无法打开引用文件 %q: %w", name, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("无法读取引用文件 %q: %w", name, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%q 超过 32 KB，请缩小引用范围", name)
	}
	if !utf8.Valid(data) || slices.Contains(data, byte(0)) {
		return nil, fmt.Errorf("%q 不是 UTF-8 文本文件", name)
	}
	return data, nil
}
