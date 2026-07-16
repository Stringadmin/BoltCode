// Package shell implements BoltCode's interactive session mode — a persistent
// REPL where the user stays in one terminal, issues commands, watches agent
// activity, reviews proposals, and approves merges without ever leaving.
// This is the "one-stop" experience, like Claude Code's persistent session.
package shell

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/chzyer/readline"

	"github.com/Stringadmin/BoltCode/internal/banner"
	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

var (
	// promptStyle colors the main REPL prompt. NOTE: kept for compatibility but
	// NOT used to render the readline prompt directly — see promptPrefix.
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorOrange))
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMutedText))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorDanger))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSuccess))
)

// promptPrefix is the colored "boltcode" portion of the REPL prompt. It MUST
// use only basic ANSI SGR codes (here: bold + yellow fg = \033[1;33m). The
// chzyer/readline Windows ANSI writer parses the Prompt string and panics on
// 256-color codes (38;5;N indexes past its 8-entry color table). 256-color is
// fine elsewhere because those go through fmt → os.Stdout, not readline.
const promptPrefix = "\033[1;33mboltcode\033[0m"

// mainPrompt is the default REPL prompt: "boltcode> ".
const mainPrompt = promptPrefix + "> "

// promptStyle is retained as a thin wrapper returning mainPrompt, so existing
// call sites that wrote promptStyle.Render("boltcode") + "> " keep working
// without emitting 256-color codes.
var promptStyle = promptStyleShim{}

type promptStyleShim struct{}

func (promptStyleShim) Render(string) string { return promptPrefix }

// Session holds the persistent state for an interactive BoltCode session.
type Session struct {
	App       *command.App
	rl        *readline.Instance
	lastCtrlC time.Time
	mu        sync.Mutex
	sigCh     chan os.Signal // shared so launchAgent can temporarily grab signals
}

// readLine reads one line of user input using readline (with history + editing).
// Returns "exit" on Ctrl-C (ErrInterrupt), Ctrl-D (io.EOF), or when the
// readline instance is closed (ErrClosed) — all of which mean "stop reading".
// Returning "exit" (rather than "") on EOF is important: the main loop treats
// "" as "empty input, re-prompt", so an EOF previously caused an infinite
// re-prompt loop. Callers that need to distinguish a real "exit" command from
// an EOF/Ctrl-C can check the source, but the safe action in all three cases is
// to stop.
func (s *Session) readLine() string {
	line, err := s.rl.Readline()
	if err != nil {
		return "exit"
	}
	return strings.TrimSpace(line)
}

// Run enters the interactive session. It shows the banner, prints status,
// then loops reading commands until the user types `exit` or `quit`.
func Run(version string) error {
	banner.Print(version)

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	app, err := command.Load(wd)
	if err != nil {
		fmt.Println(infoStyle.Render("  not in a boltcode project"))
		fmt.Println(infoStyle.Render("  run 'init' to set up, or cd into a project"))
		fmt.Println()
	}

	// Tab completion for commands
	completer := readline.NewPrefixCompleter(
		readline.PcItem("status"),
		readline.PcItem("setup"),
		readline.PcItem("config"),
		readline.PcItem("task",
			readline.PcItem("new"),
			readline.PcItem("ls"),
			readline.PcItem("claim"),
		),
		readline.PcItem("run"),
		readline.PcItem("submit"),
		readline.PcItem("review"),
		readline.PcItem("diff"),
		readline.PcItem("approve"),
		readline.PcItem("merge"),
		readline.PcItem("reject"),
		readline.PcItem("spec",
			readline.PcItem("init"),
		),
		readline.PcItem("sync"),
		readline.PcItem("team",
			readline.PcItem("init"),
		),
		readline.PcItem("push"),
		readline.PcItem("pull"),
		readline.PcItem("help"),
		readline.PcItem("exit"),
	)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          mainPrompt,
		HistoryFile:     historyPath(),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	// Intercept Ctrl+C for double-press exit protection
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	s := &Session{App: app, rl: rl, sigCh: sigCh}
	go s.handleInterrupt(sigCh)
	// Stop signal delivery when the session ends so the goroutine doesn't
	// touch readline after Run returns. (We don't close sigCh: the signal
	// package may still attempt a non-blocking send, and closing would risk a
	// send-on-closed panic. The goroutine becomes idle and is collected with
	// the process.)
	defer signal.Stop(sigCh)

	s.showStatus()
	s.loop()
	return nil
}

// handleInterrupt implements the double Ctrl+C exit protection.
// First press: show a warning. Second press within 2 seconds: exit.
//
// On the second press we close the readline instance instead of calling
// os.Exit. Closing readline unblocks any pending Readline() call in the loop,
// which then returns "exit" and lets Run return normally — so deferred cleanup
// (rl.Close, history flush) actually runs. The previous os.Exit(0) skipped
// those defers and could leave the terminal in raw mode.
func (s *Session) handleInterrupt(sigCh <-chan os.Signal) {
	for range sigCh {
		s.mu.Lock()
		elapsed := time.Since(s.lastCtrlC)
		s.lastCtrlC = time.Now()
		s.mu.Unlock()

		if elapsed < 2*time.Second {
			// Close readline so a pending Readline() in the loop returns and
			// the loop exits normally — letting deferred cleanup (rl.Close,
			// history flush) actually run. The previous os.Exit(0) skipped
			// those defers and could leave the terminal in raw mode. We don't
			// print "bye" here; the loop prints it when readLine returns
			// "exit" (which it does once the instance is closed).
			s.rl.Close()
			return
		}
		fmt.Println()
		fmt.Println(infoStyle.Render("  press Ctrl+C again to exit (within 2s)"))
		s.prompt()
	}
}

func (s *Session) showStatus() {
	if s.App == nil {
		return
	}
	// Status is a team dashboard; in solo mode there are no tasks/proposals to
	// show, so skip it rather than printing an empty team view.
	if !s.App.IsTeamMode() {
		return
	}
	r, err := s.App.Status()
	if err != nil {
		return
	}
	fmt.Print(command.FormatStatus(r))
}

func (s *Session) prompt() {
	// readline handles prompt display automatically
}

func (s *Session) loop() {
	fmt.Println()
	for {
		line := s.readLine()
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" || line == "q" {
			fmt.Println(infoStyle.Render("  bye"))
			return
		}
		s.exec(line)
		fmt.Println()
	}
}

func historyPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".boltcode")
	os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "history")
}

func (s *Session) exec(line string) {
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "help", "?":
		s.showHelp()
	case "status", "st":
		if s.App != nil && !s.App.IsTeamMode() {
			fmt.Println(infoStyle.Render("  status is a team dashboard — run 'team init' to enable collaboration"))
		} else {
			s.showStatus()
		}
	case "init":
		s.runInit()
	case "setup":
		s.runSetup()
	case "task":
		s.runTask(args)
	case "run":
		s.runAgent(args)
	case "config":
		s.runConfig(args)
	case "submit":
		s.runSubmit(args)
	case "review":
		s.runReview()
	case "diff":
		s.runDiff(args)
	case "approve":
		s.runApprove(args, false)
	case "merge":
		s.runApprove(args, true)
	case "reject":
		s.runReject(args)
	case "spec":
		s.runSpec(args)
	case "sync":
		s.runSync()
	case "team":
		s.runTeam(args)
	case "push":
		s.runPush()
	case "pull":
		s.runPull()
	default:
		fmt.Println(errStyle.Render("  unknown: " + cmd + " (type 'help' for commands)"))
	}
}
