package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/idgen"
)

func (s *Service) SetChatArchived(id string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	thread, err := s.chat(id)
	if err != nil {
		return err
	}
	if thread.Archived == archived {
		return nil
	}
	thread.Archived = archived
	if err := s.save(); err != nil {
		thread.Archived = !archived
		return err
	}
	return nil
}

func (s *Service) DeleteChat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	index := slices.IndexFunc(s.data.Chats, func(entry storedChat) bool {
		return entry.ID == id && sameProject(entry.Repository, s.data.Repository)
	})
	if index < 0 {
		return errors.New("找不到此对话")
	}
	previous := s.data
	s.data.Chats = slices.Delete(slices.Clone(previous.Chats), index, index+1)
	if err := s.saveWithoutChats(previous, previous.Chats[index:index+1]); err != nil {
		return err
	}
	if s.review != nil && s.review.chatID == id {
		s.review = nil
	}
	return nil
}

func (s *Service) ArchiveProject(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	index := slices.IndexFunc(s.data.Recent, func(entry string) bool { return sameProject(entry, path) })
	if index < 0 {
		return errors.New("此项目不在最近打开记录中")
	}
	path = s.data.Recent[index]
	previous := s.data
	s.data.Recent = slices.DeleteFunc(slices.Clone(previous.Recent), func(entry string) bool { return sameProject(entry, path) })
	s.data.ArchivedProjects = append([]string{path}, previous.ArchivedProjects...)
	isCurrent := sameProject(previous.Repository, path)
	if isCurrent {
		s.data.Repository = ""
	}
	if err := s.save(); err != nil {
		s.data = previous
		return err
	}
	if isCurrent {
		s.review = nil
	}
	return nil
}

func (s *Service) RestoreProject(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	index := slices.IndexFunc(s.data.ArchivedProjects, func(entry string) bool { return sameProject(entry, path) })
	if index < 0 {
		return errors.New("找不到此归档项目")
	}
	previous := s.data
	s.data.Recent = recentWithProject(previous.Recent, previous.ArchivedProjects[index])
	s.data.ArchivedProjects = slices.Delete(slices.Clone(previous.ArchivedProjects), index, index+1)
	if err := s.save(); err != nil {
		s.data = previous
		return err
	}
	return nil
}

func recentWithProject(recent []string, path string) []string {
	next := []string{path}
	for _, entry := range recent {
		if len(next) >= 12 {
			break
		}
		if !sameProject(entry, path) {
			next = append(next, entry)
		}
	}
	return next
}

func (s *Service) knownProjectPath(path string) string {
	if sameProject(s.data.Repository, path) {
		return s.data.Repository
	}
	for _, paths := range [][]string{s.data.Recent, s.data.ArchivedProjects} {
		for _, entry := range paths {
			if sameProject(entry, path) {
				return entry
			}
		}
	}
	for _, chat := range s.data.Chats {
		if sameProject(chat.Repository, path) {
			return chat.Repository
		}
	}
	return path
}

func (s *Service) RemoveProject(path string, deleteRecords bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	index := slices.IndexFunc(s.data.Recent, func(entry string) bool { return sameProject(entry, path) })
	if index < 0 {
		return errors.New("此仓库不在最近打开记录中")
	}
	path = s.data.Recent[index]
	previous := s.data
	s.data.Recent = slices.DeleteFunc(slices.Clone(previous.Recent), func(entry string) bool { return sameProject(entry, path) })
	isCurrent := sameProject(previous.Repository, path)
	if isCurrent {
		s.data.Repository = ""
	}

	var removed []storedChat
	if deleteRecords {
		s.data.Chats = slices.DeleteFunc(slices.Clone(previous.Chats), func(entry storedChat) bool {
			if sameProject(entry.Repository, path) {
				removed = append(removed, entry)
				return true
			}
			return false
		})
		s.data.Projects = slices.DeleteFunc(slices.Clone(previous.Projects), func(entry projectPreferences) bool {
			return sameProject(entry.Repository, path)
		})
	}
	if err := s.saveWithoutChats(previous, removed); err != nil {
		return err
	}
	if isCurrent {
		s.review = nil
	}
	return nil
}

// Delete the Agent's own memory files along with the stored transcript. Git
// worktrees are preserved because they may contain user edits or task output.
func (s *Service) saveWithoutChats(previous storage, removed []storedChat) (err error) {
	type memoryFile struct {
		path string
		data []byte
	}
	var deleted []memoryFile
	defer func() {
		if err == nil {
			return
		}
		s.data = previous
		for _, file := range deleted {
			err = errors.Join(err, fio.WriteFileAtomic(file.path, file.data, 0o600))
		}
	}()
	for _, entry := range removed {
		if !idgen.Validate(entry.ID) {
			return errors.New("对话编号无效")
		}
		path := filepath.Join(s.root, "memory", entry.ID+".json")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("读取对话记忆：%w", err)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("删除对话记忆：%w", err)
		}
		deleted = append(deleted, memoryFile{path, data})
	}
	return s.save()
}
