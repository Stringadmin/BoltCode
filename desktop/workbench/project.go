package workbench

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

type ProjectChecks struct {
	Test      string `json:"test"`
	Typecheck string `json:"typecheck"`
}

type projectPreferences struct {
	Repository  string             `json:"repository"`
	Checks      ProjectChecks      `json:"checks"`
	FullAccess  bool               `json:"fullAccess,omitempty"`
	ToolLessons []agent.ToolLesson `json:"toolLessons,omitempty"`
}

func (s *Service) projectSettings() projectPreferences {
	for _, settings := range s.data.Projects {
		if sameProject(settings.Repository, s.data.Repository) {
			return settings
		}
	}
	return projectPreferences{Repository: s.data.Repository}
}

func (s *Service) projectChecks() ProjectChecks {
	return s.projectSettings().Checks
}

func sameProject(left, right string) bool {
	return left != "" && right != "" && (samePath(left, right) || sameDirectory(left, right))
}

func (s *Service) SetFullAccess(repository string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameProject(repository, s.data.Repository) {
		return errors.New("当前项目已更换，请重新确认访问权限")
	}
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请先结束当前任务或验证")
	}
	settings := s.projectSettings()
	if settings.FullAccess == enabled {
		return nil
	}
	settings.FullAccess = enabled
	return s.saveProjectSettings(settings)
}

func (s *Service) SaveProjectChecks(repository string, checks ProjectChecks) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameProject(repository, s.data.Repository) {
		return errors.New("当前项目已更换，请重新打开项目设置")
	}
	if s.closed || s.active != nil || s.checking != nil {
		return errors.New("请在任务或验证结束后保存项目设置")
	}
	checks.Test = strings.TrimSpace(checks.Test)
	checks.Typecheck = strings.TrimSpace(checks.Typecheck)
	for _, command := range []string{checks.Test, checks.Typecheck} {
		if strings.ContainsAny(command, "\r\n\x00") || utf8.RuneCountInString(command) > 1024 {
			return errors.New("检查命令必须是 1024 字符以内的单行文本")
		}
	}
	settings := s.projectSettings()
	settings.Checks = checks
	return s.saveProjectSettings(settings)
}

func (s *Service) saveProjectSettings(settings projectPreferences) error {
	previous := s.data.Projects
	s.setProjectSettings(settings)
	if err := s.save(); err != nil {
		s.data.Projects = previous
		return err
	}
	return nil
}

func (s *Service) setProjectSettings(settings projectPreferences) {
	settings.Repository = s.data.Repository
	previous := s.data.Projects
	next := make([]projectPreferences, 0, len(previous)+1)
	for _, entry := range previous {
		if !sameProject(entry.Repository, settings.Repository) {
			next = append(next, entry)
		}
	}
	if settings.FullAccess || settings.Checks != (ProjectChecks{}) || len(settings.ToolLessons) != 0 {
		next = append(next, settings)
	}
	s.data.Projects = next
}

func (s *Service) rememberToolLessons(learned []agent.ToolLesson) {
	if len(learned) == 0 {
		return
	}
	const maxToolLessons = 8
	settings := s.projectSettings()
	lessons := slices.Clone(settings.ToolLessons)
	for _, lesson := range learned {
		lessons = slices.DeleteFunc(lessons, func(previous agent.ToolLesson) bool { return previous == lesson })
		lessons = append(lessons, lesson)
	}
	settings.ToolLessons = slices.Clone(lessons[max(0, len(lessons)-maxToolLessons):])
	s.setProjectSettings(settings)
}

func readProjectGuidance(workdir string) (path, text string, err error) {
	root, err := os.OpenRoot(workdir)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", "", err
		}
		data, err := readReference(root, name)
		if err != nil {
			return "", "", fmt.Errorf("无法载入项目规则：%w", err)
		}
		return name, strings.TrimSpace(string(data)), nil
	}
	return "", "", nil
}
