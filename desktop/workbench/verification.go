package workbench

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"time"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/charmbracelet/x/ansi"
)

const validationTimeout = 3 * time.Minute

type CheckResult struct {
	Command    string    `json:"command"`
	Status     string    `json:"status"`
	ExitCode   *int      `json:"exitCode,omitempty"`
	DurationMS int64     `json:"durationMs"`
	FinishedAt time.Time `json:"finishedAt"`
	Output     string    `json:"output"`
}

type checkSnapshot struct {
	Tree   string `json:"tree"`
	Head   string `json:"head"`
	Target string `json:"target"`
}

type savedCheck struct {
	ID       string        `json:"id"`
	Snapshot checkSnapshot `json:"snapshot"`
	Changed  bool          `json:"changed,omitempty"`
	Result   CheckResult   `json:"result"`
}

type CheckState struct {
	ID      string       `json:"id"`
	Command string       `json:"command"`
	Status  string       `json:"status"`
	Result  *CheckResult `json:"result,omitempty"`
}

type Verification struct {
	Checks  []CheckState `json:"checks"`
	Running bool         `json:"running"`
	Changed bool         `json:"changed"`
}

type verificationRun struct {
	chatID  string
	checkID string
	cancel  context.CancelFunc
	done    chan struct{}
}

func (s *snapshot) checkSnapshot() checkSnapshot {
	return checkSnapshot{Tree: s.tree, Head: s.head, Target: s.target}
}

func (checks ProjectChecks) configured() []CheckState {
	list := []CheckState{}
	if checks.Test != "" {
		list = append(list, CheckState{ID: "test", Command: checks.Test, Status: "pending"})
	}
	if checks.Typecheck != "" {
		list = append(list, CheckState{ID: "typecheck", Command: checks.Typecheck, Status: "pending"})
	}
	return list
}

func (s *Service) verification(thread *storedChat, review, current *snapshot) Verification {
	projection := Verification{Checks: s.projectChecks().configured()}
	if review != nil && current != nil {
		projection.Changed = review.checkSnapshot() != current.checkSnapshot()
	}
	run := s.checking
	projection.Running = run != nil && run.chatID == thread.ID
	for index := range projection.Checks {
		check := &projection.Checks[index]
		for _, saved := range thread.CheckResults {
			if saved.ID != check.ID {
				continue
			}
			result := saved.Result
			check.Result = &result
			check.Status = result.Status
			if current == nil || saved.Snapshot != current.checkSnapshot() || saved.Changed ||
				result.Command != check.Command || projection.Changed {
				check.Status = "stale"
			}
			break
		}
		if projection.Running && run.checkID == check.ID {
			check.Status = "running"
		}
	}
	return projection
}

func (s *Service) CheckStatus(id, token string) (Verification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, err := s.chat(id)
	if err != nil {
		return Verification{}, err
	}
	review, err := s.reviewFor(id, token)
	if err != nil {
		return Verification{}, err
	}
	if s.checking != nil || thread.Merged {
		return s.verification(thread, review, review), nil
	}
	current, err := s.capture(thread)
	if err != nil {
		return Verification{}, err
	}
	return s.verification(thread, review, current), nil
}

func (s *Service) beginVerification(id, token string) (*verificationRun, *snapshot, []CheckState, string, error) {
	if s.closed || s.active != nil || s.checking != nil {
		return nil, nil, nil, "", errors.New("请等待当前任务或验证结束")
	}
	thread, err := s.chat(id)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if thread.Merged {
		return nil, nil, nil, "", errors.New("此对话已经合并")
	}
	if thread.Archived {
		return nil, nil, nil, "", errors.New("请先移出归档，再运行检查")
	}
	review, err := s.reviewFor(id, token)
	if err != nil {
		return nil, nil, nil, "", err
	}
	checks := s.projectChecks().configured()
	if len(checks) == 0 {
		return nil, nil, nil, "", errors.New("请先配置项目检查命令")
	}
	if err := checkGitOperations(s.worktree(id)); err != nil {
		return nil, nil, nil, "", err
	}
	current, err := s.capture(thread)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if current.checkSnapshot() != review.checkSnapshot() {
		return nil, nil, nil, "", errors.New("工作区或目标分支已变化，请重新扫描 Diff")
	}
	key := ""
	if config, _ := s.agentConfig(); config != nil {
		key = config.APIKey
	}
	return &verificationRun{chatID: id, checkID: checks[0].ID, done: make(chan struct{})}, review, checks, key, nil
}

func (s *Service) RunChecks(id, token string) (Verification, error) {
	s.mu.Lock()
	run, review, checks, key, err := s.beginVerification(id, token)
	if err != nil {
		s.mu.Unlock()
		return Verification{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run.cancel = cancel
	s.checking = run
	s.mu.Unlock()
	defer cancel()
	defer close(run.done)
	s.publish(Event{ChatID: id, Kind: "checks_started"})
	workdir := s.worktree(id)
	current := review
	for _, check := range checks {
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		run.checkID = check.ID
		s.mu.Unlock()
		result := runProjectCheck(ctx, workdir, check.Command, key)

		s.mu.Lock()
		thread, _ := s.chat(id)
		var captureErr error
		current, captureErr = s.capture(thread)
		changed := captureErr != nil || current.checkSnapshot() != review.checkSnapshot()
		previous := slices.Clone(thread.CheckResults)
		thread.CheckResults = slices.DeleteFunc(thread.CheckResults, func(entry savedCheck) bool { return entry.ID == check.ID })
		thread.CheckResults = append(thread.CheckResults, savedCheck{
			ID: check.ID, Snapshot: review.checkSnapshot(), Changed: changed, Result: result,
		})
		err = s.save()
		if err != nil {
			thread.CheckResults = previous
		}
		s.mu.Unlock()
		if err != nil || changed {
			if captureErr != nil {
				err = errors.Join(err, fmt.Errorf("无法重新核对工作区：%w", captureErr))
			}
			break
		}
	}
	s.mu.Lock()
	thread, _ := s.chat(id)
	s.checking = nil
	overview := s.verification(thread, review, current)
	s.mu.Unlock()
	s.publish(Event{ChatID: id, Kind: "checks_finished"})
	return overview, err
}

func (s *Service) StopChecks(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.checking; run != nil && run.chatID == id {
		run.cancel()
	}
}

func runProjectCheck(parent context.Context, workdir, command, key string) CheckResult {
	ctx, cancel := context.WithTimeout(parent, validationTimeout)
	defer cancel()
	start := time.Now()
	output, err := agent.RunValidationCommand(ctx, workdir, command, key)
	finish := time.Now()
	result := CheckResult{
		Command: command, Status: "passed", ExitCode: new(int), FinishedAt: finish,
		DurationMS: finish.Sub(start).Milliseconds(),
		Output:     redact(ansi.Strip(output), key),
	}
	if err == nil {
		return result
	}
	result.ExitCode = nil
	var processErr *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		result.Status = "cancelled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Status = "timeout"
	case errors.As(err, &processErr):
		result.Status = "failed"
		code := processErr.ExitCode()
		result.ExitCode = &code
	default:
		result.Status = "error"
		result.Output = redact(ansi.Strip(err.Error()+"\n"+output), key)
	}
	return result
}
