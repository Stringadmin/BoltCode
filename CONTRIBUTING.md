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
internal/command/    # subcommand business logic (task, approve, sync, remote...)
internal/shell/      # interactive REPL
internal/gitx/       # git CLI wrapper (worktree, diff, merge)
internal/store/      # JSON metadata persistence
internal/model/      # domain models (Task, Proposal, Review)
internal/tui/        # Bubble Tea review interface
```

## Before you submit a PR

1. **Build passes:** `go build ./...`
2. **Tests pass:** `go test ./...`
3. **Style:** run `gofmt -w .` and match the existing conventions — package-level
   doc comments on every package, function doc comments on exported functions.
4. **Keep changes focused:** one logical change per PR.

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include
the BoltCode version (`boltcode --help` shows it), your OS, and a minimal
reproduction.

## License

By contributing, you agree your contributions are licensed under the [MIT License](LICENSE).
