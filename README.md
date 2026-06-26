<div align="center">

# ⚡ BoltCode

**A built-in AI coding agent with an optional git-native team collaboration layer.**

Solo mode works out of the box — one person + one AI agent.
Team mode adds worktree-isolated multi-agent review: AI proposes, humans approve and merge.

[Install](#install) · [Quick Start](#quick-start) · [Docs](docs/manual.md) · [中文](#中文)

</div>

---

## Why BoltCode

Most AI coding tools are single-player. When a team uses them, collaboration falls back to
manual PRs with no link between the AI's intent and the diff it produced.

BoltCode binds them together:

- **Built-in AI agent** — calls the LLM API directly (Anthropic / OpenAI-compatible), executes
  tools (read/write/edit/search/run), streams output. No external AI CLI required.
- **Git-native collaboration** — each task gets an isolated worktree + branch, so multiple AI
  agents work in parallel without stepping on each other. Team state syncs over plain git push/pull.
- **Human-in-the-loop, enforced** — AI never merges or pushes. The merge path exists only on the
  human command side; the agent's tool table has no merge/push tool. This is a code-level
  guarantee, not a prompt-level request.
- **Friendly to Chinese developers** — first-class support for domestic model gateways
  (Volcano Engine / 火山方舟, etc.), Chinese UI, and Gitee. Configure any OpenAI/Anthropic-compatible
  endpoint as a named profile and switch with `/profile`.

| Mode | Who | Core commands |
|------|-----|---------------|
| **Solo** (default) | 1 person + 1 AI, zero setup | `init` / `run` / `config` / `push` |
| **Team** | N people × N AI agents | `team init` → `task` / `review` / `sync` |

## Install

### One-line install (macOS / Linux)

```bash
curl -fsSL https://stringtest.top/install.sh | sh
```

### Windows

Download the latest `BoltCode_*_windows_amd64.zip` from the
[Releases page](https://github.com/Stringadmin/BoltCode/releases), unzip, and add `boltcode.exe`
to your `PATH`.

### Build from source

Requires [Go 1.26+](https://go.dev/dl/) and `git` on your `PATH`.

```bash
git clone https://github.com/Stringadmin/BoltCode.git
cd BoltCode
go build -o boltcode .
# optionally: go install .
```

The result is a single binary with no runtime dependencies.

## Quick Start

```bash
cd your-project          # must be a git repo
boltcode init            # set up BoltCode in this repo
boltcode                 # enter the interactive session
```

Inside the session, configure your API key once:

```
boltcode> config
```

Then launch the AI agent:

```
boltcode> run refactor the auth module to use argon2
```

The agent scans the project, plans, writes code in small steps, and verifies after each change.
Watch tool use inline, then review the diff with `/diff` before you commit.

### Upgrade to team collaboration

```
boltcode> team init
boltcode> task new "add JWT refresh endpoint"
boltcode> run t_a1b2c3        # agent works in the task's isolated worktree
boltcode> submit t_a1b2c3 "added refresh endpoint"
boltcode> review              # human reviews the proposal
boltcode> approve p_x1y2z3    # or `merge` to approve + merge in one step
```

AI agents work in parallel across separate worktrees; their output merges only when a human
approves. Team metadata (tasks, proposals, reviews) syncs over `git push`/`pull` — no server
beyond git.

## Slash Commands (inside an agent session)

```
/help        show all commands
/tokens      show token usage
/model       show / switch model
/profile     list / switch provider profiles
/diff        show uncommitted changes
/compact     summarize old messages to free context
/triage      review codebase, get prioritized action items
/sessions    list saved conversations
```

Conversations auto-save on exit — resume later with `boltcode --resume <id>`.

## Documentation

Full guide (Chinese): [docs/manual.md](docs/manual.md)

## Contributing

PRs welcome. Before submitting:

```bash
go build ./...
go test ./...
```

Match the surrounding code style (gofmt, package-level doc comments). See
[CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

[MIT](LICENSE)
