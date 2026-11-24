package tui

import (
	"bufio"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// stdout is still used as a compatibility bridge for legacy agent text and
// slash-command rendering. Serialize full-screen chat runtimes so two sessions
// in one process cannot replace the process-wide handle at the same time.
var chatRuntimeMu sync.Mutex

func (m *ChatModel) waitForConfirm() tea.Cmd {
	return func() tea.Msg {
		cmd, ok := <-m.confirmReqCh
		if !ok {
			return nil
		}
		return confirmRequestMsg{cmd: cmd}
	}
}

func (m *ChatModel) waitForAgentEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.eventCh
		if !ok {
			return nil
		}
		return agentEventMsg{event: event}
	}
}

func (m *ChatModel) waitForAgentLine() tea.Cmd {
	return func() tea.Msg {
		line, ok := <-m.lineCh
		if !ok {
			return agentDoneMsg{}
		}
		return agentLineMsg{text: line}
	}
}

func (m *ChatModel) waitForAgentReady() tea.Cmd {
	return func() tea.Msg {
		<-m.readyCh
		return agentReadyMsg{}
	}
}

func (m *ChatModel) waitForAgentResult() tea.Cmd {
	return func() tea.Msg {
		return agentResultMsg{err: <-m.resultCh}
	}
}

// nextAgentInput is the hand-off point between the agent state machine and
// Bubble Tea. The UI becomes send-ready only after the previous turn is fully
// complete, preventing visible prompts from getting ahead of model history.
func (m *ChatModel) nextAgentInput() string {
	select {
	case m.readyCh <- struct{}{}:
	default:
	}
	select {
	case line, ok := <-m.agentIn:
		if !ok {
			return "exit"
		}
		return line
	case <-m.agent.Done():
		return "exit"
	}
}

func (m *ChatModel) statusTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return statusTickMsg{}
	})
}

// RunChat owns one full-screen chat runtime. Agent output is currently bridged
// through a private pipe for backwards-compatible terminal rendering, while
// progress and tool lifecycle use structured events. Session-local flags and
// cancellation prevent one chat from leaking state into another.
func RunChat(a *agent.Agent) (string, error) {
	chatRuntimeMu.Lock()
	defer chatRuntimeMu.Unlock()

	previousNoSpinner := a.NoSpinner
	previousUIMode := a.UIMode
	previousEventFn := a.EventFn
	previousConfirmFn := a.ConfirmFn
	a.NoSpinner = true
	a.UIMode = true
	defer func() {
		a.NoSpinner = previousNoSpinner
		a.UIMode = previousUIMode
		a.EventFn = previousEventFn
		a.ConfirmFn = previousConfirmFn
	}()

	width, height, _ := term.GetSize(os.Stdout.Fd())
	model := NewChat(a)
	model.lineCh = make(chan string, 512)
	if width > 0 {
		model.width = width
	}
	if height > 0 {
		model.height = height
	}
	model.layout()

	a.EventFn = func(event agent.Event) {
		select {
		case model.eventCh <- event:
		case <-a.Done():
		}
	}
	a.ConfirmFn = func(cmd string) bool {
		select {
		case model.confirmReqCh <- cmd:
		case <-a.Done():
			return false
		}
		select {
		case answer := <-model.confirmCh:
			return answer
		case <-a.Done():
			return false
		}
	}

	realStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer

	go forwardAgentOutput(reader, model.lineCh, a.Done())
	go func() {
		defer close(model.agentDone)
		model.resultCh <- a.Run(model.nextAgentInput)
		writer.Close()
	}()

	// Keep the default os.Stdin input on Windows. Bubble Tea recognizes that
	// exact handle and uses its UTF-16 coninput reader. WithInputTTY opens a
	// second CONIN$ handle, bypasses coninput, and leaks physical pinyin keys
	// plus code-page-garbled IME commits through the ANSI reader.
	options := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithOutput(realStdout),
		tea.WithMouseCellMotion(),
	}
	_, runErr := tea.NewProgram(model, options...).Run()

	a.Cancel()
	close(model.agentIn)
	// Do not restore the agent's classic terminal mode while its goroutine is
	// still unwinding. The old timeout allowed a slow tool to outlive the TUI,
	// after which it printed the classic summary and session footer over the
	// shell's commit prompt. Agent operations use the session context, so cancel
	// first and then make the hand-off back to the shell strictly synchronous.
	<-model.agentDone
	_ = writer.Close()
	os.Stdout = realStdout
	_ = reader.Close()
	return model.switchSessionID, runErr
}

func forwardAgentOutput(reader *os.File, lines chan<- string, done <-chan struct{}) {
	defer close(lines)
	// ReadString tolerates arbitrarily long lines; Scanner would abort the
	// whole stream at its buffer cap and close lineCh early. Overlong lines
	// are truncated instead so one huge line cannot end the display stream.
	const maxLine = 4 << 20
	br := bufio.NewReader(reader)
	for {
		raw, err := br.ReadString('\n')
		if raw != "" {
			line := strings.TrimSuffix(raw, "\n")
			if len(line) > maxLine {
				line = line[:maxLine] + "… [line truncated]"
			}
			clean, drop := normalizeAgentLine(line)
			if !drop {
				select {
				case lines <- clean:
				case <-done:
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}
