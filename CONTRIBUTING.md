# Contributing to BoltCode

Thanks for your interest in improving BoltCode! This is a small project, so the
process is lightweight.

## Development setup

Requirements:

- [Go 1.26+](https://go.dev/dl/)
- `git` on your `PATH`

```bash
git clone https://github.com/Stringadmin/BoltCode.git
cd BoltCode
go build ./...
go test ./...
```

The project is structured as:

```
main.go, dispatch.go, handlers_*.go   # entry + subcommand routing
internal/agent/      # built-in AI agent (LLM API, tools, session)
internal/banner/     # startup ASCII logo
internal/command/    # subcommand business logic (task, approve, sync, remote...)
internal/fio/        # atomic file writes + cross-process file locks
internal/gitx/       # git CLI wrapper (worktree, diff, merge)
internal/idgen/      # short random id generation + validation
internal/model/      # domain models (Task, Proposal, Review)
internal/shell/      # interactive REPL
internal/sshx/       # SSH client (key copy, bare-repo init, TOFU host keys)
internal/store/      # JSON metadata persistence (with locking)
internal/tui/        # Bubble Tea review interface
```

## Before you submit a PR

1. **Build passes:** `go build ./...`
2. **Tests pass:** `go test ./...`
3. **Vet is clean:** `go vet ./...`
4. **Formatting:** `gofmt -w .` — the CI enforces this; a PR with unformatted
   files will fail. Match the existing conventions: package-level doc comments
   on every package, function doc comments on exported functions.
5. **Keep changes focused:** one logical change per PR.

CI runs build/vet/test across Linux, macOS, and Windows on every push and PR.
Make sure the checks are green before requesting review.

## Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/)
so the release changelog is generated automatically. Prefix your commits with:

- `feat:` — a new feature
- `fix:` — a bug fix
- `docs:` — documentation only
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `test:` — adding or correcting tests
- `chore:` — build/tooling/maintenance

Example: `fix: prevent Ctrl-C from approving a command in the agent session`

`docs:`, `test:`, and `chore:` commits are excluded from the release changelog.

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include
the BoltCode version (`boltcode --version`), your OS, and a minimal
reproduction.

## Security

BoltCode handles API keys and SSH credentials. If you find a security
vulnerability, **please do not open a public issue** — see
[SECURITY.md](SECURITY.md) for how to report it privately.

## License

By contributing, you agree your contributions are licensed under the [MIT License](LICENSE).
