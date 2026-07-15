# Security Policy

BoltCode handles LLM API keys, SSH credentials, and executes shell commands on
behalf of an AI agent. We take security reports seriously.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Instead, report them privately:

1. Open a **private** security advisory on GitHub:
   [Report a vulnerability](https://github.com/Stringadmin/BoltCode/security/advisories/new)
2. Or email the maintainer directly if the GitHub advisory flow is unavailable.

Please include:

- A description of the issue and its potential impact
- Steps to reproduce (a minimal example is ideal)
- The BoltCode version (`boltcode --version`) and your OS

We will acknowledge your report within 72 hours and aim to provide a fix or
mitigation within 30 days for confirmed vulnerabilities. Once a fix is released,
we will credit you in the advisory (unless you prefer to remain anonymous).

## Scope

The following are in scope:

- Command injection or path traversal in the agent's tool execution
- Credential leakage (API keys passed to subprocesses, written to world-readable
  files, or logged)
- Sandbox escapes (the agent reading/writing outside its worktree)
- TOFU/host-key verification bypasses in the SSH layer
- Race conditions that corrupt metadata or allow double-claim/double-merge

The following are generally **out of scope**:

- An AI model generating malicious code that the user then approves and runs —
  BoltCode's design is human-in-the-loop; the agent never merges or pushes
  without explicit approval. Prompt-injection resistance is a best-effort
  mitigation, not a guarantee.
- Vulnerabilities in dependencies — report those upstream, but we welcome a
  heads-up so we can bump the dependency.

## Supported Versions

Only the latest release line receives security fixes.
