package command

import (
	"fmt"
	"os"
	"path/filepath"
)

// specPath returns the path to the project spec file.
func specPath(app *App) string {
	return filepath.Join(app.Repo.Root, ".boltcode", "spec.md")
}

// SpecExists reports whether a project spec has been created.
func (a *App) SpecExists() bool {
	_, err := os.Stat(specPath(a))
	return err == nil
}

// SpecContent returns the current spec content.
func (a *App) SpecContent() (string, error) {
	data, err := os.ReadFile(specPath(a))
	if err != nil {
		return "", fmt.Errorf("no spec found — create one with: boltcode spec init")
	}
	return string(data), nil
}

// SpecWrite writes or overwrites the project spec.
func (a *App) SpecWrite(content string) error {
	return os.WriteFile(specPath(a), []byte(content), 0o644)
}

// SpecPath returns the absolute path to the spec file (exported for the editor).
func (a *App) SpecPath() string {
	return specPath(a)
}

// SpecInit creates the spec file from the built-in template.
func (a *App) SpecInit() error {
	return a.SpecWrite(specTemplate)
}

// specTemplate is the default project spec skeleton. It covers the key areas
// that AI agents need to follow: architecture, standards, file layout, API
// design, and constraints. Teams fill this in during `boltcode spec init`.
const specTemplate = `# Project Spec
<!-- This file is the "constitution" of the project. All AI agents MUST follow
     these constraints. Human reviewers judge proposals against this spec. -->

## Overview

- **Project**: [project name]
- **Description**: [one-line description]
- **Language**: [primary language, e.g. Go 1.26, TypeScript 5.x]
- **Type**: [CLI / Web / API / Library / Monorepo]

## Architecture

<!-- Describe the high-level architecture: layers, services, data flow -->

` + "```" + `
[draw your architecture here, e.g.]
┌─────────┐     ┌─────────┐     ┌─────────┐
│  CLI    │────▶│  Core   │────▶│  Store  │
└─────────┘     └─────────┘     └─────────┘
` + "```" + `

### Layers & Responsibilities

| Layer | Path | Responsibility |
|-------|------|----------------|
| | | |

## File Structure

` + "```" + `
project/
├── cmd/            [entry points]
├── internal/       [private packages]
├── pkg/            [public packages]
├── tests/          [integration tests]
└── docs/           [documentation]
` + "```" + `

## Coding Standards

### Style
- [e.g. gofmt / prettier / black — no negotiation]
- [naming conventions]
- [error handling patterns]

### Testing
- [unit test requirements: coverage threshold, naming]
- [integration test approach]
- [what MUST be tested vs what can skip]

### Dependencies
- [approved dependencies list or policy]
- [what NOT to introduce]

## API Design

<!-- If applicable: REST/gRPC/CLI interface contracts -->

### Endpoints / Commands

| Method | Path | Description |
|--------|------|-------------|
| | | |

### Data Models

<!-- Key data structures that all agents must respect -->

## Backend & Infrastructure

- **Database**: [type, schema migration approach]
- **Deployment**: [how it ships]
- **CI/CD**: [pipeline description]

## Constraints & Rules

<!-- Hard rules that AI agents must NEVER violate -->

1. [ ]
2. [ ]
3. [ ]

## Review Checklist

<!-- When reviewing a proposal, check these items -->

- [ ] Follows file structure above
- [ ] No new dependencies without justification
- [ ] Tests included for new logic
- [ ] No secrets or credentials in code
- [ ] Consistent with architecture diagram
`
