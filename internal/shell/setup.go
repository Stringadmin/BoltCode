package shell

import (
	"fmt"
	"strings"
	"syscall"

	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/sshx"

	"golang.org/x/term"
)

// runSetup is the interactive server configuration wizard.
// It asks for server details, connects, copies the SSH key, and inits the repo.
func (s *Session) runSetup() {
	fmt.Println()
	fmt.Println(titleStyle.Render("  ╔══════════════════════════════════════╗"))
	fmt.Println(titleStyle.Render("  ║   BoltCode — 远程仓库配置向导        ║"))
	fmt.Println(titleStyle.Render("  ╚══════════════════════════════════════╝"))
	fmt.Println()
	fmt.Println(infoStyle.Render("  配置代码远程仓库"))
	fmt.Println()

	// Choose: hosted platform (GitHub/GitLab…) vs self-hosted server.
	choice := s.ask("  连接到 GitHub/GitLab 等托管平台？(y/n) [n]: ")
	if strings.EqualFold(choice, "y") {
		s.runHostedSetup()
		return
	}
	s.runSelfHostedSetup()
}

// runHostedSetup records a hosted-platform remote (GitHub/GitLab/…) and gives
// SSH-key / HTTPS-token guidance. No bare-repo init — the repo exists already.
func (s *Session) runHostedSetup() {
	if !s.ensureApp() {
		return
	}
	url := s.ask("  仓库 URL (https://... 或 git@github.com:...): ")
	if url == "" {
		fmt.Println(errStyle.Render("  取消"))
		return
	}
	if err := s.App.RemoteAdd(url, "origin"); err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		return
	}
	fmt.Println(okStyle.Render("  ✓ remote 已配置: " + url))

	pubKey, generated, err := command.EnsureSSHKey()
	if err != nil {
		fmt.Println(errStyle.Render("  生成 SSH 密钥失败: " + err.Error()))
		return
	}
	fmt.Println()
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		fmt.Println(infoStyle.Render("  HTTPS 方式：首次 push 时输入用户名 + Personal Access Token"))
	} else {
		if generated {
			fmt.Println(infoStyle.Render("  已生成新 SSH 密钥"))
		}
		fmt.Println(infoStyle.Render("  将以下公钥添加到平台账户 (Settings → SSH keys):"))
		fmt.Println(infoStyle.Render("  " + pubKey))
	}
	fmt.Println()
	fmt.Println(okStyle.Render("  完成！用 'push' 推送代码"))
}

// runSelfHostedSetup is the guided flow for a self-hosted git server: it asks
// for details, connects, copies the SSH key, and inits the bare repo.
func (s *Session) runSelfHostedSetup() {
	fmt.Println()
	fmt.Println(infoStyle.Render("  配置你的自托管服务器（通过 SSH 连接）"))
	fmt.Println()

	// Step 1: Collect info
	host := s.ask("  服务器地址 (IP 或域名): ")
	if host == "" {
		fmt.Println(errStyle.Render("  取消"))
		return
	}
	port := s.ask("  SSH 端口 [22]: ")
	if port == "" {
		port = "22"
	}
	user := s.ask("  登录用户名 [root]: ")
	if user == "" {
		user = "root"
	}
	password := s.askPassword("  SSH 登录密码: ")
	if password == "" {
		fmt.Println(errStyle.Render("  取消"))
		return
	}
	repoPath := s.ask("  仓库路径 [~/boltcode.git]: ")
	if repoPath == "" {
		repoPath = "~/boltcode.git"
	}

	cfg := &sshx.Config{
		Host: host, Port: port, User: user,
		Password: password, RepoPath: repoPath,
	}

	fmt.Println()

	// Step 2: Connect
	fmt.Print(infoStyle.Render("  [1/4] 连接服务器... "))
	client, err := sshx.Connect(cfg)
	if err != nil {
		fmt.Println(errStyle.Render("✗"))
		fmt.Println(errStyle.Render("  连接失败: " + err.Error()))
		return
	}
	defer client.Close()
	fmt.Println(okStyle.Render("✓ 已连接"))

	// Step 3: Ensure SSH key and copy to server
	fmt.Print(infoStyle.Render("  [2/4] 配置免密登录... "))
	_, generated, err := command.EnsureSSHKey()
	if err != nil {
		fmt.Println(errStyle.Render("✗"))
		fmt.Println(errStyle.Render("  生成密钥失败: " + err.Error()))
		return
	}
	if generated {
		fmt.Print(infoStyle.Render("(已生成新密钥) "))
	}
	if err := sshx.CopyPublicKey(client); err != nil {
		fmt.Println(errStyle.Render("✗"))
		fmt.Println(errStyle.Render("  拷贝公钥失败: " + err.Error()))
		return
	}
	fmt.Println(okStyle.Render("✓ 免密登录已配置"))

	// Step 4: Init bare repo on server
	fmt.Print(infoStyle.Render("  [3/4] 初始化远程仓库... "))
	out, err := sshx.InitBareRepo(client, cfg.RepoPath)
	if err != nil {
		fmt.Println(errStyle.Render("✗"))
		fmt.Println(errStyle.Render("  " + out))
		return
	}
	fmt.Println(okStyle.Render("✓ " + out))

	// Step 5: Configure git remote locally
	fmt.Print(infoStyle.Render("  [4/4] 配置本地 git remote... "))
	if s.App != nil {
		url := fmt.Sprintf("%s@%s:%s", cfg.User, cfg.Host, cfg.RepoPath)
		if err := s.App.RemoteAdd(url, "origin"); err != nil {
			fmt.Println(errStyle.Render("✗ " + err.Error()))
			return
		}
		fmt.Println(okStyle.Render("✓ remote 已配置"))
	} else {
		fmt.Println(infoStyle.Render("跳过 (不在 boltcode 项目中)"))
	}

	fmt.Println()
	fmt.Println(okStyle.Render("  ══════════════════════════════════════"))
	fmt.Println(okStyle.Render("  ✓ 服务器配置完成！"))
	fmt.Println(infoStyle.Render(fmt.Sprintf("  远程地址: %s@%s:%s", cfg.User, cfg.Host, cfg.RepoPath)))
	fmt.Println(infoStyle.Render("  现在可以用 'push' 推送代码到服务器"))
	fmt.Println(okStyle.Render("  ══════════════════════════════════════"))
}

// ask prints a prompt and reads one line of input.
func (s *Session) ask(prompt string) string {
	s.rl.SetPrompt(prompt)
	line, err := s.rl.Readline()
	s.rl.SetPrompt(promptStyle.Render("boltcode") + "> ")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

// askWithBack is like ask but recognizes navigation commands: "b"/"back" to go
// to the previous field, "q"/"quit"/"cancel" (or EOF/Ctrl-C) to abort. It
// returns the trimmed input plus flags for back and cancel. An empty input
// (just Enter) is returned as "" with both flags false, letting the caller
// treat it as "keep current value".
func (s *Session) askWithBack(prompt string) (input string, back, cancel bool) {
	s.rl.SetPrompt(prompt)
	line, err := s.rl.Readline()
	s.rl.SetPrompt(promptStyle.Render("boltcode") + "> ")
	if err != nil {
		// EOF or Ctrl-C interrupt → treat as cancel.
		return "", false, true
	}
	input = strings.TrimSpace(line)
	switch strings.ToLower(input) {
	case "b", "back":
		return "", true, false
	case "q", "quit", "cancel":
		return "", false, true
	}
	return input, false, false
}

// askSecretWithBack is the hidden-input counterpart of askWithBack. It keeps
// navigation consistent with the rest of the config wizard without echoing an
// API key into the terminal or readline history.
func (s *Session) askSecretWithBack(prompt string) (input string, back, cancel bool) {
	fmt.Print(prompt)
	secret, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", false, true
	}
	input = strings.TrimSpace(string(secret))
	switch strings.ToLower(input) {
	case "b", "back":
		return "", true, false
	case "q", "quit", "cancel":
		return "", false, true
	}
	return input, false, false
}

// askPassword reads a password without echoing it to the terminal.
func (s *Session) askPassword(prompt string) string {
	fmt.Print(prompt)
	pw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after hidden input
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(pw))
}
